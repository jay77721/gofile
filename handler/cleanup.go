package handler

import (
	"context"
	"gofile/ai"
	"gofile/repository"
	"gofile/storage"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

const (
	// ChunkCleanupInterval 磁盘 chunk 清理间隔
	ChunkCleanupInterval = 1 * time.Hour
	// ChunkMaxAge 磁盘 chunk 最大保留时间
	ChunkMaxAge = 24 * time.Hour

	// SoftDeleteGCAge 软删除文件在存储层保留的最长时间，超过后由 GC 从存储层移除
	SoftDeleteGCAge = 7 * 24 * time.Hour
	// SoftDeleteGCInterval 软删除 GC 运行间隔
	SoftDeleteGCInterval = 24 * time.Hour
)

// StartChunkCleanup 启动定时清理过期 chunk 目录的任务
func StartChunkCleanup(chunkDir string) {
	go func() {
		ticker := time.NewTicker(ChunkCleanupInterval)
		defer ticker.Stop()

		slog.InfoContext(context.Background(), "chunk cleanup started", "interval", ChunkCleanupInterval, "maxAge", ChunkMaxAge, "dir", chunkDir)
		for range ticker.C {
			cleanupExpiredChunks(chunkDir)
		}
	}()
}

// cleanupExpiredChunks 清理超过 ChunkMaxAge 的 chunk 目录
// 目录结构：<chunkDir>/<username>/<filehash>/<index>
func cleanupExpiredChunks(chunkDir string) {
	userEntries, err := os.ReadDir(chunkDir)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.ErrorContext(context.Background(), "read chunk dir failed", "error", err, "dir", chunkDir)
		}
		return
	}

	now := time.Now()
	for _, userEntry := range userEntries {
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
			if err != nil {
				continue
			}
			if now.Sub(info.ModTime()) > ChunkMaxAge {
				dirPath := filepath.Join(userDir, hashEntry.Name())
				if err := os.RemoveAll(dirPath); err != nil {
					slog.ErrorContext(context.Background(), "remove expired chunk dir failed", "error", err, "dir", dirPath)
				} else {
					slog.InfoContext(context.Background(), "removed expired chunk dir", "dir", dirPath, "age", now.Sub(info.ModTime()))
				}
			}
		}
	}
}

// StartShareCleanup 定时清理过期分享（每天）
func StartShareCleanup(shareRepo repository.ShareRepository) {
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			before := time.Now()
			if err := shareRepo.DeleteExpired(context.Background(), before); err != nil {
				slog.ErrorContext(context.Background(), "cleanup expired shares failed", "error", err)
			} else {
				slog.InfoContext(context.Background(), "cleaned up expired shares", "before", before)
			}
		}
	}()
}

// StartAICompensation 启动 AI 失败任务补偿（周期性重新入队 failed 任务）
// StartAITaskCleanup 启动 AI 任务 TTL 清理（每天清理过期任务）
func StartAITaskCleanup(aiRepo repository.AITaskRepository, retention time.Duration) {
	if retention <= 0 {
		retention = 7 * 24 * time.Hour
	}
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()

		slog.InfoContext(context.Background(), "ai task cleanup started", "interval", "24h", "retention", retention)
		for range ticker.C {
			before := time.Now().Add(-retention)
			if err := aiRepo.CleanupExpired(context.Background(), before); err != nil {
				slog.ErrorContext(context.Background(), "cleanup expired ai tasks failed", "error", err)
			} else {
				slog.InfoContext(context.Background(), "cleaned up expired ai tasks", "before", before)
			}
		}
	}()
}

func StartAICompensation(aiProcessor *ai.Processor) {
	if aiProcessor == nil {
		return
	}
	const maxBackoff = 30 * time.Minute
	go func() {
		backoff := time.Minute
		consecutiveEmpty := 0
		slog.InfoContext(context.Background(), "ai compensation started", "initial_interval", "1m", "max_backoff", maxBackoff)
		ctx := context.Background()
		for {
			n := aiProcessor.RequeueFailed(ctx)
			if n > 0 {
				// 有任务被补偿，重置退避
				consecutiveEmpty = 0
				backoff = time.Minute
			} else {
				consecutiveEmpty++
				if consecutiveEmpty >= 3 {
					// 连续 3 次无任务，指数退避
					backoff = min(backoff*2, maxBackoff)
					consecutiveEmpty = 0
				}
			}
			slog.DebugContext(ctx, "ai compensation tick", "requeued", n, "next_interval", backoff)
			time.Sleep(backoff)
		}
	}()
}

// 逻辑：统计 tbl_user_file 中每个 file_sha1 的活跃引用数，
// 引用数为 0（即所有用户都已软删除该文件）时，从存储层删除文件内容。
func StartSoftDeleteGC(fileRepo repository.FileRepository, store storage.Storage, orphanAge time.Duration, indexer ai.Indexer) {
	if orphanAge <= 0 {
		orphanAge = SoftDeleteGCAge
	}
	go func() {
		ticker := time.NewTicker(SoftDeleteGCInterval)
		defer ticker.Stop()

		slog.InfoContext(context.Background(), "soft-delete GC started", "interval", SoftDeleteGCInterval, "orphanAge", orphanAge)
		// 启动时先跑一次，避免等待首个周期
		cleanupOrphanedFiles(fileRepo, store, orphanAge, indexer)
		for range ticker.C {
			cleanupOrphanedFiles(fileRepo, store, orphanAge, indexer)
		}
	}()
}

// cleanupOrphanedFiles 清理无活跃引用的全局文件
func cleanupOrphanedFiles(fileRepo repository.FileRepository, store storage.Storage, orphanAge time.Duration, indexer ai.Indexer) {
	// 获取创建时间超过 orphanAge 的全局文件（GC 候选）
	before := time.Now().Add(-orphanAge)
	files, err := fileRepo.ListOldest(context.Background(), before)
	if err != nil {
		slog.ErrorContext(context.Background(), "GC: list oldest files failed", "error", err)
		return
	}

	for _, f := range files {
		// 二次确认：检查 tbl_user_file 中活跃引用数
		refs, err := fileRepo.CountRefs(context.Background(), f.FileSha1)
		if err != nil {
			slog.WarnContext(context.Background(), "GC: count refs failed", "filehash", f.FileSha1, "error", err)
			continue
		}
		if refs > 0 {
			continue // 还有用户引用，跳过
		}

		// 从存储层删除文件内容
		ctx := context.Background()
		if err := store.Delete(ctx, f.FileSha1); err != nil {
			slog.ErrorContext(context.Background(), "GC: delete file from storage failed", "filehash", f.FileSha1, "error", err)
			continue
		}

		// 从 tbl_file 删除记录
		if err := fileRepo.RemoveOrphan(context.Background(), f.FileSha1); err != nil {
			slog.ErrorContext(context.Background(), "GC: remove orphan file record failed", "error", err, "filehash", f.FileSha1)
		} else {
			slog.InfoContext(context.Background(), "GC: removed orphan file", "filehash", f.FileSha1, "size", f.FileSize)
		}

		// 清理 Typesense 中该全局文件的所有用户文档
		if indexer != nil {
			if err := indexer.DeleteByFilehash(context.Background(), f.FileSha1); err != nil {
				slog.WarnContext(context.Background(), "GC: clean typesense index failed", "error", err, "filehash", f.FileSha1)
			}
		}

	}
}
