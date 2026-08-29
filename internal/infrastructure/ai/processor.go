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
	// maxRetry is the maximum number of retries per task
	maxRetry = 3
	// queueCapacity is the buffer capacity of the task queue (fallback in-memory queue)
	queueCapacity = 100
)

// taskItem is an internal queue item (used for fallback in-memory chan)
type taskItem struct {
	Ctx      context.Context
	Filehash string
	Filename string
	Username string
}

// Processor is an async AI analysis orchestrator (worker pool + task state machine)
//
// Consumption flow: extract -> analyze -> embed -> save -> upsert.
// Fast-dedup hit (global summary already exists) skips LLM calls and builds the document at zero cost.
// Failed tasks with retry_count < maxRetry are compensated by RequeueFailed.
//
// Enqueue priority: Asynq (Redis-persisted, cross-instance) -> in-process chan (fallback)
type Processor struct {
	provider port.Provider
	indexer  port.Indexer
	fileRepo port.FileRepository
	aiRepo   port.AITaskRepository
	store    port.Storage
	cfg      *config.Config

	// resolve resolves the effective Provider by username (user custom config takes precedence), falls back to default provider when nil
	resolve func(ctx context.Context, username string) port.Provider

	// taskEnqueuer is the Asynq client (optional, falls back to in-process chan when nil)
	taskEnqueuer port.TaskEnqueuer
	// workers is the number of internal goroutines (fixed at startup)
	workers int

	queue   chan taskItem
	mu      sync.Mutex
	stopped bool
	done    chan struct{}
	wg      sync.WaitGroup
}

// NewProcessor creates an async orchestrator
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

// WithResolver injects the function to resolve Provider per user (user-level AI config)
func (p *Processor) WithResolver(fn func(ctx context.Context, username string) port.Provider) *Processor {
	p.resolve = fn
	return p
}

// WithTaskEnqueuer injects the Asynq client (optional, falls back to in-memory chan when nil)
func (p *Processor) WithTaskEnqueuer(e port.TaskEnqueuer) *Processor {
	p.taskEnqueuer = e
	return p
}

// providerFor resolves the effective Provider for the task's user, falling back to default when not found
func (p *Processor) providerFor(ctx context.Context, username string) port.Provider {
	if p.resolve != nil {
		if prov := p.resolve(ctx, username); prov != nil {
			return prov
		}
	}
	return p.provider
}

// Start launches the internal worker pool (fallback chan path; workers mostly idle when Asynq is enabled)
func (p *Processor) Start() {
	slog.Info("ai processor started", "workers", p.workers)
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
}

// Stop gracefully stops: mark as stopped -> broadcast done -> workers drain remaining tasks and exit.
// Key invariant: never close(p.queue), so concurrent Enqueue after Stop will not panic when sending to the queue.
// Note: Processor is not reusable; done channel is closed after Stop, and a subsequent Start will exit immediately.
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

// Enqueue dispatches an AI analysis task (non-blocking, does not block the main upload path)
// Priority: Asynq (Redis-persisted + cross-instance + built-in retry) -> in-process chan (fallback when Redis unavailable)
func (p *Processor) Enqueue(ctx context.Context, filehash, filename, username string) error {
	if p == nil {
		return nil
	}
	// Prefer the Asynq persistent path (Asynq path is not affected by the in-process queue lifecycle)
	if p.taskEnqueuer != nil {
		if err := p.taskEnqueuer.Enqueue(ctx, filehash, filename, username); err != nil {
			slog.WarnContext(ctx, "asynq enqueue failed, fallback to chan",
				"error", err, "filehash", filehash, "username", username)
		} else {
			return nil
		}
	}
	// In-process fallback path: must atomically check stopped and attempt enqueue while holding the lock,
	// to avoid race between Stop's concurrent close(done) and enqueue; select is non-blocking and lock hold time is controllable.
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

// RequeueFailed compensation: re-enqueues tasks with status=failed && retry_count<maxRetry
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
			// Drain remaining tasks and exit
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

// ProcessOne consumes a single task (idempotency anchor = UNIQUE(file_sha1, username))
// Called by both the Asynq handler (task.AITaskProcessor) and internal workers
// When an error is returned, Asynq automatically retries according to MaxRetry
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

// process is the entry point for internal workers (chan fallback path)
func (p *Processor) process(item taskItem) {
	ctx := item.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	_ = p.ProcessOne(ctx, item.Filehash, item.Filename, item.Username)
}

// isAlreadyDone checks whether the task is already completed (skip idempotently)
func (p *Processor) isAlreadyDone(ctx context.Context, filehash, username string) bool {
	existing, err := p.aiRepo.GetTask(ctx, filehash, username)
	if err == nil && existing != nil && existing.Status == 2 {
		slog.DebugContext(ctx, "ai task already done, skip", "filehash", filehash, "username", username)
		return true
	}
	return false
}

// registerTask creates and marks the task as processing
func (p *Processor) registerTask(ctx context.Context, filehash, username string) {
	if err := p.aiRepo.CreateTask(ctx, &model.AITask{FileSha1: filehash, Username: username}); err != nil {
		slog.WarnContext(ctx, "create ai task failed", "error", err, "filehash", filehash)
	}
	_ = p.aiRepo.MarkProcessing(ctx, filehash, username)
}

// analyze performs text extraction and LLM analysis (skips LLM on fast-dedup hit)
func (p *Processor) analyze(ctx context.Context, item taskItem) (summary string, tags []string, filename string, err error) {
	filename = item.Filename
	filehash, username := item.Filehash, item.Username

	// Fast-dedup check: reuse at zero cost if global summary already exists
	global, gErr := p.fileRepo.GetGlobalFile(ctx, filehash)
	if gErr == nil && global.Summary != "" {
		slog.InfoContext(ctx, "ai task: fast-dedup, skip LLM", "filehash", filehash, "username", username)
		if filename == "" {
			filename = global.FileSha1
		}
		return global.Summary, splitTags(global.Tags), filename, nil
	}

	// Full analysis pipeline: extract text -> LLM analysis -> write global summary
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

// indexDocument builds the vector and writes to the search engine
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

// complete marks the task as completed and records metrics
func (p *Processor) complete(ctx context.Context, filehash, username, summary string) {
	if err := p.aiRepo.MarkDone(ctx, filehash, username); err != nil {
		slog.WarnContext(ctx, "mark ai task done failed", "error", err, "filehash", filehash)
	}
	metrics.RecordAITask("done")
	slog.InfoContext(ctx, "ai task done", "filehash", filehash, "username", username, "summaryLen", len(summary))
}

// extractText extracts text (wraps ai.Extract)
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

// fail marks as failed (retry_count+1), compensated by RequeueFailed
func (p *Processor) fail(ctx context.Context, filehash, username, errMsg string) {
	slog.ErrorContext(ctx, "ai task failed", "filehash", filehash, "username", username, "error", errMsg)
	metrics.RecordAITask("failed")
	if err := p.aiRepo.MarkFailed(ctx, filehash, username, errMsg); err != nil {
		slog.WarnContext(ctx, "mark ai task failed failed", "error", err, "filehash", filehash)
	}
}

// splitTags converts a comma-separated string to a slice
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

// joinTags converts a slice to a comma-separated string
func joinTags(tags []string) string {
	return strings.Join(tags, ",")
}
