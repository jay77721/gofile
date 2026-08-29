package service

import (
	"context"
	"fmt"
	"gofile/internal/domain"
	"log/slog"
)

// ListTrash 分页查询用户回收站文件
func (s *FileService) ListTrash(ctx context.Context, username string, page, size int) ([]model.FileMeta, int64, error) {
	return s.fileRepo.ListTrash(ctx, username, page, size)
}

// Restore 恢复回收站中的文件（status 2→1），并重新入队 AI 分析重建检索引擎文档
func (s *FileService) Restore(ctx context.Context, filehash, username string) error {
	ok, err := s.fileRepo.Restore(ctx, filehash, username)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("file not found in trash")
	}
	s.enqueue(ctx, filehash, "", username)
	return nil
}

// Purge 彻底删除（回收站）：删除用户关联行；无其他活跃引用时同步清理
// 存储层内容、tbl_file 全局记录与检索引擎文档
func (s *FileService) Purge(ctx context.Context, filehash, username string) error {
	ok, err := s.fileRepo.PurgeUserFile(ctx, filehash, username)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("file not found or no permission")
	}

	refs, err := s.fileRepo.CountRefs(ctx, filehash)
	if err != nil {
		return err
	}
	if refs > 0 {
		return nil // 其他用户仍引用该文件，仅移除当前用户的关联
	}

	// 零引用：清理存储层 + 全局文件记录 + 检索引擎
	if err := s.store.Delete(ctx, filehash); err != nil {
		slog.WarnContext(ctx, "purge: delete storage failed", "error", err, "filehash", filehash)
	}
	if err := s.fileRepo.RemoveOrphan(ctx, filehash); err != nil {
		slog.WarnContext(ctx, "purge: remove orphan failed", "error", err, "filehash", filehash)
	}
	if s.indexer != nil {
		if err := s.indexer.DeleteByFilehash(ctx, filehash); err != nil {
			slog.WarnContext(ctx, "purge: clean index failed", "error", err, "filehash", filehash)
		}
	}
	return nil
}
