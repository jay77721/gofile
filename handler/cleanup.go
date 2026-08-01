package handler

import (
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
)

// StartChunkCleanup 启动定时清理过期 chunk 目录的任务
func StartChunkCleanup(chunkDir string) {
	go func() {
		ticker := time.NewTicker(ChunkCleanupInterval)
		defer ticker.Stop()

		slog.Info("chunk cleanup started", "interval", ChunkCleanupInterval, "maxAge", ChunkMaxAge, "dir", chunkDir)
		for range ticker.C {
			cleanupExpiredChunks(chunkDir)
		}
	}()
}

// cleanupExpiredChunks 清理超过 ChunkMaxAge 的 chunk 目录
func cleanupExpiredChunks(chunkDir string) {
	entries, err := os.ReadDir(chunkDir)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Error("read chunk dir failed", "error", err, "dir", chunkDir)
		}
		return
	}

	now := time.Now()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > ChunkMaxAge {
			dirPath := filepath.Join(chunkDir, entry.Name())
			if err := os.RemoveAll(dirPath); err != nil {
				slog.Error("remove expired chunk dir failed", "error", err, "dir", dirPath)
			} else {
				slog.Info("removed expired chunk dir", "dir", dirPath, "age", now.Sub(info.ModTime()))
			}
		}
	}
}