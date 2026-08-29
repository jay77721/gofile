package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"gofile/internal/port"
)

// LocalStorage is a local filesystem storage implementation
type LocalStorage struct {
	uploadDir string
}

// NewLocal creates a local storage instance
func NewLocal(uploadDir string) *LocalStorage {
	os.MkdirAll(uploadDir, 0755)
	return &LocalStorage{uploadDir: uploadDir}
}

// Put writes a file to local disk (atomic write: write to temp file first, then rename on success to avoid leaving a partial file on interruption)
func (s *LocalStorage) Put(ctx context.Context, key string, reader io.Reader, size int64) error {
	path := filepath.Join(s.uploadDir, filepath.Base(key))

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dir failed: %w", err)
	}

	// Write temp file (same directory ensures atomic rename)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file failed: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// After successful rename, tmpName no longer exists and Remove errors are ignored
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

	// Atomic replacement: interruption will only leave a temp file, not a partial target file
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename file failed: %w", err)
	}

	slog.Info("file stored locally", "key", key, "size", size)
	return nil
}

// Get reads a file from local disk
func (s *LocalStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	path := filepath.Join(s.uploadDir, filepath.Base(key))
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file failed: %w", err)
	}
	return file, nil
}

// GetRange reads a local file by byte range (io.SectionReader supports zero-copy range reading)
func (s *LocalStorage) GetRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	path := filepath.Join(s.uploadDir, filepath.Base(key))
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file failed: %w", err)
	}
	// SectionReader: zero-copy range reading, reads [offset, offset+length)
	// Wrap with custom ReadCloser: NopCloser would not close the underlying fd and would leak file handles
	return &sectionReadCloser{SectionReader: io.NewSectionReader(file, offset, length), f: file}, nil
}

// sectionReadCloser combines SectionReader with the underlying file, releasing fd on Close
type sectionReadCloser struct {
	*io.SectionReader
	f *os.File
}

func (s *sectionReadCloser) Close() error { return s.f.Close() }

// FileSize retrieves the local file size
func (s *LocalStorage) FileSize(ctx context.Context, key string) (int64, error) {
	path := filepath.Join(s.uploadDir, filepath.Base(key))
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("stat file failed: %w", err)
	}
	return info.Size(), nil
}

// Exists checks whether a file exists on local disk
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

// Delete deletes a file from local disk
func (s *LocalStorage) Delete(ctx context.Context, key string) error {
	path := filepath.Join(s.uploadDir, filepath.Base(key))
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete file failed: %w", err)
	}
	return nil
}

// PresignPut is not supported for local storage, returns ErrPresignNotSupported
func (s *LocalStorage) PresignPut(ctx context.Context, key string, expiry time.Duration) (string, error) {
	return "", ErrPresignNotSupported
}

// PresignGet is not supported for local storage, returns ErrPresignNotSupported
func (s *LocalStorage) PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	return "", ErrPresignNotSupported
}

// InitMultipart is not supported for local storage (S3 presigned multipart upload)
func (s *LocalStorage) InitMultipart(ctx context.Context, key string) (string, error) {
	return "", ErrPresignNotSupported
}

// PresignPartPut is not supported for local storage (S3 presigned multipart upload)
func (s *LocalStorage) PresignPartPut(ctx context.Context, key, uploadID string, partNumber int, expiry time.Duration) (string, error) {
	return "", ErrPresignNotSupported
}

// CompleteMultipart is not supported for local storage (S3 multipart completion)
func (s *LocalStorage) CompleteMultipart(ctx context.Context, key, uploadID string, parts []port.CompletePart) error {
	return ErrPresignNotSupported
}

// AbortMultipart is not supported for local storage (S3 multipart abort)
func (s *LocalStorage) AbortMultipart(ctx context.Context, key, uploadID string) error {
	return ErrPresignNotSupported
}
