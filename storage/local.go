package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// LocalStorage 本地文件系统存储实现
type LocalStorage struct {
	uploadDir string
}

// NewLocal 创建本地存储实例
func NewLocal(uploadDir string) *LocalStorage {
	os.MkdirAll(uploadDir, 0755)
	return &LocalStorage{uploadDir: uploadDir}
}

// Put 将文件写入本地磁盘
func (s *LocalStorage) Put(ctx context.Context, key string, reader io.Reader, size int64) error {
	path := filepath.Join(s.uploadDir, filepath.Base(key))

	// 确保目录存在
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dir failed: %w", err)
	}

	dst, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file failed: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, reader); err != nil {
		return fmt.Errorf("write file failed: %w", err)
	}

	slog.Info("file stored locally", "key", key, "size", size)
	return nil
}

// Get 从本地磁盘读取文件
func (s *LocalStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	path := filepath.Join(s.uploadDir, filepath.Base(key))
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file failed: %w", err)
	}
	return file, nil
}

// Exists 检查文件是否存在于本地磁盘
func (s *LocalStorage) Exists(ctx context.Context, key string) (bool, error) {
	path := filepath.Join(s.uploadDir, filepath.Base(key))
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat file failed: %w", err)
	}
	return true, nil
}

// Delete 从本地磁盘删除文件
func (s *LocalStorage) Delete(ctx context.Context, key string) error {
	path := filepath.Join(s.uploadDir, filepath.Base(key))
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete file failed: %w", err)
	}
	return nil
}
