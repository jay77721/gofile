package repository

import (
	"database/sql"
	"fmt"
	"gofile/model"
	"log/slog"
)

// FileRepository 文件数据访问接口
type FileRepository interface {
	// Create 创建文件元信息
	Create(f model.FileMeta) error
	// GetByHash 按 hash 和用户名获取文件元信息
	GetByHash(filehash, username string) (model.FileMeta, error)
	// ListByUser 获取用户的所有文件
	ListByUser(username string) ([]model.FileMeta, error)
	// Delete 软删除文件（status=2）
	Delete(filehash string) (bool, error)
	// UpdateName 更新文件名
	UpdateName(filehash, newFilename string) (bool, error)
}

// mysqlFileRepo MySQL 实现的 FileRepository
type mysqlFileRepo struct {
	db *sql.DB
}

// NewFileRepository 创建 MySQL 文件仓库
func NewFileRepository(db *sql.DB) FileRepository {
	return &mysqlFileRepo{db: db}
}

func (r *mysqlFileRepo) Create(f model.FileMeta) error {
	stmt, err := r.db.Prepare(
		"INSERT IGNORE INTO tbl_file(`file_sha1`,`user_name`,`file_name`,`file_size`,`file_addr`,`create_at`,status) VALUES(?,?,?,?,?,?,1)")
	if err != nil {
		return fmt.Errorf("prepare insert file failed: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(f.FileSha1, f.Username, f.FileName, f.FileSize, f.Location, f.UploadAt)
	if err != nil {
		return fmt.Errorf("exec insert file failed: %w", err)
	}
	return nil
}

func (r *mysqlFileRepo) GetByHash(filehash, username string) (model.FileMeta, error) {
	row := r.db.QueryRow(
		"SELECT file_sha1, user_name, file_name, file_size, file_addr, create_at FROM tbl_file WHERE file_sha1=? AND user_name=? AND status=1 LIMIT 1",
		filehash, username)

	var f model.FileMeta
	var fileName, fileAddr sql.NullString
	var fileSize sql.NullInt64
	var createAt sql.NullTime
	err := row.Scan(&f.FileSha1, &f.Username, &fileName, &fileSize, &fileAddr, &createAt)
	if err != nil {
		return model.FileMeta{}, fmt.Errorf("get file meta by user failed: %w", err)
	}
	f.FileName = fileName.String
	f.FileSize = fileSize.Int64
	f.Location = fileAddr.String
	f.UploadAt = createAt.Time
	return f, nil
}

func (r *mysqlFileRepo) ListByUser(username string) ([]model.FileMeta, error) {
	rows, err := r.db.Query(
		"SELECT file_sha1, user_name, file_name, file_size, file_addr, create_at FROM tbl_file WHERE user_name=? AND status=1 ORDER BY create_at DESC",
		username)
	if err != nil {
		return nil, fmt.Errorf("query files by user failed: %w", err)
	}
	defer rows.Close()

	var files []model.FileMeta
	for rows.Next() {
		var f model.FileMeta
		var fileName, fileAddr sql.NullString
		var fileSize sql.NullInt64
		var createAt sql.NullTime
		if err := rows.Scan(&f.FileSha1, &f.Username, &fileName, &fileSize, &fileAddr, &createAt); err != nil {
			slog.Error("scan file row failed", "error", err)
			continue
		}
		f.FileName = fileName.String
		f.FileSize = fileSize.Int64
		f.Location = fileAddr.String
		f.UploadAt = createAt.Time
		files = append(files, f)
	}
	return files, nil
}

func (r *mysqlFileRepo) Delete(filehash string) (bool, error) {
	stmt, err := r.db.Prepare("UPDATE tbl_file SET status=2 WHERE file_sha1=? AND status=1")
	if err != nil {
		return false, fmt.Errorf("prepare delete file failed: %w", err)
	}
	defer stmt.Close()

	ret, err := stmt.Exec(filehash)
	if err != nil {
		return false, fmt.Errorf("exec delete file failed: %w", err)
	}
	rows, _ := ret.RowsAffected()
	return rows > 0, nil
}

func (r *mysqlFileRepo) UpdateName(filehash, newFilename string) (bool, error) {
	stmt, err := r.db.Prepare("UPDATE tbl_file SET file_name=? WHERE file_sha1=? AND status=1")
	if err != nil {
		return false, fmt.Errorf("prepare update file name failed: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(newFilename, filehash)
	if err != nil {
		return false, fmt.Errorf("exec update file name failed: %w", err)
	}
	return true, nil
}

// ---- Mock 实现 ----

// mockFileRepo 内存 mock 文件仓库
type mockFileRepo struct {
	files map[string]model.FileMeta // key: filehash
}

// NewMockFileRepository 创建 mock 文件仓库
func NewMockFileRepository() FileRepository {
	return &mockFileRepo{files: make(map[string]model.FileMeta)}
}

func (m *mockFileRepo) Create(f model.FileMeta) error {
	m.files[f.FileSha1] = f
	return nil
}

func (m *mockFileRepo) GetByHash(filehash, username string) (model.FileMeta, error) {
	f, ok := m.files[filehash]
	if !ok || f.Username != username {
		return model.FileMeta{}, fmt.Errorf("file not found")
	}
	return f, nil
}

func (m *mockFileRepo) ListByUser(username string) ([]model.FileMeta, error) {
	var result []model.FileMeta
	for _, f := range m.files {
		if f.Username == username {
			result = append(result, f)
		}
	}
	return result, nil
}

func (m *mockFileRepo) Delete(filehash string) (bool, error) {
	_, ok := m.files[filehash]
	if !ok {
		return false, nil
	}
	delete(m.files, filehash)
	return true, nil
}

func (m *mockFileRepo) UpdateName(filehash, newFilename string) (bool, error) {
	f, ok := m.files[filehash]
	if !ok {
		return false, nil
	}
	f.FileName = newFilename
	m.files[filehash] = f
	return true, nil
}

// 确保编译时检查接口实现
var _ FileRepository = (*mysqlFileRepo)(nil)
var _ FileRepository = (*mockFileRepo)(nil)