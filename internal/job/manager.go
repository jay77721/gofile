package job

import (
	"context"
	"errors"
	model "gofile/internal/domain"
	"gofile/internal/port"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Requeuer is the capability to re-enqueue failed tasks, provided by the AI Processor.
type Requeuer interface {
	RequeueFailed(ctx context.Context) int
}

const (
	ChunkCleanupInterval   = time.Hour
	ChunkMaxAge            = 24 * time.Hour
	SoftDeleteGCAge        = 7 * 24 * time.Hour
	SoftDeleteGCInterval   = 24 * time.Hour
	shareCleanupInterval   = 24 * time.Hour
	multipartCleanupPeriod = 24 * time.Hour
	aiCleanupInterval      = 24 * time.Hour
)

// Manager owns all periodic and compensating background jobs of the
// application. Jobs share one cancellable context and are waited for during
// shutdown, so a process cannot close its dependencies while a job is still
// using them.
type Manager struct {
	chunkDir      string
	fileRepo      port.FileRepository
	multipartRepo port.MultipartRepository
	shareRepo     port.ShareRepository
	aiRepo        port.AITaskRepository
	aiProcessor   Requeuer
	store         port.Storage
	indexer       port.Indexer

	mu        sync.Mutex
	started   bool
	ctx       context.Context
	cancel    context.CancelFunc
	startDone chan struct{}
	wg        sync.WaitGroup
}

func NewManager(
	chunkDir string,
	fileRepo port.FileRepository,
	multipartRepo port.MultipartRepository,
	shareRepo port.ShareRepository,
	aiRepo port.AITaskRepository,
	aiProcessor Requeuer,
	store port.Storage,
	indexer port.Indexer,
) *Manager {
	return &Manager{
		chunkDir:      chunkDir,
		fileRepo:      fileRepo,
		multipartRepo: multipartRepo,
		shareRepo:     shareRepo,
		aiRepo:        aiRepo,
		aiProcessor:   aiProcessor,
		store:         store,
		indexer:       indexer,
	}
}

// Start starts every configured job. It is idempotent and returns immediately.
func (m *Manager) Start(parent context.Context) {
	if m == nil {
		return
	}
	if parent == nil {
		parent = context.Background()
	}

	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	m.started = true
	m.ctx = ctx
	m.cancel = cancel
	m.startDone = make(chan struct{})
	m.mu.Unlock()

	if m.chunkDir != "" {
		m.launch(func(ctx context.Context) { m.runChunkCleanup(ctx) })
	}
	if m.shareRepo != nil {
		m.launch(func(ctx context.Context) { m.runShareCleanup(ctx) })
	}
	if m.multipartRepo != nil && m.store != nil {
		m.launch(func(ctx context.Context) { m.runMultipartCleanup(ctx) })
	}
	if m.fileRepo != nil && m.store != nil {
		m.launch(func(ctx context.Context) { m.runSoftDeleteGC(ctx) })
	}
	if m.aiRepo != nil {
		m.launch(func(ctx context.Context) { m.runAITaskCleanup(ctx) })
	}
	if m.aiProcessor != nil {
		m.launch(func(ctx context.Context) { m.runAICompensation(ctx) })
	}

	m.mu.Lock()
	close(m.startDone)
	m.mu.Unlock()
}

// Stop cancels all jobs and waits for them to finish. It returns the context
// error if jobs do not exit before the supplied deadline.
func (m *Manager) Stop(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	m.mu.Lock()
	cancel := m.cancel
	started := m.started
	startDone := m.startDone
	m.mu.Unlock()
	if !started {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	select {
	case <-startDone:
	case <-ctx.Done():
		return ctx.Err()
	}

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return errors.Join(ctx.Err(), errors.New("background jobs did not stop before deadline"))
	}
}

func (m *Manager) launch(fn func(context.Context)) {
	m.mu.Lock()
	ctx := m.ctx
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		fn(ctx)
	}()
}

func (m *Manager) runChunkCleanup(ctx context.Context) {
	slog.InfoContext(ctx, "chunk cleanup started", "interval", ChunkCleanupInterval, "maxAge", ChunkMaxAge, "dir", m.chunkDir)
	runTicker(ctx, ChunkCleanupInterval, func() { cleanupExpiredChunks(ctx, m.chunkDir) })
}

func (m *Manager) runShareCleanup(ctx context.Context) {
	runTicker(ctx, shareCleanupInterval, func() {
		before := time.Now()
		if err := m.shareRepo.DeleteExpired(ctx, before); err != nil {
			slog.ErrorContext(ctx, "cleanup expired shares failed", "error", err)
		} else {
			slog.InfoContext(ctx, "cleaned up expired shares", "before", before)
		}
	})
}

func (m *Manager) runMultipartCleanup(ctx context.Context) {
	slog.InfoContext(ctx, "multipart cleanup started", "interval", multipartCleanupPeriod)
	cleanupExpiredMultipart(ctx, m.multipartRepo, m.store)
	runTicker(ctx, multipartCleanupPeriod, func() {
		cleanupExpiredMultipart(ctx, m.multipartRepo, m.store)
	})
}

func (m *Manager) runAITaskCleanup(ctx context.Context) {
	const retention = 7 * 24 * time.Hour
	slog.InfoContext(ctx, "ai task cleanup started", "interval", aiCleanupInterval, "retention", retention)
	runTicker(ctx, aiCleanupInterval, func() {
		before := time.Now().Add(-retention)
		if err := m.aiRepo.CleanupExpired(ctx, before); err != nil {
			slog.ErrorContext(ctx, "cleanup expired ai tasks failed", "error", err)
		} else {
			slog.InfoContext(ctx, "cleaned up expired ai tasks", "before", before)
		}
	})
}

func (m *Manager) runAICompensation(ctx context.Context) {
	const maxBackoff = 30 * time.Minute
	backoff := time.Minute
	consecutiveEmpty := 0
	slog.InfoContext(ctx, "ai compensation started", "initial_interval", "1m", "max_backoff", maxBackoff)
	for {
		n := m.aiProcessor.RequeueFailed(ctx)
		if n > 0 {
			consecutiveEmpty = 0
			backoff = time.Minute
		} else {
			consecutiveEmpty++
			if consecutiveEmpty >= 3 {
				backoff = min(backoff*2, maxBackoff)
				consecutiveEmpty = 0
			}
		}
		slog.DebugContext(ctx, "ai compensation tick", "requeued", n, "next_interval", backoff)

		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

func (m *Manager) runSoftDeleteGC(ctx context.Context) {
	slog.InfoContext(ctx, "soft-delete GC started", "interval", SoftDeleteGCInterval, "orphanAge", SoftDeleteGCAge)
	cleanupOrphanedFiles(ctx, m.fileRepo, m.store, SoftDeleteGCAge, m.indexer)
	runTicker(ctx, SoftDeleteGCInterval, func() {
		cleanupOrphanedFiles(ctx, m.fileRepo, m.store, SoftDeleteGCAge, m.indexer)
	})
}

// runTicker periodically executes fn until ctx is canceled. Callers that need an immediate first run should
// explicitly run once before calling runTicker (e.g., runMultipartCleanup / runSoftDeleteGC),
// to keep single responsibility and avoid ambiguity of a runImmediately boolean.
func runTicker(ctx context.Context, interval time.Duration, fn func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fn()
		}
	}
}

func cleanupExpiredChunks(ctx context.Context, chunkDir string) {
	userEntries, err := os.ReadDir(chunkDir)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.ErrorContext(ctx, "read chunk dir failed", "error", err, "dir", chunkDir)
		}
		return
	}

	now := time.Now()
	for _, userEntry := range userEntries {
		if err := ctx.Err(); err != nil {
			return
		}
		if !userEntry.IsDir() {
			continue
		}
		userDir := filepath.Join(chunkDir, userEntry.Name())
		hashEntries, err := os.ReadDir(userDir)
		if err != nil {
			continue
		}
		for _, hashEntry := range hashEntries {
			if !hashEntry.IsDir() {
				continue
			}
			info, err := hashEntry.Info()
			if err != nil || now.Sub(info.ModTime()) <= ChunkMaxAge {
				continue
			}
			dirPath := filepath.Join(userDir, hashEntry.Name())
			if err := os.RemoveAll(dirPath); err != nil {
				slog.ErrorContext(ctx, "remove expired chunk dir failed", "error", err, "dir", dirPath)
			} else {
				slog.InfoContext(ctx, "removed expired chunk dir", "dir", dirPath, "age", now.Sub(info.ModTime()))
			}
		}
	}
}

func cleanupExpiredMultipart(ctx context.Context, multipartRepo port.MultipartRepository, store port.Storage) {
	now := time.Now()
	expiredList, err := multipartRepo.ListExpired(ctx, now)
	if err != nil {
		slog.ErrorContext(ctx, "list expired multipart uploads failed", "error", err)
		return
	}
	for _, mu := range expiredList {
		if err := store.AbortMultipart(ctx, mu.FileSha1, mu.UploadID); err != nil {
			slog.WarnContext(ctx, "abort expired multipart on storage failed", "upload_id", mu.UploadID, "error", err)
			continue
		}
		if err := multipartRepo.UpdateStatus(ctx, mu.UploadID, mu.Username, model.MultipartStatusAborted); err != nil {
			slog.ErrorContext(ctx, "update expired multipart status failed", "upload_id", mu.UploadID, "error", err)
		} else {
			slog.InfoContext(ctx, "cleaned up expired multipart upload", "upload_id", mu.UploadID, "filehash", mu.FileSha1, "username", mu.Username)
		}
	}
}

func cleanupOrphanedFiles(ctx context.Context, fileRepo port.FileRepository, store port.Storage, orphanAge time.Duration, indexer port.Indexer) {
	before := time.Now().Add(-orphanAge)
	files, err := fileRepo.ListOldest(ctx, before)
	if err != nil {
		slog.ErrorContext(ctx, "GC: list oldest files failed", "error", err)
		return
	}
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return
		}
		refs, err := fileRepo.CountRefs(ctx, f.FileSha1)
		if err != nil {
			slog.WarnContext(ctx, "GC: count refs failed", "filehash", f.FileSha1, "error", err)
			continue
		}
		if refs > 0 {
			continue
		}
		if err := store.Delete(ctx, f.FileSha1); err != nil {
			slog.ErrorContext(ctx, "GC: delete file from storage failed", "filehash", f.FileSha1, "error", err)
			continue
		}
		if err := fileRepo.RemoveOrphan(ctx, f.FileSha1); err != nil {
			slog.ErrorContext(ctx, "GC: remove orphan file record failed", "error", err, "filehash", f.FileSha1)
		} else {
			slog.InfoContext(ctx, "GC: removed orphan file", "filehash", f.FileSha1, "size", f.FileSize)
		}
		if indexer != nil {
			if err := indexer.DeleteByFilehash(ctx, f.FileSha1); err != nil {
				slog.WarnContext(ctx, "GC: clean typesense index failed", "error", err, "filehash", f.FileSha1)
			}
		}
	}
}
