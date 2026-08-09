package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"gofile/config"
	"gofile/metrics"
	"gofile/model"
	"gofile/repository"
	"gofile/storage"
)

const (
	// maxRetry 单任务最大重试次数
	maxRetry = 3
	// queueCapacity 任务队列缓冲容量
	queueCapacity = 100
)

// taskItem 内部队列项
type taskItem struct {
	Ctx      context.Context
	Filehash string
	Filename string
	Username string
}

// Processor 异步 AI 分析编排器（worker pool + 任务状态机）
//
// 消费流程：extract → analyze → embed → save → upsert。
// 秒传命中（全局 summary 已存在）跳过 LLM 调用，零成本建文档。
// 失败任务 retry_count < maxRetry 时由 RequeueFailed 补偿。
type Processor struct {
	provider Provider
	indexer  Indexer
	fileRepo repository.FileRepository
	aiRepo   repository.AITaskRepository
	store    storage.Storage
	cfg      *config.Config

	queue chan taskItem
	wg    sync.WaitGroup
}

// NewProcessor 创建异步编排器
func NewProcessor(provider Provider, indexer Indexer, fileRepo repository.FileRepository, aiRepo repository.AITaskRepository, store storage.Storage, cfg *config.Config) *Processor {
	workers := cfg.AIWorkers
	if workers <= 0 {
		workers = 4
	}
	return &Processor{
		provider: provider,
		indexer:  indexer,
		fileRepo: fileRepo,
		aiRepo:   aiRepo,
		store:    store,
		cfg:      cfg,
		queue:    make(chan taskItem, queueCapacity),
	}
}

// Start 启动 worker pool
func (p *Processor) Start() {
	n := cap(p.queue)
	if n <= 0 {
		n = 4
	}
	slog.Info("ai processor started", "workers", n)
	for i := 0; i < n; i++ {
		p.wg.Add(1)
		go p.worker()
	}
}

// Stop 优雅停止（等待队列排空）
func (p *Processor) Stop() {
	close(p.queue)
	p.wg.Wait()
}

// Enqueue 非阻塞入队（队列满则丢弃并打 warn 日志，不阻塞上传主链路）
func (p *Processor) Enqueue(ctx context.Context, filehash, filename, username string) {
	if p == nil {
		return
	}
	select {
	case p.queue <- taskItem{Ctx: ctx, Filehash: filehash, Filename: filename, Username: username}:
	default:
		slog.WarnContext(ctx, "ai task queue full, dropping", "filehash", filehash, "username", username)
	}
}

// RequeueFailed 补偿：扫 status=failed && retry_count<maxRetry 的任务重新入队
func (p *Processor) RequeueFailed(ctx context.Context) {
	if p == nil || p.aiRepo == nil {
		return
	}
	tasks, err := p.aiRepo.ListRequeueable(maxRetry)
	if err != nil {
		slog.WarnContext(ctx, "list requeueable ai tasks failed", "error", err)
		return
	}
	for _, t := range tasks {
		p.Enqueue(ctx, t.FileSha1, "", t.Username)
	}
	if len(tasks) > 0 {
		slog.InfoContext(ctx, "requeued failed ai tasks", "count", len(tasks))
	}
}

func (p *Processor) worker() {
	defer p.wg.Done()
	for item := range p.queue {
		p.process(item)
	}
}

// process 单任务消费（幂等锚点 = UNIQUE(file_sha1, username)）
func (p *Processor) process(item taskItem) {
	ctx := item.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	filehash, username, filename := item.Filehash, item.Username, item.Filename

	// 1. 幂等：已 done 直接跳过
	existing, err := p.aiRepo.GetTask(filehash, username)
	if err == nil && existing != nil && existing.Status == 2 {
		slog.DebugContext(ctx, "ai task already done, skip", "filehash", filehash, "username", username)
		return
	}

	// 2. 创建任务（幂等：已存在则忽略）
	if createErr := p.aiRepo.CreateTask(&model.AITask{FileSha1: filehash, Username: username}); createErr != nil {
		slog.WarnContext(ctx, "create ai task failed", "error", createErr, "filehash", filehash)
	}
	_ = p.aiRepo.MarkProcessing(filehash, username)

	// 3. 读全局 summary（秒传命中则跳过 LLM）
	global, gErr := p.fileRepo.GetGlobalFile(filehash)
	skipLLM := gErr == nil && global.Summary != ""

	var summary, tagsStr string
	var tags []string

	if skipLLM {
		// 秒传命中：复用全局 summary/tags，零成本
		summary = global.Summary
		tagsStr = global.Tags
		tags = splitTags(tagsStr)
		if filename == "" {
			filename = global.FileSha1
		}
		slog.InfoContext(ctx, "ai task: fast-dedup, skip LLM", "filehash", filehash, "username", username)
	} else {
		// 4. 完整分析管线
		text, extErr := p.extractText(ctx, filehash, filename)
		if extErr != nil {
			p.fail(ctx, filehash, username, fmt.Sprintf("extract failed: %v", extErr))
			return
		}
		analysis, aErr := p.provider.Analyze(ctx, filename, text)
		if aErr != nil {
			p.fail(ctx, filehash, username, fmt.Sprintf("analyze failed: %v", aErr))
			return
		}
		summary = analysis.Summary
		tags = analysis.Tags
		tagsStr = joinTags(tags)

		// 写全局 summary/tags（多用户秒传共享）
		if sErr := p.fileRepo.SaveAnalysis(filehash, summary, tagsStr); sErr != nil {
			p.fail(ctx, filehash, username, fmt.Sprintf("save analysis failed: %v", sErr))
			return
		}
	}

	// 5. 构建向量 + 写 Typesense
	vector, vErr := p.provider.Embed(ctx, filename+" "+summary+" "+tagsStr)
	if vErr != nil {
		p.fail(ctx, filehash, username, fmt.Sprintf("embed failed: %v", vErr))
		return
	}

	doc := &Doc{
		ID:         username + ":" + filehash,
		Username:   username,
		Filehash:   filehash,
		Filename:   filename,
		Summary:    summary,
		Tags:       tags,
		CreatedAt:  time.Now().UnixMilli(),
		ContentVec: vector,
	}
	if uErr := p.indexer.Upsert(ctx, doc); uErr != nil {
		p.fail(ctx, filehash, username, fmt.Sprintf("index upsert failed: %v", uErr))
		return
	}

	// 6. 置 done
	if dErr := p.aiRepo.MarkDone(filehash, username); dErr != nil {
		slog.WarnContext(ctx, "mark ai task done failed", "error", dErr, "filehash", filehash)
	}
	metrics.RecordAITask("done")
	slog.InfoContext(ctx, "ai task done", "filehash", filehash, "username", username, "summaryLen", len(summary))
}

// extractText 提取文本（包装 ai.Extract）
func (p *Processor) extractText(ctx context.Context, filehash, filename string) (string, error) {
	if p.store == nil {
		return "", errors.New("store is nil")
	}
	ex, err := Extract(ctx, p.store, filehash, filename)
	if err != nil {
		return "", err
	}
	return ex.Text, nil
}

// fail 标记失败（retry_count+1），由 RequeueFailed 补偿
func (p *Processor) fail(ctx context.Context, filehash, username, errMsg string) {
	slog.ErrorContext(ctx, "ai task failed", "filehash", filehash, "username", username, "error", errMsg)
	metrics.RecordAITask("failed")
	if err := p.aiRepo.MarkFailed(filehash, username, errMsg); err != nil {
		slog.WarnContext(ctx, "mark ai task failed failed", "error", err, "filehash", filehash)
	}
}

// splitTags 逗号分隔字符串 → 切片
func splitTags(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// joinTags 切片 → 逗号分隔字符串
func joinTags(tags []string) string {
	return strings.Join(tags, ",")
}
