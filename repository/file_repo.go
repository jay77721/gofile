package repository

import (
	"context"
	"fmt"
	"gofile/model"
	"log/slog"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FileRepository 文件数据访问接口
type FileRepository interface {
	// Create 注册全局文件（按 SHA1 去重，已存在则忽略）
	Create(ctx context.Context, f model.File) error
	// CreateUserFile 建立用户文件拥有关系（幂等）
	CreateUserFile(ctx context.Context, uf model.UserFile) error
	// GetByHash 按 hash 和用户名获取文件元信息
	GetByHash(ctx context.Context, filehash, username string) (model.FileMeta, error)
	// ListByUser 获取用户的所有文件
	ListByUser(ctx context.Context, username string) ([]model.FileMeta, error)
	// CountByUser 统计用户文件总数
	CountByUser(ctx context.Context, username string) (int64, error)
	// ListTrash 分页查询用户回收站文件（status=2）
	ListTrash(ctx context.Context, username string, page, size int) ([]model.FileMeta, int64, error)
	// Restore 恢复软删除文件（status 2→1）
	Restore(ctx context.Context, filehash, username string) (bool, error)
	// PurgeUserFile 彻底删除用户文件关联行
	PurgeUserFile(ctx context.Context, filehash, username string) (bool, error)
	// ListByUserPaged 分页查询用户文件（批量查询避免 N+1）
	ListByUserPaged(ctx context.Context, username string, page, size int) ([]model.FileMeta, error)
	// Delete 软删除用户文件（status=2）
	Delete(ctx context.Context, filehash, username string) (bool, error)
	// UpdateName 更新用户文件名
	UpdateName(ctx context.Context, filehash, username, newFilename string) (bool, error)
	// CountRefs 统计某文件在 tbl_user_file 中的活跃引用数
	CountRefs(ctx context.Context, filehash string) (int64, error)
	// ListOldest 列出创建时间早于 before 的全局文件（GC 候选）
	ListOldest(ctx context.Context, before time.Time) ([]model.File, error)
	// RemoveOrphan 从 tbl_file 删除无引用的全局文件记录
	RemoveOrphan(ctx context.Context, filehash string) error
	// SaveAnalysis 写入 AI 生成的摘要与标签（全局文件维度，幂等）
	SaveAnalysis(ctx context.Context, filehash, summary, tags string) error
	// GetGlobalFile 按 hash 读取全局文件（含摘要/标签），不带用户维度
	GetGlobalFile(ctx context.Context, filehash string) (model.File, error)

	// VFS 虚拟文件系统扩展接口
	GetUserFileByID(ctx context.Context, id uint, username string) (model.UserFile, error)
	ListByParent(ctx context.Context, username string, parentID uint64, offset, limit int) ([]model.FileMeta, int64, error)
	CreateFolder(ctx context.Context, uf model.UserFile) (model.UserFile, error)
	MoveItem(ctx context.Context, id uint, username string, targetParentID uint64, newDirPath string) error
	UpdateDirPathPrefix(ctx context.Context, username, oldPrefix, newPrefix string) error
	RenameItem(ctx context.Context, id uint, username, newName, newDirPath string) error
	SoftDeleteDir(ctx context.Context, username, dirPath string) error
	GetBreadcrumbs(ctx context.Context, username string, folderID uint64) ([]model.Breadcrumb, error)
}

// mysqlFileRepo GORM 实现的 FileRepository
type mysqlFileRepo struct {
	db *gorm.DB
}

// NewFileRepository 创建 GORM 文件仓库
func NewFileRepository(db *gorm.DB) FileRepository {
	return &mysqlFileRepo{db: db}
}

// Create 注册全局文件，已存在则忽略（幂等）
func (r *mysqlFileRepo) Create(ctx context.Context, f model.File) error {
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&f).Error; err != nil {
		return fmt.Errorf("create file failed: %w", err)
	}
	return nil
}

// CreateUserFile 建立用户文件拥有关系，已存在则忽略（幂等）
func (r *mysqlFileRepo) CreateUserFile(ctx context.Context, uf model.UserFile) error {
	if uf.DirPath == "" {
		uf.DirPath = "/"
	}
	// 幂等防重检查
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

// GetByHash 查询用户拥有的某个文件（JOIN tbl_file）
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

// ListByUser 查询用户的所有活跃文件（批量查询避免 N+1）
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

// CountByUser 统计用户文件总数
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

// ListByUserPaged 分页查询用户文件（批量查询避免 N+1）
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

// Delete 软删除用户文件（status=UserFileStatusDeleted），仅删除该用户的所有权
func (r *mysqlFileRepo) Delete(ctx context.Context, filehash, username string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.UserFile{}).
		Where("file_sha1 = ? AND user_name = ? AND status = ?", filehash, username, model.UserFileStatusActive).
		Update("status", model.UserFileStatusDeleted)
	if res.Error != nil {
		return false, fmt.Errorf("delete user file failed: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// ListTrash 分页查询回收站文件（status=2），批量查询避免 N+1
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

// Restore 恢复软删除文件（status 2→1）
func (r *mysqlFileRepo) Restore(ctx context.Context, filehash, username string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.UserFile{}).
		Where("file_sha1 = ? AND user_name = ? AND status = ?", filehash, username, model.UserFileStatusDeleted).
		Update("status", model.UserFileStatusActive)
	if res.Error != nil {
		return false, fmt.Errorf("restore user file failed: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// PurgeUserFile 彻底删除用户文件关联行
func (r *mysqlFileRepo) PurgeUserFile(ctx context.Context, filehash, username string) (bool, error) {
	res := r.db.WithContext(ctx).Where("file_sha1 = ? AND user_name = ?", filehash, username).
		Delete(&model.UserFile{})
	if res.Error != nil {
		return false, fmt.Errorf("purge user file failed: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// UpdateName 更新用户文件名
func (r *mysqlFileRepo) UpdateName(ctx context.Context, filehash, username, newFilename string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.UserFile{}).
		Where("file_sha1 = ? AND user_name = ? AND status = ?", filehash, username, model.UserFileStatusActive).
		Update("file_name", newFilename)
	if res.Error != nil {
		return false, fmt.Errorf("update file name failed: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// CountRefs 统计某文件在 tbl_user_file 中的活跃引用数
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

// ListOldest 列出创建时间早于 before 的全局文件
func (r *mysqlFileRepo) ListOldest(ctx context.Context, before time.Time) ([]model.File, error) {
	var files []model.File
	if err := r.db.WithContext(ctx).Where("create_at < ?", before).Find(&files).Error; err != nil {
		return nil, fmt.Errorf("list oldest files failed: %w", err)
	}
	return files, nil
}

// RemoveOrphan 从 tbl_file 删除无引用的全局文件记录
func (r *mysqlFileRepo) RemoveOrphan(ctx context.Context, filehash string) error {
	if err := r.db.WithContext(ctx).Where("file_sha1 = ?", filehash).Delete(&model.File{}).Error; err != nil {
		return fmt.Errorf("remove orphan file failed: %w", err)
	}
	return nil
}

// SaveAnalysis 写入 AI 生成的摘要与标签（全局文件维度）
func (r *mysqlFileRepo) SaveAnalysis(ctx context.Context, filehash, summary, tags string) error {
	res := r.db.WithContext(ctx).Model(&model.File{}).
		Where("file_sha1 = ?", filehash).
		Updates(map[string]any{"file_summary": summary, "tags": tags})
	if res.Error != nil {
		return fmt.Errorf("save file analysis failed: %w", res.Error)
	}
	return nil
}

// GetGlobalFile 按 hash 读取全局文件（含摘要/标签），不带用户维度
func (r *mysqlFileRepo) GetGlobalFile(ctx context.Context, filehash string) (model.File, error) {
	var f model.File
	if err := r.db.WithContext(ctx).Where("file_sha1 = ?", filehash).First(&f).Error; err != nil {
		return model.File{}, fmt.Errorf("get global file failed: %w", err)
	}
	return f, nil
}

// ---- VFS 方法实现 ----

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
	uf.IsDir = 1
	uf.Status = model.UserFileStatusActive
	if uf.DirPath == "" {
		uf.DirPath = "/"
	}
	if err := r.db.WithContext(ctx).Create(&uf).Error; err != nil {
		return model.UserFile{}, fmt.Errorf("create folder failed: %w", err)
	}
	return uf, nil
}

func (r *mysqlFileRepo) MoveItem(ctx context.Context, id uint, username string, targetParentID uint64, newDirPath string) error {
	updates := map[string]any{
		"parent_id": targetParentID,
		"dir_path":  newDirPath,
	}
	res := r.db.WithContext(ctx).Model(&model.UserFile{}).
		Where("id = ? AND user_name = ?", id, username).
		Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("move item failed: %w", res.Error)
	}
	return nil
}

func (r *mysqlFileRepo) UpdateDirPathPrefix(ctx context.Context, username, oldPrefix, newPrefix string) error {
	dialect := r.db.Dialector.Name()
	oldLenPlusOne := len(oldPrefix) + 1
	likePattern := oldPrefix + "%"
	var sql string
	if dialect == "sqlite" {
		sql = "UPDATE tbl_user_file SET dir_path = ? || SUBSTR(dir_path, ?) WHERE user_name = ? AND (dir_path = ? OR dir_path LIKE ?)"
	} else {
		sql = "UPDATE tbl_user_file SET dir_path = CONCAT(?, SUBSTRING(dir_path, ?)) WHERE user_name = ? AND (dir_path = ? OR dir_path LIKE ?)"
	}
	if err := r.db.WithContext(ctx).Exec(sql, newPrefix, oldLenPlusOne, username, oldPrefix, likePattern).Error; err != nil {
		return fmt.Errorf("update dir path prefix failed: %w", err)
	}
	return nil
}

func (r *mysqlFileRepo) RenameItem(ctx context.Context, id uint, username, newName, newDirPath string) error {
	updates := map[string]any{
		"file_name": newName,
	}
	if newDirPath != "" {
		updates["dir_path"] = newDirPath
	}
	res := r.db.WithContext(ctx).Model(&model.UserFile{}).
		Where("id = ? AND user_name = ?", id, username).
		Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("rename item failed: %w", res.Error)
	}
	return nil
}

func (r *mysqlFileRepo) SoftDeleteDir(ctx context.Context, username, dirPath string) error {
	likePattern := dirPath + "%"
	res := r.db.WithContext(ctx).Model(&model.UserFile{}).
		Where("user_name = ? AND (dir_path = ? OR dir_path LIKE ?) AND status = ?", username, dirPath, likePattern, model.UserFileStatusActive).
		Update("status", model.UserFileStatusDeleted)
	if res.Error != nil {
		return fmt.Errorf("soft delete dir failed: %w", res.Error)
	}
	return nil
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

// ---- Mock 实现 ----

// mockFileRepo 内存 mock 文件仓库
type mockFileRepo struct {
	mu        sync.RWMutex
	nextID    uint
	files     map[string]model.File        // key: filehash -> 全局文件
	userFile  map[string]map[string]string // key: username -> filehash -> filename
	deleted   map[string]map[string]bool   // key: username -> filehash -> 已软删除
	userItems map[string][]model.UserFile  // key: username -> 用户所有文件/目录列表
}

// NewMockFileRepository 创建 mock 文件仓库
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
	// 幂等：已存在则忽略
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
	for i := range items {
		if items[i].ID == id {
			items[i].ParentID = targetParentID
			items[i].DirPath = newDirPath
			return nil
		}
	}
	return fmt.Errorf("item not found")
}

func (m *mockFileRepo) UpdateDirPathPrefix(ctx context.Context, username, oldPrefix, newPrefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := m.userItems[username]
	for i := range items {
		if strings.HasPrefix(items[i].DirPath, oldPrefix) {
			items[i].DirPath = newPrefix + strings.TrimPrefix(items[i].DirPath, oldPrefix)
		}
	}
	return nil
}

func (m *mockFileRepo) RenameItem(ctx context.Context, id uint, username, newName, newDirPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := m.userItems[username]
	for i := range items {
		if items[i].ID == id {
			items[i].FileName = newName
			if newDirPath != "" {
				items[i].DirPath = newDirPath
			}
			return nil
		}
	}
	return fmt.Errorf("item not found")
}

func (m *mockFileRepo) SoftDeleteDir(ctx context.Context, username, dirPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := m.userItems[username]
	for i := range items {
		if strings.HasPrefix(items[i].DirPath, dirPath) {
			items[i].Status = model.UserFileStatusDeleted
		}
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

// 确保编译时检查接口实现
var _ FileRepository = (*mysqlFileRepo)(nil)
var _ FileRepository = (*mockFileRepo)(nil)
