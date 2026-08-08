package repository

import (
	"fmt"
	"gofile/model"
	"log/slog"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FileRepository 文件数据访问接口
type FileRepository interface {
	// Create 注册全局文件（按 SHA1 去重，已存在则忽略）
	Create(f model.File) error
	// CreateUserFile 建立用户文件拥有关系（幂等）
	CreateUserFile(uf model.UserFile) error
	// GetByHash 按 hash 和用户名获取文件元信息
	GetByHash(filehash, username string) (model.FileMeta, error)
	// ListByUser 获取用户的所有文件
	ListByUser(username string) ([]model.FileMeta, error)
	// Delete 软删除用户文件（status=2）
	Delete(filehash, username string) (bool, error)
	// UpdateName 更新用户文件名
	UpdateName(filehash, username, newFilename string) (bool, error)
	// CountRefs 统计某文件在 tbl_user_file 中的活跃引用数
	CountRefs(filehash string) (int64, error)
	// ListOldest 列出创建时间早于 before 的全局文件（GC 候选）
	ListOldest(before time.Time) ([]model.File, error)
	// RemoveOrphan 从 tbl_file 删除无引用的全局文件记录
	RemoveOrphan(filehash string) error
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
func (r *mysqlFileRepo) Create(f model.File) error {
	// OnConflict DoNothing 等价于 INSERT IGNORE
	if err := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&f).Error; err != nil {
		return fmt.Errorf("create file failed: %w", err)
	}
	return nil
}

// CreateUserFile 建立用户文件拥有关系，已存在则忽略（幂等）
func (r *mysqlFileRepo) CreateUserFile(uf model.UserFile) error {
	if err := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&uf).Error; err != nil {
		return fmt.Errorf("create user file failed: %w", err)
	}
	return nil
}

// GetByHash 查询用户拥有的某个文件（JOIN tbl_file）
func (r *mysqlFileRepo) GetByHash(filehash, username string) (model.FileMeta, error) {
	var uf model.UserFile
	if err := r.db.Where("file_sha1 = ? AND user_name = ? AND status = 1", filehash, username).
		First(&uf).Error; err != nil {
		return model.FileMeta{}, fmt.Errorf("get user file failed: %w", err)
	}

	var f model.File
	if err := r.db.Where("file_sha1 = ?", filehash).First(&f).Error; err != nil {
		return model.FileMeta{}, fmt.Errorf("get file failed: %w", err)
	}

	return model.FileMeta{
		FileSha1: uf.FileSha1,
		FileName: uf.FileName,
		FileSize: f.FileSize,
		Username: uf.Username,
		UploadAt: uf.CreateAt.Format("2006-01-02 15:04:05"),
	}, nil
}

// ListByUser 查询用户的所有文件
func (r *mysqlFileRepo) ListByUser(username string) ([]model.FileMeta, error) {
	var ufs []model.UserFile
	if err := r.db.Where("user_name = ? AND status = 1", username).
		Order("create_at DESC").
		Find(&ufs).Error; err != nil {
		return nil, fmt.Errorf("query user files failed: %w", err)
	}

	files := make([]model.FileMeta, 0, len(ufs))
	for _, uf := range ufs {
		var f model.File
		if err := r.db.Where("file_sha1 = ?", uf.FileSha1).First(&f).Error; err != nil {
			slog.Warn("file record missing", "filehash", uf.FileSha1)
			continue
		}
		files = append(files, model.FileMeta{
			FileSha1: uf.FileSha1,
			FileName: uf.FileName,
			FileSize: f.FileSize,
			Username: uf.Username,
			UploadAt: uf.CreateAt.Format("2006-01-02 15:04:05"),
		})
	}
	return files, nil
}

// Delete 软删除用户文件（status=2），仅删除该用户的所有权
func (r *mysqlFileRepo) Delete(filehash, username string) (bool, error) {
	res := r.db.Model(&model.UserFile{}).
		Where("file_sha1 = ? AND user_name = ? AND status = 1", filehash, username).
		Update("status", 2)
	if res.Error != nil {
		return false, fmt.Errorf("delete user file failed: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// UpdateName 更新用户文件名
func (r *mysqlFileRepo) UpdateName(filehash, username, newFilename string) (bool, error) {
	res := r.db.Model(&model.UserFile{}).
		Where("file_sha1 = ? AND user_name = ? AND status = 1", filehash, username).
		Update("file_name", newFilename)
	if res.Error != nil {
		return false, fmt.Errorf("update file name failed: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// CountRefs 统计某文件在 tbl_user_file 中的活跃引用数
func (r *mysqlFileRepo) CountRefs(filehash string) (int64, error) {
	var count int64
	err := r.db.Model(&model.UserFile{}).
		Where("file_sha1 = ? AND status = 1", filehash).
		Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("count refs failed: %w", err)
	}
	return count, nil
}

// ListOldest 列出创建时间早于 before 的全局文件
func (r *mysqlFileRepo) ListOldest(before time.Time) ([]model.File, error) {
	var files []model.File
	if err := r.db.Where("create_at < ?", before).Find(&files).Error; err != nil {
		return nil, fmt.Errorf("list oldest files failed: %w", err)
	}
	return files, nil
}

// RemoveOrphan 从 tbl_file 删除无引用的全局文件记录
func (r *mysqlFileRepo) RemoveOrphan(filehash string) error {
	if err := r.db.Where("file_sha1 = ?", filehash).Delete(&model.File{}).Error; err != nil {
		return fmt.Errorf("remove orphan file failed: %w", err)
	}
	return nil
}

// ---- Mock 实现 ----

// mockFileRepo 内存 mock 文件仓库
type mockFileRepo struct {
	files    map[string]model.File         // key: filehash -> 全局文件
	userFile map[string]map[string]string  // key: username -> filehash -> filename
}

// NewMockFileRepository 创建 mock 文件仓库
func NewMockFileRepository() FileRepository {
	return &mockFileRepo{
		files:    make(map[string]model.File),
		userFile: make(map[string]map[string]string),
	}
}

func (m *mockFileRepo) Create(f model.File) error {
	m.files[f.FileSha1] = f
	return nil
}

func (m *mockFileRepo) CreateUserFile(uf model.UserFile) error {
	if _, ok := m.userFile[uf.Username]; !ok {
		m.userFile[uf.Username] = make(map[string]string)
	}
	// 幂等：已存在则忽略
	if _, exists := m.userFile[uf.Username][uf.FileSha1]; !exists {
		m.userFile[uf.Username][uf.FileSha1] = uf.FileName
	}
	return nil
}

func (m *mockFileRepo) GetByHash(filehash, username string) (model.FileMeta, error) {
	name, ok := m.userFile[username][filehash]
	if !ok || name == "" && !m.userHas(username, filehash) {
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

func (m *mockFileRepo) userHas(username, filehash string) bool {
	_, ok := m.userFile[username][filehash]
	return ok
}

func (m *mockFileRepo) ListByUser(username string) ([]model.FileMeta, error) {
	var result []model.FileMeta
	names := m.userFile[username]
	for hash, name := range names {
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

func (m *mockFileRepo) Delete(filehash, username string) (bool, error) {
	if m.userHas(username, filehash) {
		delete(m.userFile[username], filehash)
		return true, nil
	}
	return false, nil
}

func (m *mockFileRepo) UpdateName(filehash, username, newFilename string) (bool, error) {
	if !m.userHas(username, filehash) {
		return false, nil
	}
	m.userFile[username][filehash] = newFilename
	return true, nil
}

func (m *mockFileRepo) CountRefs(filehash string) (int64, error) {
	var count int64
	for _, names := range m.userFile {
		if _, ok := names[filehash]; ok {
			count++
		}
	}
	return count, nil
}

func (m *mockFileRepo) ListOldest(before time.Time) ([]model.File, error) {
	var result []model.File
	for _, f := range m.files {
		if f.CreateAt.Before(before) {
			result = append(result, f)
		}
	}
	return result, nil
}

func (m *mockFileRepo) RemoveOrphan(filehash string) error {
	delete(m.files, filehash)
	return nil
}

// 确保编译时检查接口实现
var _ FileRepository = (*mysqlFileRepo)(nil)
var _ FileRepository = (*mockFileRepo)(nil)