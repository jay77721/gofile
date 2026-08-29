package repository

import (
	"context"
	"errors"
	"fmt"
	"gofile/internal/domain"
	"log/slog"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FileRepository is the file data access interface
type FileRepository interface {
	// Create registers a global file (deduplicated by SHA1, ignored if already exists)
	Create(ctx context.Context, f model.File) error
	// CreateUserFile creates the user file ownership (idempotent)
	CreateUserFile(ctx context.Context, uf model.UserFile) error
	// GetByHash retrieves file metadata by hash and username
	GetByHash(ctx context.Context, filehash, username string) (model.FileMeta, error)
	// ListByUser retrieves all files for a user
	ListByUser(ctx context.Context, username string) ([]model.FileMeta, error)
	// CountByUser counts the total number of files for a user
	CountByUser(ctx context.Context, username string) (int64, error)
	// ListTrash paginates trashed files for a user (status=2)
	ListTrash(ctx context.Context, username string, page, size int) ([]model.FileMeta, int64, error)
	// Restore restores a soft-deleted file (status 2->1)
	Restore(ctx context.Context, filehash, username string) (bool, error)
	// PurgeUserFile permanently deletes the user file association
	PurgeUserFile(ctx context.Context, filehash, username string) (bool, error)
	// ListByUserPaged paginates user files (batch query to avoid N+1)
	ListByUserPaged(ctx context.Context, username string, page, size int) ([]model.FileMeta, error)
	// Delete soft-deletes a user file (status=2)
	Delete(ctx context.Context, filehash, username string) (bool, error)
	// UpdateName updates the user file name
	UpdateName(ctx context.Context, filehash, username, newFilename string) (bool, error)
	// CountRefs counts active references to a file in tbl_user_file
	CountRefs(ctx context.Context, filehash string) (int64, error)
	// ListOldest lists global files created before 'before' (GC candidates)
	ListOldest(ctx context.Context, before time.Time) ([]model.File, error)
	// RemoveOrphan deletes orphan global file records from tbl_file
	RemoveOrphan(ctx context.Context, filehash string) error
	// SaveAnalysis writes the AI-generated summary and tags (global file dimension, idempotent)
	SaveAnalysis(ctx context.Context, filehash, summary, tags string) error
	// GetGlobalFile reads the global file by hash (including summary/tags), without user dimension
	GetGlobalFile(ctx context.Context, filehash string) (model.File, error)

	// VFS virtual file system extension interface
	GetUserFileByID(ctx context.Context, id uint, username string) (model.UserFile, error)
	ListByParent(ctx context.Context, username string, parentID uint64, offset, limit int) ([]model.FileMeta, int64, error)
	CreateFolder(ctx context.Context, uf model.UserFile) (model.UserFile, error)
	MoveItem(ctx context.Context, id uint, username string, targetParentID uint64, newDirPath string) error
	UpdateDirPathPrefix(ctx context.Context, username, oldPrefix, newPrefix string) error
	RenameItem(ctx context.Context, id uint, username, newName, newDirPath string) error
	SoftDeleteDir(ctx context.Context, username, dirPath string) error
	GetBreadcrumbs(ctx context.Context, username string, folderID uint64) ([]model.Breadcrumb, error)
}

// mysqlFileRepo is the GORM implementation of FileRepository
type mysqlFileRepo struct {
	db *gorm.DB
}

// NewFileRepository creates a GORM file repository
func NewFileRepository(db *gorm.DB) FileRepository {
	return &mysqlFileRepo{db: db}
}

// Create registers a global file, ignored if already exists (idempotent)
func (r *mysqlFileRepo) Create(ctx context.Context, f model.File) error {
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&f).Error; err != nil {
		return fmt.Errorf("create file failed: %w", err)
	}
	return nil
}

// CreateUserFile creates the user file ownership, ignored if already exists (idempotent)
func (r *mysqlFileRepo) CreateUserFile(ctx context.Context, uf model.UserFile) error {
	if uf.DirPath == "" {
		uf.DirPath = "/"
	}
	// Idempotency duplicate check
	var count int64
	_ = r.db.WithContext(ctx).Model(&model.UserFile{}).
		Where("user_name = ? AND parent_id = ? AND file_sha1 = ? AND status = ?", uf.Username, uf.ParentID, uf.FileSha1, model.UserFileStatusActive).
		Count(&count).Error
	if count > 0 {
		return nil
	}

	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&uf).Error; err != nil {
		return fmt.Errorf("create user file failed: %w", err)
	}
	return nil
}

// GetByHash queries a file owned by the user (JOIN tbl_file)
func (r *mysqlFileRepo) GetByHash(ctx context.Context, filehash, username string) (model.FileMeta, error) {
	var uf model.UserFile
	if err := r.db.WithContext(ctx).Where("file_sha1 = ? AND user_name = ? AND status = 1", filehash, username).
		First(&uf).Error; err != nil {
		return model.FileMeta{}, fmt.Errorf("get user file failed: %w", err)
	}

	var f model.File
	if err := r.db.WithContext(ctx).Where("file_sha1 = ?", filehash).First(&f).Error; err != nil {
		return model.FileMeta{}, fmt.Errorf("get file failed: %w", err)
	}

	return model.FileMeta{
		ID:       uf.ID,
		FileSha1: uf.FileSha1,
		FileName: uf.FileName,
		FileSize: f.FileSize,
		Username: uf.Username,
		ParentID: uf.ParentID,
		IsDir:    uf.IsDir,
		DirPath:  uf.DirPath,
		UploadAt: uf.CreateAt.Format("2006-01-02 15:04:05"),
		Summary:  f.Summary,
		Tags:     f.Tags,
	}, nil
}

// ListByUser queries all active files for a user (batch query to avoid N+1)
func (r *mysqlFileRepo) ListByUser(ctx context.Context, username string) ([]model.FileMeta, error) {
	var ufs []model.UserFile
	if err := r.db.WithContext(ctx).Where("user_name = ? AND status = ? AND is_dir = 0", username, model.UserFileStatusActive).
		Order("create_at DESC").
		Find(&ufs).Error; err != nil {
		return nil, fmt.Errorf("query user files failed: %w", err)
	}
	if len(ufs) == 0 {
		return []model.FileMeta{}, nil
	}

	hashes := make([]string, len(ufs))
	for i, uf := range ufs {
		hashes[i] = uf.FileSha1
	}

	var globalFiles []model.File
	if err := r.db.WithContext(ctx).Where("file_sha1 IN ?", hashes).Find(&globalFiles).Error; err != nil {
		return nil, fmt.Errorf("query global files failed: %w", err)
	}
	fileMap := make(map[string]model.File, len(globalFiles))
	for i := range globalFiles {
		fileMap[globalFiles[i].FileSha1] = globalFiles[i]
	}

	files := make([]model.FileMeta, 0, len(ufs))
	for _, uf := range ufs {
		f, ok := fileMap[uf.FileSha1]
		if !ok {
			slog.Warn("file record missing", "filehash", uf.FileSha1)
			continue
		}
		files = append(files, model.FileMeta{
			ID:       uf.ID,
			FileSha1: uf.FileSha1,
			FileName: uf.FileName,
			FileSize: f.FileSize,
			Username: uf.Username,
			ParentID: uf.ParentID,
			IsDir:    uf.IsDir,
			DirPath:  uf.DirPath,
			UploadAt: uf.CreateAt.Format("2006-01-02 15:04:05"),
			Summary:  f.Summary,
			Tags:     f.Tags,
		})
	}
	return files, nil
}

// CountByUser counts the total number of files for a user
func (r *mysqlFileRepo) CountByUser(ctx context.Context, username string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.UserFile{}).
		Where("user_name = ? AND status = ? AND is_dir = 0", username, model.UserFileStatusActive).
		Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("count user files failed: %w", err)
	}
	return count, nil
}

// ListByUserPaged paginates user files (batch query to avoid N+1)
func (r *mysqlFileRepo) ListByUserPaged(ctx context.Context, username string, page, size int) ([]model.FileMeta, error) {
	var ufs []model.UserFile
	offset := (page - 1) * size
	if err := r.db.WithContext(ctx).Where("user_name = ? AND status = ? AND is_dir = 0", username, model.UserFileStatusActive).
		Order("create_at DESC").
		Offset(offset).
		Limit(size).
		Find(&ufs).Error; err != nil {
		return nil, fmt.Errorf("paged query user files failed: %w", err)
	}
	if len(ufs) == 0 {
		return []model.FileMeta{}, nil
	}

	hashes := make([]string, len(ufs))
	for i, uf := range ufs {
		hashes[i] = uf.FileSha1
	}
	var globalFiles []model.File
	if err := r.db.WithContext(ctx).Where("file_sha1 IN ?", hashes).Find(&globalFiles).Error; err != nil {
		return nil, fmt.Errorf("query global files failed: %w", err)
	}
	fileMap := make(map[string]model.File, len(globalFiles))
	for i := range globalFiles {
		fileMap[globalFiles[i].FileSha1] = globalFiles[i]
	}

	files := make([]model.FileMeta, 0, len(ufs))
	for _, uf := range ufs {
		f, ok := fileMap[uf.FileSha1]
		if !ok {
			slog.Warn("file record missing", "filehash", uf.FileSha1)
			continue
		}
		files = append(files, model.FileMeta{
			ID:       uf.ID,
			FileSha1: uf.FileSha1,
			FileName: uf.FileName,
			FileSize: f.FileSize,
			Username: uf.Username,
			ParentID: uf.ParentID,
			IsDir:    uf.IsDir,
			DirPath:  uf.DirPath,
			UploadAt: uf.CreateAt.Format("2006-01-02 15:04:05"),
			Summary:  f.Summary,
			Tags:     f.Tags,
		})
	}
	return files, nil
}

// Delete soft-deletes a user file (status=UserFileStatusDeleted), only removing that user's ownership
func (r *mysqlFileRepo) Delete(ctx context.Context, filehash, username string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.UserFile{}).
		Where("file_sha1 = ? AND user_name = ? AND status = ?", filehash, username, model.UserFileStatusActive).
		Update("status", model.UserFileStatusDeleted)
	if res.Error != nil {
		return false, fmt.Errorf("delete user file failed: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// ListTrash paginates trashed files (status=2), batch query to avoid N+1
func (r *mysqlFileRepo) ListTrash(ctx context.Context, username string, page, size int) ([]model.FileMeta, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&model.UserFile{}).
		Where("user_name = ? AND status = ?", username, model.UserFileStatusDeleted).
		Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count trash failed: %w", err)
	}
	if total == 0 {
		return []model.FileMeta{}, 0, nil
	}

	var ufs []model.UserFile
	offset := (page - 1) * size
	if err := r.db.WithContext(ctx).Where("user_name = ? AND status = ?", username, model.UserFileStatusDeleted).
		Order("create_at DESC").
		Offset(offset).
		Limit(size).
		Find(&ufs).Error; err != nil {
		return nil, 0, fmt.Errorf("list trash failed: %w", err)
	}

	hashes := make([]string, 0, len(ufs))
	for _, uf := range ufs {
		if uf.IsDir == 0 && uf.FileSha1 != "" {
			hashes = append(hashes, uf.FileSha1)
		}
	}
	fileMap := make(map[string]model.File)
	if len(hashes) > 0 {
		var globalFiles []model.File
		if err := r.db.WithContext(ctx).Where("file_sha1 IN ?", hashes).Find(&globalFiles).Error; err == nil {
			for i := range globalFiles {
				fileMap[globalFiles[i].FileSha1] = globalFiles[i]
			}
		}
	}

	files := make([]model.FileMeta, 0, len(ufs))
	for _, uf := range ufs {
		fm := model.FileMeta{
			ID:       uf.ID,
			FileSha1: uf.FileSha1,
			FileName: uf.FileName,
			Username: uf.Username,
			ParentID: uf.ParentID,
			IsDir:    uf.IsDir,
			DirPath:  uf.DirPath,
			UploadAt: uf.CreateAt.Format("2006-01-02 15:04:05"),
		}
		if uf.IsDir == 0 {
			if f, ok := fileMap[uf.FileSha1]; ok {
				fm.FileSize = f.FileSize
				fm.Summary = f.Summary
				fm.Tags = f.Tags
			}
		}
		files = append(files, fm)
	}
	return files, total, nil
}

// Restore restores a soft-deleted file (status 2->1)
func (r *mysqlFileRepo) Restore(ctx context.Context, filehash, username string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.UserFile{}).
		Where("file_sha1 = ? AND user_name = ? AND status = ?", filehash, username, model.UserFileStatusDeleted).
		Update("status", model.UserFileStatusActive)
	if res.Error != nil {
		return false, fmt.Errorf("restore user file failed: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// PurgeUserFile permanently deletes the user file association
func (r *mysqlFileRepo) PurgeUserFile(ctx context.Context, filehash, username string) (bool, error) {
	res := r.db.WithContext(ctx).Where("file_sha1 = ? AND user_name = ?", filehash, username).
		Delete(&model.UserFile{})
	if res.Error != nil {
		return false, fmt.Errorf("purge user file failed: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// UpdateName updates the user file name
func (r *mysqlFileRepo) UpdateName(ctx context.Context, filehash, username, newFilename string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.UserFile{}).
		Where("file_sha1 = ? AND user_name = ? AND status = ?", filehash, username, model.UserFileStatusActive).
		Update("file_name", newFilename)
	if res.Error != nil {
		return false, fmt.Errorf("update file name failed: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// CountRefs counts active references to a file in tbl_user_file
func (r *mysqlFileRepo) CountRefs(ctx context.Context, filehash string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.UserFile{}).
		Where("file_sha1 = ? AND status = ? AND is_dir = 0", filehash, model.UserFileStatusActive).
		Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("count refs failed: %w", err)
	}
	return count, nil
}

// ListOldest lists global files created before 'before'
func (r *mysqlFileRepo) ListOldest(ctx context.Context, before time.Time) ([]model.File, error) {
	var files []model.File
	if err := r.db.WithContext(ctx).Where("create_at < ?", before).Find(&files).Error; err != nil {
		return nil, fmt.Errorf("list oldest files failed: %w", err)
	}
	return files, nil
}

// RemoveOrphan deletes orphan global file records from tbl_file
func (r *mysqlFileRepo) RemoveOrphan(ctx context.Context, filehash string) error {
	if err := r.db.WithContext(ctx).Where("file_sha1 = ?", filehash).Delete(&model.File{}).Error; err != nil {
		return fmt.Errorf("remove orphan file failed: %w", err)
	}
	return nil
}

// SaveAnalysis writes the AI-generated summary and tags (global file dimension)
func (r *mysqlFileRepo) SaveAnalysis(ctx context.Context, filehash, summary, tags string) error {
	res := r.db.WithContext(ctx).Model(&model.File{}).
		Where("file_sha1 = ?", filehash).
		Updates(map[string]any{"file_summary": summary, "tags": tags})
	if res.Error != nil {
		return fmt.Errorf("save file analysis failed: %w", res.Error)
	}
	return nil
}

// GetGlobalFile reads the global file by hash (including summary/tags), without user dimension
func (r *mysqlFileRepo) GetGlobalFile(ctx context.Context, filehash string) (model.File, error) {
	var f model.File
	if err := r.db.WithContext(ctx).Where("file_sha1 = ?", filehash).First(&f).Error; err != nil {
		return model.File{}, fmt.Errorf("get global file failed: %w", err)
	}
	return f, nil
}

// ---- VFS method implementations ----

func (r *mysqlFileRepo) GetUserFileByID(ctx context.Context, id uint, username string) (model.UserFile, error) {
	var uf model.UserFile
	db := r.db.WithContext(ctx).Where("id = ?", id)
	if username != "" {
		db = db.Where("user_name = ?", username)
	}
	if err := db.First(&uf).Error; err != nil {
		return model.UserFile{}, fmt.Errorf("get user file by id failed: %w", err)
	}
	return uf, nil
}

func (r *mysqlFileRepo) ListByParent(ctx context.Context, username string, parentID uint64, offset, limit int) ([]model.FileMeta, int64, error) {
	var total int64
	query := r.db.WithContext(ctx).Model(&model.UserFile{}).
		Where("user_name = ? AND parent_id = ? AND status = ?", username, parentID, model.UserFileStatusActive)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count files by parent failed: %w", err)
	}
	if total == 0 {
		return []model.FileMeta{}, 0, nil
	}

	var ufs []model.UserFile
	if err := query.Order("is_dir DESC, create_at DESC").Offset(offset).Limit(limit).Find(&ufs).Error; err != nil {
		return nil, 0, fmt.Errorf("list files by parent failed: %w", err)
	}

	hashes := make([]string, 0, len(ufs))
	for _, uf := range ufs {
		if uf.IsDir == 0 && uf.FileSha1 != "" {
			hashes = append(hashes, uf.FileSha1)
		}
	}

	fileMap := make(map[string]model.File)
	if len(hashes) > 0 {
		var globalFiles []model.File
		if err := r.db.WithContext(ctx).Where("file_sha1 IN ?", hashes).Find(&globalFiles).Error; err == nil {
			for i := range globalFiles {
				fileMap[globalFiles[i].FileSha1] = globalFiles[i]
			}
		}
	}

	files := make([]model.FileMeta, 0, len(ufs))
	for _, uf := range ufs {
		fm := model.FileMeta{
			ID:       uf.ID,
			FileSha1: uf.FileSha1,
			FileName: uf.FileName,
			Username: uf.Username,
			ParentID: uf.ParentID,
			IsDir:    uf.IsDir,
			DirPath:  uf.DirPath,
			UploadAt: uf.CreateAt.Format("2006-01-02 15:04:05"),
		}
		if uf.IsDir == 0 {
			if f, ok := fileMap[uf.FileSha1]; ok {
				fm.FileSize = f.FileSize
				fm.Summary = f.Summary
				fm.Tags = f.Tags
			}
		}
		files = append(files, fm)
	}
	return files, total, nil
}

func (r *mysqlFileRepo) CreateFolder(ctx context.Context, uf model.UserFile) (model.UserFile, error) {
	var created model.UserFile
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		uf.IsDir = 1
		uf.Status = model.UserFileStatusActive
		if uf.ParentID == 0 {
			uf.DirPath = "/" + uf.FileName + "/"
		} else {
			parentPath, err := lockMoveTarget(tx, uf.Username, uf.ParentID)
			if err != nil {
				return fmt.Errorf("parent folder unavailable: %w", err)
			}
			uf.DirPath = parentPath + uf.FileName + "/"
		}
		if err := tx.Create(&uf).Error; err != nil {
			return fmt.Errorf("create folder failed: %w", err)
		}
		created = uf
		return nil
	})
	if err != nil {
		return model.UserFile{}, err
	}
	return created, nil
}

func (r *mysqlFileRepo) MoveItem(ctx context.Context, id uint, username string, targetParentID uint64, newDirPath string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := lockActiveUserFile(tx, id, username)
		if err != nil {
			return fmt.Errorf("move item failed: %w", err)
		}

		targetPath, err := lockMoveTarget(tx, username, targetParentID)
		if err != nil {
			return fmt.Errorf("move item failed: %w", err)
		}
		if item.IsDir == 1 && vfsPathWithin(targetPath, item.DirPath) {
			return fmt.Errorf("move item failed: cannot move folder into its own subfolder")
		}

		expectedPath := targetPath
		if item.IsDir == 1 {
			expectedPath += item.FileName + "/"
		}
		if newDirPath != expectedPath {
			return fmt.Errorf("move item failed: inconsistent materialized path")
		}

		var subtree []model.UserFile
		if item.IsDir == 1 {
			subtree, err = lockPathItems(tx, username, item.DirPath)
			if err != nil {
				return fmt.Errorf("move item failed: %w", err)
			}
		}

		if item.ParentID != targetParentID || item.DirPath != expectedPath {
			res := tx.Model(&model.UserFile{}).
				Where("id = ? AND user_name = ? AND status = ?", id, username, model.UserFileStatusActive).
				Updates(map[string]any{"parent_id": targetParentID, "dir_path": expectedPath})
			if res.Error != nil {
				return fmt.Errorf("move item failed: %w", res.Error)
			}
			if res.RowsAffected != 1 {
				return fmt.Errorf("move item failed: item changed concurrently")
			}
		}
		if err := rewriteSubtreePaths(tx, username, item.DirPath, expectedPath, subtree, id); err != nil {
			return fmt.Errorf("move item failed: %w", err)
		}
		return nil
	})
}

func (r *mysqlFileRepo) UpdateDirPathPrefix(ctx context.Context, username, oldPrefix, newPrefix string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		items, err := lockPathItems(tx, username, oldPrefix)
		if err != nil {
			return fmt.Errorf("update dir path prefix failed: %w", err)
		}
		if len(items) == 0 {
			return fmt.Errorf("update dir path prefix failed: no matching path")
		}
		if err := rewriteSubtreePaths(tx, username, oldPrefix, newPrefix, items, 0); err != nil {
			return fmt.Errorf("update dir path prefix failed: %w", err)
		}
		return nil
	})
}

func (r *mysqlFileRepo) RenameItem(ctx context.Context, id uint, username, newName, newDirPath string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := lockActiveUserFile(tx, id, username)
		if err != nil {
			return fmt.Errorf("rename item failed: %w", err)
		}
		if item.IsDir == 1 && newDirPath == "" {
			return fmt.Errorf("rename item failed: missing materialized path")
		}
		if item.IsDir == 1 {
			expectedPath := strings.TrimSuffix(item.DirPath, item.FileName+"/") + newName + "/"
			if newDirPath != expectedPath {
				return fmt.Errorf("rename item failed: inconsistent materialized path")
			}
		} else if newDirPath != "" && newDirPath != item.DirPath {
			return fmt.Errorf("rename item failed: file path cannot change during rename")
		}

		var subtree []model.UserFile
		if item.IsDir == 1 {
			subtree, err = lockPathItems(tx, username, item.DirPath)
			if err != nil {
				return fmt.Errorf("rename item failed: %w", err)
			}
		}

		updates := map[string]any{"file_name": newName}
		if newDirPath != "" {
			updates["dir_path"] = newDirPath
		}
		if item.FileName != newName || (newDirPath != "" && item.DirPath != newDirPath) {
			res := tx.Model(&model.UserFile{}).
				Where("id = ? AND user_name = ? AND status = ?", id, username, model.UserFileStatusActive).
				Updates(updates)
			if res.Error != nil {
				return fmt.Errorf("rename item failed: %w", res.Error)
			}
			if res.RowsAffected != 1 {
				return fmt.Errorf("rename item failed: item changed concurrently")
			}
		}
		if item.IsDir == 1 {
			if err := rewriteSubtreePaths(tx, username, item.DirPath, newDirPath, subtree, id); err != nil {
				return fmt.Errorf("rename item failed: %w", err)
			}
		}
		return nil
	})
}

func (r *mysqlFileRepo) SoftDeleteDir(ctx context.Context, username, dirPath string) error {
	pattern := vfsPathLikePattern(dirPath)
	res := r.db.WithContext(ctx).Model(&model.UserFile{}).
		Where("user_name = ? AND (dir_path = ? OR dir_path LIKE ? ESCAPE '!') AND status = ?", username, dirPath, pattern, model.UserFileStatusActive).
		Update("status", model.UserFileStatusDeleted)
	if res.Error != nil {
		return fmt.Errorf("soft delete dir failed: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("soft delete dir failed: no matching active path")
	}
	return nil
}

func lockActiveUserFile(tx *gorm.DB, id uint, username string) (model.UserFile, error) {
	var item model.UserFile
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND user_name = ? AND status = ?", id, username, model.UserFileStatusActive).
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.UserFile{}, fmt.Errorf("item not found or no longer active")
	}
	if err != nil {
		return model.UserFile{}, err
	}
	return item, nil
}

func lockMoveTarget(tx *gorm.DB, username string, targetParentID uint64) (string, error) {
	if targetParentID == 0 {
		return "/", nil
	}
	var parent model.UserFile
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND user_name = ? AND is_dir = 1 AND status = ?", targetParentID, username, model.UserFileStatusActive).
		First(&parent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", fmt.Errorf("target folder not found or no longer active")
	}
	if err != nil {
		return "", err
	}
	return parent.DirPath, nil
}

func lockPathItems(tx *gorm.DB, username, oldPrefix string) ([]model.UserFile, error) {
	if oldPrefix == "" {
		return nil, fmt.Errorf("empty path prefix")
	}
	pattern := vfsPathLikePattern(oldPrefix)
	var items []model.UserFile
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_name = ? AND (dir_path = ? OR dir_path LIKE ? ESCAPE '!')", username, oldPrefix, pattern).
		Find(&items).Error
	if err != nil {
		return nil, err
	}
	filtered := items[:0]
	for _, item := range items {
		if vfsPathWithin(item.DirPath, oldPrefix) {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func rewriteSubtreePaths(tx *gorm.DB, username, oldPrefix, newPrefix string, items []model.UserFile, skipID uint) error {
	for _, item := range items {
		if item.ID == skipID || !vfsPathWithin(item.DirPath, oldPrefix) {
			continue
		}
		suffix := ""
		normalizedOldPrefix := vfsNormalizedPrefix(oldPrefix)
		if item.DirPath != oldPrefix && item.DirPath != normalizedOldPrefix {
			suffix = strings.TrimPrefix(item.DirPath, normalizedOldPrefix)
		}
		newPath := newPrefix + suffix
		if item.DirPath == newPrefix || item.DirPath == newPath {
			continue
		}
		res := tx.Model(&model.UserFile{}).
			Where("id = ? AND user_name = ?", item.ID, username).
			Update("dir_path", newPath)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return fmt.Errorf("path item %d changed concurrently", item.ID)
		}
	}
	return nil
}

func vfsNormalizedPrefix(prefix string) string {
	if prefix == "/" {
		return "/"
	}
	return strings.TrimSuffix(prefix, "/") + "/"
}

func vfsPathWithin(path, prefix string) bool {
	normalized := vfsNormalizedPrefix(prefix)
	return path == prefix || path == normalized || strings.HasPrefix(path, normalized)
}

func vfsPathLikePattern(prefix string) string {
	return escapeLikePattern(vfsNormalizedPrefix(prefix)) + "%"
}

func escapeLikePattern(value string) string {
	value = strings.ReplaceAll(value, `!`, `!!`)
	value = strings.ReplaceAll(value, `\`, `!\`)
	value = strings.ReplaceAll(value, `%`, `!%`)
	return strings.ReplaceAll(value, `_`, `!_`)
}

func (r *mysqlFileRepo) GetBreadcrumbs(ctx context.Context, username string, folderID uint64) ([]model.Breadcrumb, error) {
	var crumbs []model.Breadcrumb
	currID := folderID
	for currID != 0 {
		var uf model.UserFile
		if err := r.db.WithContext(ctx).Where("id = ? AND user_name = ? AND is_dir = 1", currID, username).First(&uf).Error; err != nil {
			break
		}
		crumbs = append([]model.Breadcrumb{{
			ID:   uint64(uf.ID),
			Name: uf.FileName,
			Path: uf.DirPath,
		}}, crumbs...)
		currID = uf.ParentID
	}
	crumbs = append([]model.Breadcrumb{{
		ID:   0,
		Name: "全部文件",
		Path: "/",
	}}, crumbs...)
	return crumbs, nil
}

// ---- Mock implementation ----

// mockFileRepo is an in-memory mock file repository
type mockFileRepo struct {
	mu        sync.RWMutex
	nextID    uint
	files     map[string]model.File        // key: filehash -> global file
	userFile  map[string]map[string]string // key: username -> filehash -> filename
	deleted   map[string]map[string]bool   // key: username -> filehash -> soft-deleted
	userItems map[string][]model.UserFile  // key: username -> list of all user files/directories
}

// NewMockFileRepository creates a mock file repository
func NewMockFileRepository() FileRepository {
	return &mockFileRepo{
		nextID:    1,
		files:     make(map[string]model.File),
		userFile:  make(map[string]map[string]string),
		deleted:   make(map[string]map[string]bool),
		userItems: make(map[string][]model.UserFile),
	}
}

func (m *mockFileRepo) Create(ctx context.Context, f model.File) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[f.FileSha1] = f
	return nil
}

func (m *mockFileRepo) CreateUserFile(ctx context.Context, uf model.UserFile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.userFile[uf.Username]; !ok {
		m.userFile[uf.Username] = make(map[string]string)
	}
	if uf.ID == 0 {
		uf.ID = m.nextID
		m.nextID++
	}
	if uf.DirPath == "" {
		uf.DirPath = "/"
	}
	if uf.Status == 0 {
		uf.Status = model.UserFileStatusActive
	}
	// Idempotent: ignore if already exists
	if _, exists := m.userFile[uf.Username][uf.FileSha1]; !exists {
		m.userFile[uf.Username][uf.FileSha1] = uf.FileName
		m.userItems[uf.Username] = append(m.userItems[uf.Username], uf)
	}
	return nil
}

func (m *mockFileRepo) GetByHash(ctx context.Context, filehash, username string) (model.FileMeta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	name, ok := m.userFile[username][filehash]
	if !ok || name == "" && !m.userHasLocked(username, filehash) {
		return model.FileMeta{}, fmt.Errorf("file not found")
	}
	if m.deleted[username][filehash] {
		return model.FileMeta{}, fmt.Errorf("file not found")
	}
	f, ok := m.files[filehash]
	if !ok {
		return model.FileMeta{}, fmt.Errorf("file not found")
	}
	return model.FileMeta{
		FileSha1: filehash,
		FileName: name,
		FileSize: f.FileSize,
		Username: username,
	}, nil
}

func (m *mockFileRepo) userHasLocked(username, filehash string) bool {
	_, ok := m.userFile[username][filehash]
	return ok
}

func (m *mockFileRepo) ListByUser(ctx context.Context, username string) ([]model.FileMeta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []model.FileMeta
	names := m.userFile[username]
	for hash, name := range names {
		if m.deleted[username][hash] {
			continue
		}
		f, ok := m.files[hash]
		if !ok {
			continue
		}
		result = append(result, model.FileMeta{
			FileSha1: hash,
			FileName: name,
			FileSize: f.FileSize,
			Username: username,
		})
	}
	return result, nil
}

func (m *mockFileRepo) CountByUser(ctx context.Context, username string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for hash := range m.userFile[username] {
		if !m.deleted[username][hash] {
			n++
		}
	}
	return int64(n), nil
}

func (m *mockFileRepo) ListByUserPaged(ctx context.Context, username string, page, size int) ([]model.FileMeta, error) {
	all, _ := m.ListByUser(ctx, username)
	start := (page - 1) * size
	if start >= len(all) {
		return []model.FileMeta{}, nil
	}
	end := start + size
	if end > len(all) {
		end = len(all)
	}
	return all[start:end], nil
}

func (m *mockFileRepo) Delete(ctx context.Context, filehash, username string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.userHasLocked(username, filehash) {
		return false, nil
	}
	if m.deleted[username] == nil {
		m.deleted[username] = make(map[string]bool)
	}
	m.deleted[username][filehash] = true
	return true, nil
}

func (m *mockFileRepo) ListTrash(ctx context.Context, username string, page, size int) ([]model.FileMeta, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var trash []model.FileMeta
	for hash := range m.deleted[username] {
		f, ok := m.files[hash]
		if !ok {
			continue
		}
		trash = append(trash, model.FileMeta{
			FileSha1: hash,
			FileName: m.userFile[username][hash],
			FileSize: f.FileSize,
			Username: username,
		})
	}
	total := int64(len(trash))
	start := (page - 1) * size
	if start >= len(trash) {
		return []model.FileMeta{}, total, nil
	}
	end := start + size
	if end > len(trash) {
		end = len(trash)
	}
	return trash[start:end], total, nil
}

func (m *mockFileRepo) Restore(ctx context.Context, filehash, username string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.deleted[username][filehash] {
		return false, nil
	}
	delete(m.deleted[username], filehash)
	return true, nil
}

func (m *mockFileRepo) PurgeUserFile(ctx context.Context, filehash, username string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.userHasLocked(username, filehash) {
		return false, nil
	}
	delete(m.userFile[username], filehash)
	if m.deleted[username] != nil {
		delete(m.deleted[username], filehash)
	}
	return true, nil
}

func (m *mockFileRepo) UpdateName(ctx context.Context, filehash, username, newFilename string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.userHasLocked(username, filehash) {
		return false, nil
	}
	m.userFile[username][filehash] = newFilename
	return true, nil
}

func (m *mockFileRepo) CountRefs(ctx context.Context, filehash string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var count int64
	for _, names := range m.userFile {
		if _, ok := names[filehash]; ok {
			count++
		}
	}
	return count, nil
}

func (m *mockFileRepo) ListOldest(ctx context.Context, before time.Time) ([]model.File, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []model.File
	for _, f := range m.files {
		if f.CreateAt.Before(before) {
			result = append(result, f)
		}
	}
	return result, nil
}

func (m *mockFileRepo) RemoveOrphan(ctx context.Context, filehash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, filehash)
	return nil
}

func (m *mockFileRepo) SaveAnalysis(ctx context.Context, filehash, summary, tags string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.files[filehash]
	f.Summary = summary
	f.Tags = tags
	m.files[filehash] = f
	return nil
}

func (m *mockFileRepo) GetGlobalFile(ctx context.Context, filehash string) (model.File, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	f, ok := m.files[filehash]
	if !ok {
		return model.File{}, fmt.Errorf("file not found")
	}
	return f, nil
}

// Mock VFS methods
func (m *mockFileRepo) GetUserFileByID(ctx context.Context, id uint, username string) (model.UserFile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, item := range m.userItems[username] {
		if item.ID == id {
			return item, nil
		}
	}
	return model.UserFile{}, fmt.Errorf("record not found")
}

func (m *mockFileRepo) ListByParent(ctx context.Context, username string, parentID uint64, offset, limit int) ([]model.FileMeta, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []model.FileMeta
	for _, item := range m.userItems[username] {
		if item.ParentID == parentID && item.Status == model.UserFileStatusActive {
			var size int64
			var summary, tags string
			if item.IsDir == 0 {
				if gf, ok := m.files[item.FileSha1]; ok {
					size = gf.FileSize
					summary = gf.Summary
					tags = gf.Tags
				}
			}
			list = append(list, model.FileMeta{
				ID:       item.ID,
				FileSha1: item.FileSha1,
				FileName: item.FileName,
				FileSize: size,
				Username: item.Username,
				ParentID: item.ParentID,
				IsDir:    item.IsDir,
				DirPath:  item.DirPath,
				UploadAt: item.CreateAt.Format("2006-01-02 15:04:05"),
				Summary:  summary,
				Tags:     tags,
			})
		}
	}
	total := int64(len(list))
	if offset >= len(list) {
		return []model.FileMeta{}, total, nil
	}
	end := offset + limit
	if end > len(list) {
		end = len(list)
	}
	return list[offset:end], total, nil
}

func (m *mockFileRepo) CreateFolder(ctx context.Context, uf model.UserFile) (model.UserFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	uf.ID = m.nextID
	m.nextID++
	uf.IsDir = 1
	uf.Status = model.UserFileStatusActive
	if uf.DirPath == "" {
		uf.DirPath = "/"
	}
	m.userItems[uf.Username] = append(m.userItems[uf.Username], uf)
	return uf, nil
}

func (m *mockFileRepo) MoveItem(ctx context.Context, id uint, username string, targetParentID uint64, newDirPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := m.userItems[username]
	itemIndex := -1
	for i := range items {
		if items[i].ID == id {
			if items[i].Status != model.UserFileStatusActive {
				return fmt.Errorf("item not found or no longer active")
			}
			itemIndex = i
			break
		}
	}
	if itemIndex < 0 {
		return fmt.Errorf("item not found")
	}
	item := items[itemIndex]
	targetPath := "/"
	if targetParentID != 0 {
		found := false
		for _, parent := range items {
			if uint64(parent.ID) == targetParentID && parent.IsDir == 1 && parent.Status == model.UserFileStatusActive {
				targetPath = parent.DirPath
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("target folder not found or no longer active")
		}
	}
	if item.IsDir == 1 && vfsPathWithin(targetPath, item.DirPath) {
		return fmt.Errorf("cannot move folder into its own subfolder")
	}
	expectedPath := targetPath
	if item.IsDir == 1 {
		expectedPath += item.FileName + "/"
	}
	if newDirPath != expectedPath {
		return fmt.Errorf("inconsistent materialized path")
	}
	oldPath := item.DirPath
	items[itemIndex].ParentID = targetParentID
	items[itemIndex].DirPath = expectedPath
	for i := range items {
		if items[i].ID == id || !vfsPathWithin(items[i].DirPath, oldPath) {
			continue
		}
		suffix := ""
		if items[i].DirPath != oldPath && items[i].DirPath != vfsNormalizedPrefix(oldPath) {
			suffix = strings.TrimPrefix(items[i].DirPath, vfsNormalizedPrefix(oldPath))
		}
		items[i].DirPath = expectedPath + suffix
	}
	m.userItems[username] = items
	return nil
}

func (m *mockFileRepo) UpdateDirPathPrefix(ctx context.Context, username, oldPrefix, newPrefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := m.userItems[username]
	matched := 0
	for i := range items {
		if vfsPathWithin(items[i].DirPath, oldPrefix) {
			matched++
			suffix := ""
			if items[i].DirPath != oldPrefix && items[i].DirPath != vfsNormalizedPrefix(oldPrefix) {
				suffix = strings.TrimPrefix(items[i].DirPath, vfsNormalizedPrefix(oldPrefix))
			}
			items[i].DirPath = newPrefix + suffix
		}
	}
	if matched == 0 {
		return fmt.Errorf("no matching path")
	}
	return nil
}

func (m *mockFileRepo) RenameItem(ctx context.Context, id uint, username, newName, newDirPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := m.userItems[username]
	itemIndex := -1
	for i := range items {
		if items[i].ID == id {
			if items[i].Status != model.UserFileStatusActive {
				return fmt.Errorf("item not found or no longer active")
			}
			itemIndex = i
			break
		}
	}
	if itemIndex < 0 {
		return fmt.Errorf("item not found")
	}
	item := items[itemIndex]
	if item.IsDir == 1 {
		if newDirPath == "" {
			return fmt.Errorf("missing materialized path")
		}
		expectedPath := strings.TrimSuffix(item.DirPath, item.FileName+"/") + newName + "/"
		if newDirPath != expectedPath {
			return fmt.Errorf("inconsistent materialized path")
		}
		oldPath := item.DirPath
		items[itemIndex].FileName = newName
		items[itemIndex].DirPath = newDirPath
		for i := range items {
			if items[i].ID == id || !vfsPathWithin(items[i].DirPath, oldPath) {
				continue
			}
			suffix := ""
			if items[i].DirPath != oldPath && items[i].DirPath != vfsNormalizedPrefix(oldPath) {
				suffix = strings.TrimPrefix(items[i].DirPath, vfsNormalizedPrefix(oldPath))
			}
			items[i].DirPath = newDirPath + suffix
		}
	} else {
		if newDirPath != "" && newDirPath != item.DirPath {
			return fmt.Errorf("file path cannot change during rename")
		}
		items[itemIndex].FileName = newName
		if newDirPath != "" {
			items[itemIndex].DirPath = newDirPath
		}
	}
	m.userItems[username] = items
	return nil
}

func (m *mockFileRepo) SoftDeleteDir(ctx context.Context, username, dirPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := m.userItems[username]
	matched := 0
	for i := range items {
		if vfsPathWithin(items[i].DirPath, dirPath) && items[i].Status == model.UserFileStatusActive {
			items[i].Status = model.UserFileStatusDeleted
			matched++
		}
	}
	if matched == 0 {
		return fmt.Errorf("no matching active path")
	}
	return nil
}

func (m *mockFileRepo) GetBreadcrumbs(ctx context.Context, username string, folderID uint64) ([]model.Breadcrumb, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var crumbs []model.Breadcrumb
	currID := folderID
	for currID != 0 {
		var found *model.UserFile
		for _, it := range m.userItems[username] {
			if uint64(it.ID) == currID && it.IsDir == 1 {
				found = &it
				break
			}
		}
		if found == nil {
			break
		}
		crumbs = append([]model.Breadcrumb{{
			ID:   uint64(found.ID),
			Name: found.FileName,
			Path: found.DirPath,
		}}, crumbs...)
		currID = found.ParentID
	}
	crumbs = append([]model.Breadcrumb{{
		ID:   0,
		Name: "全部文件",
		Path: "/",
	}}, crumbs...)
	return crumbs, nil
}

// Ensure interface implementation is checked at compile time
var _ FileRepository = (*mysqlFileRepo)(nil)
var _ FileRepository = (*mockFileRepo)(nil)
