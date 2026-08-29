package service

import (
	"context"
	"fmt"
	"gofile/internal/domain"
	"log/slog"
)

// ListTrash lists trashed files for a user with pagination.
func (s *FileService) ListTrash(ctx context.Context, username string, page, size int) ([]model.FileMeta, int64, error) {
	return s.fileRepo.ListTrash(ctx, username, page, size)
}

// Restore restores a trashed file (status 2->1) and re-enqueues AI analysis to rebuild the search index document.
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

// Purge permanently deletes from trash: removes the user association; when no other active references remain,
// also cleans up storage content, tbl_file global record, and search index document.
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
		return nil // other users still reference the file, only remove current user's association
	}

	// zero references: clean up storage + global file record + search index
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
