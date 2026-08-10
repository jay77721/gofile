package storage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLocalPutAtomic 验证 Put 原子写：写入后目标文件完整、内容正确、无临时文件残留
func TestLocalPutAtomic(t *testing.T) {
	dir := t.TempDir()
	s := NewLocal(dir)

	key := "abcdef0123456789abcdef0123456789abcdef01"
	content := []byte("hello gofile, this is a test payload")
	ctx := context.Background()

	if err := s.Put(ctx, key, bytes.NewReader(content), int64(len(content))); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// 目标文件存在且内容完整
	got, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(got); err != nil {
		t.Fatalf("read failed: %v", err)
	}
	got.Close()
	if !bytes.Equal(buf.Bytes(), content) {
		t.Errorf("content = %q, want %q", buf.String(), content)
	}

	// 无临时文件残留
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir failed: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("dir entries = %d, want 1 (only the target file)", len(entries))
	}
}

// TestLocalPutKeySanitized 验证 key 经过 filepath.Base 清洗，无法越出上传目录
func TestLocalPutKeySanitized(t *testing.T) {
	dir := t.TempDir()
	s := NewLocal(dir)
	ctx := context.Background()

	if err := s.Put(ctx, "../../escape.txt", strings.NewReader("x"), 1); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// 文件应落在 uploadDir 下的 escape.txt，而不是上层目录
	if _, err := os.Stat(filepath.Join(dir, "escape.txt")); err != nil {
		t.Errorf("file not stored under upload dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "escape.txt")); err == nil {
		t.Errorf("file escaped the upload dir")
	}
}
