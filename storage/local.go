package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
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

// Put 将文件写入本地磁盘（原子写：先写临时文件，成功后 rename，避免中断留下半截文件）
func (s *LocalStorage) Put(ctx context.Context, key string, reader io.Reader, size int64) error {
	path := filepath.Join(s.uploadDir, filepath.Base(key))

	// 确保目录存在
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dir failed: %w", err)
	}

	// 写临时文件（同目录保证 rename 原子性）
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file failed: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// 成功 rename 后 tmpName 已不存在，Remove 报错被忽略
		tmp.Close()
		os.Remove(tmpName)
	}()

	if _, err := io.Copy(tmp, reader); err != nil {
		return fmt.Errorf("write file failed: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync file failed: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file failed: %w", err)
	}

	// 原子替换：中断只会留下临时文件，不会产生"半截"目标文件
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename file failed: %w", err)
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

// GetRange 按字节区间读取本地文件（io.SectionReader 支持零拷贝区间读取）
func (s *LocalStorage) GetRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	path := filepath.Join(s.uploadDir, filepath.Base(key))
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file failed: %w", err)
	}
	// SectionReader: 零拷贝区间读取，读取 [offset, offset+length) 范围
	// 用自定义 ReadCloser 包装：NopCloser 不会关闭底层 fd，会导致文件句柄泄漏
	return &sectionReadCloser{SectionReader: io.NewSectionReader(file, offset, length), f: file}, nil
}

// sectionReadCloser 组合 SectionReader 与底层文件，Close 时释放 fd
type sectionReadCloser struct {
	*io.SectionReader
	f *os.File
}

func (s *sectionReadCloser) Close() error { return s.f.Close() }

// FileSize 获取本地文件大小
func (s *LocalStorage) FileSize(ctx context.Context, key string) (int64, error) {
	path := filepath.Join(s.uploadDir, filepath.Base(key))
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("stat file failed: %w", err)
	}
	return info.Size(), nil
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

// PresignPut 本地存储不支持预签名上传，返回 ErrPresignNotSupported
func (s *LocalStorage) PresignPut(ctx context.Context, key string, expiry time.Duration) (string, error) {
	return "", ErrPresignNotSupported
}

// PresignGet 本地存储不支持预签名下载，返回 ErrPresignNotSupported
func (s *LocalStorage) PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	return "", ErrPresignNotSupported
}
