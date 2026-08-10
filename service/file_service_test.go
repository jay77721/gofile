package service

import (
	"bytes"
	"context"
	"errors"
	"gofile/model"
	"gofile/repository"
	"gofile/storage"
	"io"
	"testing"
)

// newTestFileService 组装 mock repo + 真实本地存储的 FileService
func newTestFileService(t *testing.T, hash, content string) *FileService {
	t.Helper()
	repo := repository.NewMockFileRepository()
	if err := repo.Create(model.File{FileSha1: hash, FileSize: int64(len(content))}); err != nil {
		t.Fatalf("repo.Create failed: %v", err)
	}
	if err := repo.CreateUserFile(model.UserFile{Username: "alice", FileSha1: hash, FileName: "a.txt", Status: model.UserFileStatusActive}); err != nil {
		t.Fatalf("repo.CreateUserFile failed: %v", err)
	}

	store := storage.NewLocal(t.TempDir())
	ctx := context.Background()
	if err := store.Put(ctx, hash, bytes.NewReader([]byte(content)), int64(len(content))); err != nil {
		t.Fatalf("store.Put failed: %v", err)
	}
	return NewFileService(repo, store, nil)
}

func readAll(t *testing.T, r io.Reader) []byte {
	t.Helper()
	buf, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	return buf
}

func TestDownloadRange(t *testing.T) {
	const hash = "abcdef0123456789abcdef0123456789abcdef01"
	content := "0123456789" // 10 字节
	svc := newTestFileService(t, hash, content)
	ctx := context.Background()

	t.Run("closed range", func(t *testing.T) {
		r, _, total, actualLen, err := svc.DownloadRange(ctx, hash, "alice", 2, 4)
		if err != nil {
			t.Fatalf("DownloadRange failed: %v", err)
		}
		defer r.Close()
		if total != 10 || actualLen != 4 {
			t.Errorf("total/len = %d/%d, want 10/4", total, actualLen)
		}
		if got := string(readAll(t, r)); got != "2345" {
			t.Errorf("body = %q, want %q", got, "2345")
		}
	})

	t.Run("open range fills to EOF", func(t *testing.T) {
		r, _, total, actualLen, err := svc.DownloadRange(ctx, hash, "alice", 7, -1)
		if err != nil {
			t.Fatalf("DownloadRange failed: %v", err)
		}
		defer r.Close()
		if total != 10 || actualLen != 3 {
			t.Errorf("total/len = %d/%d, want 10/3", total, actualLen)
		}
		if got := string(readAll(t, r)); got != "789" {
			t.Errorf("body = %q, want %q", got, "789")
		}
	})

	t.Run("overlong length is clamped", func(t *testing.T) {
		r, _, _, actualLen, err := svc.DownloadRange(ctx, hash, "alice", 5, 1000)
		if err != nil {
			t.Fatalf("DownloadRange failed: %v", err)
		}
		defer r.Close()
		if actualLen != 5 {
			t.Errorf("len = %d, want 5 (clamped)", actualLen)
		}
		if got := string(readAll(t, r)); got != "56789" {
			t.Errorf("body = %q, want %q", got, "56789")
		}
	})

	t.Run("offset beyond EOF returns ErrRangeOutOfBounds", func(t *testing.T) {
		r, _, total, _, err := svc.DownloadRange(ctx, hash, "alice", 10, 1)
		if r != nil {
			r.Close()
		}
		if !errors.Is(err, ErrRangeOutOfBounds) {
			t.Errorf("err = %v, want ErrRangeOutOfBounds", err)
		}
		if total != 10 {
			t.Errorf("total = %d, want 10 (用于 416 Content-Range)", total)
		}
	})

	t.Run("no permission", func(t *testing.T) {
		if _, _, _, _, err := svc.DownloadRange(ctx, hash, "bob", 0, 5); err == nil {
			t.Errorf("expected error for unauthorized user, got nil")
		}
	})
}

// TestTrashLifecycle 回收站全流程:删除 → 回收站可见 → 恢复 → 再次删除 → 彻底删除
func TestTrashLifecycle(t *testing.T) {
	const hash = "abcdef0123456789abcdef0123456789abcdef01"
	content := "0123456789"
	svc := newTestFileService(t, hash, content)
	ctx := context.Background()

	// 软删除
	if err := svc.Delete(hash, "alice"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	// 删除后正常列表不可见
	if files, _, err := svc.ListByUserPaged("alice", 1, 10); err != nil || len(files) != 0 {
		t.Errorf("active list after delete = %d files (err=%v), want 0", len(files), err)
	}
	// 回收站可见
	trash, total, err := svc.ListTrash("alice", 1, 10)
	if err != nil || total != 1 || len(trash) != 1 {
		t.Fatalf("ListTrash = %d/%d (err=%v), want 1/1", len(trash), total, err)
	}
	if trash[0].FileSha1 != hash {
		t.Errorf("trash item = %s, want %s", trash[0].FileSha1, hash)
	}

	// 恢复
	if err := svc.Restore(ctx, hash, "alice"); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	if files, _, _ := svc.ListByUserPaged("alice", 1, 10); len(files) != 1 {
		t.Errorf("active list after restore = %d, want 1", len(files))
	}
	if _, total, _ := svc.ListTrash("alice", 1, 10); total != 0 {
		t.Errorf("trash after restore = %d, want 0", total)
	}

	// 恢复后再次删除 → 彻底删除
	if err := svc.Delete(hash, "alice"); err != nil {
		t.Fatalf("second Delete failed: %v", err)
	}
	if err := svc.Purge(ctx, hash, "alice"); err != nil {
		t.Fatalf("Purge failed: %v", err)
	}
	// 彻底删除后列表与回收站都为空
	if files, _, _ := svc.ListByUserPaged("alice", 1, 10); len(files) != 0 {
		t.Errorf("active list after purge = %d, want 0", len(files))
	}
	if _, total, _ := svc.ListTrash("alice", 1, 10); total != 0 {
		t.Errorf("trash after purge = %d, want 0", total)
	}
	// 存储层已清理
	if exists, _ := svc.store.Exists(ctx, hash); exists {
		t.Errorf("storage object still exists after purge")
	}
	// 重复彻底删除应报错(已不存在)
	if err := svc.Purge(ctx, hash, "alice"); err == nil {
		t.Errorf("second Purge should fail, got nil")
	}
}
