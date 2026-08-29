package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"gofile/internal/config"
	"gofile/internal/domain"
	"gofile/internal/observability/metrics"
	"gofile/internal/port"
)

const (
	// maxRetry 单任务最大重试次数
	maxRetry = 3
	// queueCapacity 任务队列缓冲容量（降级内存队列）
	queueCapacity = 100
)

// taskItem 内部队列项（降级内存 chan 使用）
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
//
// 入队优先级：Asynq（Redis 持久化，跨实例）→ 进程内 chan（降级）
type Processor struct {
	provider port.Provider
	indexer  port.Indexer
	fileRepo port.FileRepository
	aiRepo   port.AITaskRepository
	store    port.Storage
	cfg      *config.Config

	// resolve 按用户名解析生效 Provider(用户自定义配置优先),nil 时使用默认 provider
	resolve func(ctx context.Context, username string) port.Provider

	// taskEnqueuer Asynq 客户端（可选，nil 时回退到进程内 chan）
	taskEnqueuer port.TaskEnqueuer
	// workers 内部 goroutine 数（启动时固定）
	workers int

	queue   chan taskItem
	mu      sync.Mutex
	stopped bool
	done    chan struct{}
	wg      sync.WaitGroup
}

// NewProcessor 创建异步编排器
func NewProcessor(provider port.Provider, indexer port.Indexer, fileRepo port.FileRepository, aiRepo port.AITaskRepository, store port.Storage, cfg *config.Config) *Processor {
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
		workers:  workers,
		queue:    make(chan taskItem, queueCapacity),
		done:     make(chan struct{}),
	}
}

// WithResolver 注入按用户解析 Provider 的函数(用户级 AI 配置)
func (p *Processor) WithResolver(fn func(ctx context.Context, username string) port.Provider) *Processor {
	p.resolve = fn
	return p
}

// WithTaskEnqueuer 注入 Asynq 客户端（可选，nil 时回退内存 chan）
func (p *Processor) WithTaskEnqueuer(e port.TaskEnqueuer) *Processor {
	p.taskEnqueuer = e
	return p
}

// providerFor 解析任务所属用户生效的 Provider,解析不到时回退默认
func (p *Processor) providerFor(ctx context.Context, username string) port.Provider {
	if p.resolve != nil {
		if prov := p.resolve(ctx, username); prov != nil {
			return prov
		}
	}
	return p.provider
}

// Start 启动内部 worker pool（降级 chan 路径；Asynq 启用时这些 worker 大多空转）
func (p *Processor) Start() {
	slog.Info("ai processor started", "workers", p.workers)
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
}

// Stop 优雅停止：标记停止 → 广播 done → worker 排空剩余任务后全部退出。
// 关键不变量：绝不 close(p.queue)，因此 Stop 后并发 Enqueue 向 queue 发送不会 panic。
// 注意：Processor 不可重用，Stop 后 done channel 已关闭，再次 Start 将立即退出。
func (p *Processor) Stop() {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.stopped = true
	p.mu.Unlock()
	close(p.done)
	p.wg.Wait()
}

// Enqueue 投递 AI 分析任务（非阻塞，不阻断上传主链路）
// 优先级：Asynq（Redis 持久化 + 跨实例 + 内置重试）→ 进程内 chan（Redis 不可用降级）
func (p *Processor) Enqueue(ctx context.Context, filehash, filename, username string) error {
	if p == nil {
		return nil
	}
	// 优先走 Asynq 持久化路径（Asynq 路径不受进程内队列生命周期影响）
	if p.taskEnqueuer != nil {
		if err := p.taskEnqueuer.Enqueue(ctx, filehash, filename, username); err != nil {
			slog.WarnContext(ctx, "asynq enqueue failed, fallback to chan",
				"error", err, "filehash", filehash, "username", username)
		} else {
			return nil
		}
	}
	// 进程内降级路径：需在持锁期间原子检查 stopped 并尝试入队，
	// 避免 Stop 并发 close(done) 与入队之间的竞态；select 非阻塞，持锁时间可控。
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		slog.WarnContext(ctx, "ai processor stopped, dropping task", "filehash", filehash, "username", username)
		return nil
	}
	select {
	case p.queue <- taskItem{Ctx: ctx, Filehash: filehash, Filename: filename, Username: username}:
		p.mu.Unlock()
	default:
		p.mu.Unlock()
		slog.WarnContext(ctx, "ai task queue full, dropping", "filehash", filehash, "username", username)
	}
	return nil
}

// RequeueFailed 补偿：扫 status=failed && retry_count<maxRetry 的任务重新入队
func (p *Processor) RequeueFailed(ctx context.Context) int {
	if p == nil || p.aiRepo == nil {
		return 0
	}
	tasks, err := p.aiRepo.ListRequeueable(ctx, maxRetry)
	if err != nil {
		slog.WarnContext(ctx, "list requeueable ai tasks failed", "error", err)
		return 0
	}
	for _, t := range tasks {
		p.Enqueue(ctx, t.FileSha1, "", t.Username)
	}
	if len(tasks) > 0 {
		slog.InfoContext(ctx, "requeued failed ai tasks", "count", len(tasks))
	}
	return len(tasks)
}

func (p *Processor) worker() {
	defer p.wg.Done()
	for {
		select {
		case item := <-p.queue:
			p.process(item)
		case <-p.done:
			// 排空剩余任务后退出
			for {
				select {
				case item := <-p.queue:
					p.process(item)
				default:
					return
				}
			}
		}
	}
}

// ProcessOne 单任务消费（幂等锚点 = UNIQUE(file_sha1, username)）
// 供 Asynq handler（task.AITaskProcessor）和内部 worker 共同调用
// 返回 error 时 Asynq 按 MaxRetry 自动重试
func (p *Processor) ProcessOne(ctx context.Context, filehash, filename, username string) error {
	item := taskItem{Ctx: ctx, Filehash: filehash, Filename: filename, Username: username}

	if p.isAlreadyDone(ctx, filehash, username) {
		return nil
	}
	p.registerTask(ctx, filehash, username)

	summary, tags, outFilename, err := p.analyze(ctx, item)
	if err != nil {
		p.fail(ctx, filehash, username, err.Error())
		return err
	}
	if err := p.indexDocument(ctx, filehash, username, outFilename, summary, tags); err != nil {
		p.fail(ctx, filehash, username, err.Error())
		return err
	}
	p.complete(ctx, filehash, username, summary)
	return nil
}

// process 内部 worker 调用入口（chan 降级路径）
func (p *Processor) process(item taskItem) {
	ctx := item.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	_ = p.ProcessOne(ctx, item.Filehash, item.Filename, item.Username)
}

// isAlreadyDone 检查任务是否已完成（幂等跳过）
func (p *Processor) isAlreadyDone(ctx context.Context, filehash, username string) bool {
	existing, err := p.aiRepo.GetTask(ctx, filehash, username)
	if err == nil && existing != nil && existing.Status == 2 {
		slog.DebugContext(ctx, "ai task already done, skip", "filehash", filehash, "username", username)
		return true
	}
	return false
}

// registerTask 创建并标记任务为处理中
func (p *Processor) registerTask(ctx context.Context, filehash, username string) {
	if err := p.aiRepo.CreateTask(ctx, &model.AITask{FileSha1: filehash, Username: username}); err != nil {
		slog.WarnContext(ctx, "create ai task failed", "error", err, "filehash", filehash)
	}
	_ = p.aiRepo.MarkProcessing(ctx, filehash, username)
}

// analyze 执行文本提取与 LLM 分析（秒传命中时跳过 LLM）
func (p *Processor) analyze(ctx context.Context, item taskItem) (summary string, tags []string, filename string, err error) {
	filename = item.Filename
	filehash, username := item.Filehash, item.Username

	// 秒传检测：全局 summary 已存在则零成本复用
	global, gErr := p.fileRepo.GetGlobalFile(ctx, filehash)
	if gErr == nil && global.Summary != "" {
		slog.InfoContext(ctx, "ai task: fast-dedup, skip LLM", "filehash", filehash, "username", username)
		if filename == "" {
			filename = global.FileSha1
		}
		return global.Summary, splitTags(global.Tags), filename, nil
	}

	// 完整分析管线：提取文本 → LLM 分析 → 写全局 summary
	text, err := p.extractText(ctx, filehash, filename)
	if err != nil {
		return "", nil, "", fmt.Errorf("extract failed: %w", err)
	}
	analysis, err := p.providerFor(ctx, username).Analyze(ctx, filename, text)
	if err != nil {
		return "", nil, "", fmt.Errorf("analyze failed: %w", err)
	}
	tagsStr := joinTags(analysis.Tags)
	if sErr := p.fileRepo.SaveAnalysis(ctx, filehash, analysis.Summary, tagsStr); sErr != nil {
		return "", nil, "", fmt.Errorf("save analysis failed: %w", sErr)
	}
	return analysis.Summary, analysis.Tags, filename, nil
}

// indexDocument 构建向量并写入检索引擎
func (p *Processor) indexDocument(ctx context.Context, filehash, username, filename, summary string, tags []string) error {
	vector, err := p.providerFor(ctx, username).Embed(ctx, filename+" "+summary+" "+joinTags(tags))
	if err != nil {
		return fmt.Errorf("embed failed: %w", err)
	}
	doc := &port.Doc{
		ID:         username + ":" + filehash,
		Username:   username,
		Filehash:   filehash,
		Filename:   filename,
		Summary:    summary,
		Tags:       tags,
		CreatedAt:  time.Now().UnixMilli(),
		ContentVec: vector,
	}
	if err := p.indexer.Upsert(ctx, doc); err != nil {
		return fmt.Errorf("index upsert failed: %w", err)
	}
	return nil
}

// complete 标记任务完成并记录指标
func (p *Processor) complete(ctx context.Context, filehash, username, summary string) {
	if err := p.aiRepo.MarkDone(ctx, filehash, username); err != nil {
		slog.WarnContext(ctx, "mark ai task done failed", "error", err, "filehash", filehash)
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
	if err := p.aiRepo.MarkFailed(ctx, filehash, username, errMsg); err != nil {
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
