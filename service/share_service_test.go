package service

import (
	"context"
	"errors"
	"gofile/model"
	"gofile/repository"
	"gofile/storage"
	"strings"
	"testing"
	"time"
)

// newTestShareSvc 组装 mock repo + 本地存储的分享服务,alice 拥有一个文件
func newTestShareSvc(t *testing.T) (*ShareService, *storage.LocalStorage, string) {
	t.Helper()
	const hash = "abcdef0123456789abcdef0123456789abcdef01"

	dir := t.TempDir()
	store := storage.NewLocal(dir)
	fileRepo := repository.NewMockFileRepository()
	if err := fileRepo.Create(context.Background(), model.File{FileSha1: hash, FileSize: 10}); err != nil {
		t.Fatal(err)
	}
	if err := fileRepo.CreateUserFile(context.Background(), model.UserFile{Username: "alice", FileSha1: hash, FileName: "a.txt", Status: model.UserFileStatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), hash, strings.NewReader("0123456789"), 10); err != nil {
		t.Fatal(err)
	}

	shareRepo := repository.NewMockShareRepository()
	return NewShareService(shareRepo, fileRepo), store, hash
}

func TestShareService(t *testing.T) {
	svc, _, hash := newTestShareSvc(t)
	ctx := context.Background()

	t.Run("create validates ownership", func(t *testing.T) {
		if _, err := svc.Create(ctx, "bob", hash, 7, ""); err == nil {
			t.Errorf("expected error for non-owner, got nil")
		}
	})

	t.Run("create with password and custom days", func(t *testing.T) {
		share, err := svc.Create(ctx, "alice", hash, 3, "secret")
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		if len(share.ShareToken) != 64 {
			t.Errorf("token length = %d, want 64", len(share.ShareToken))
		}
		if share.PasswordHash == "" || share.PasswordHash == "secret" {
			t.Errorf("password must be bcrypt-hashed")
		}
		if got := share.ExpireAt.Sub(time.Now()); got < 2*24*3600*1e9 || got > 4*24*3600*1e9 {
			t.Errorf("expire = %v, want ~3d", got)
		}
	})

	t.Run("create defaults days to 7 when out of range", func(t *testing.T) {
		for _, days := range []int{0, 99, -1} {
			share, err := svc.Create(ctx, "alice", hash, days, "")
			if err != nil {
				t.Fatalf("Create(days=%d) failed: %v", days, err)
			}
			got := share.ExpireAt.Sub(time.Now())
			if got < 6*24*3600*1e9 || got > 8*24*3600*1e9 {
				t.Errorf("days=%d expire = %v, want ~7d", days, got)
			}
		}
	})

	t.Run("resolve: wrong password", func(t *testing.T) {
		share, _ := svc.Create(ctx, "alice", hash, 7, "secret")
		if _, err := svc.Resolve(ctx, share.ShareToken, "wrong"); !errors.Is(err, ErrShareWrongPwd) {
			t.Errorf("err = %v, want ErrShareWrongPwd", err)
		}
	})

	t.Run("resolve: correct password", func(t *testing.T) {
		share, _ := svc.Create(ctx, "alice", hash, 7, "secret")
		meta, err := svc.Resolve(ctx, share.ShareToken, "secret")
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if meta.FileSha1 != hash {
			t.Errorf("resolved file = %s, want %s", meta.FileSha1, hash)
		}
	})

	t.Run("resolve: no password share", func(t *testing.T) {
		share, _ := svc.Create(ctx, "alice", hash, 7, "")
		if _, err := svc.Resolve(ctx, share.ShareToken, ""); err != nil {
			t.Errorf("Resolve without password failed: %v", err)
		}
	})

	t.Run("resolve: unknown token", func(t *testing.T) {
		if _, err := svc.Resolve(ctx, strings.Repeat("0", 64), ""); !errors.Is(err, ErrShareNotFound) {
			t.Errorf("err = %v, want ErrShareNotFound", err)
		}
	})

	t.Run("revoke: ownership enforced", func(t *testing.T) {
		share, _ := svc.Create(ctx, "alice", hash, 7, "")
		if err := svc.Revoke(ctx, share.ShareToken, "bob"); !errors.Is(err, ErrShareNotFound) {
			t.Errorf("bob revoke err = %v, want ErrShareNotFound", err)
		}
		if err := svc.Revoke(ctx, share.ShareToken, "alice"); err != nil {
			t.Fatalf("alice revoke failed: %v", err)
		}
		if _, err := svc.Resolve(ctx, share.ShareToken, ""); !errors.Is(err, ErrShareNotFound) {
			t.Errorf("resolve after revoke err = %v, want ErrShareNotFound", err)
		}
		t.Run("resolve: file soft-deleted", func(t *testing.T) {
			share, _ := svc.Create(ctx, "alice", hash, 7, "")
			// 用新的 FileService 删除文件(共享同一 mock repo)
			fileSvc := NewFileService(svc.fileRepo, storage.NewLocal(t.TempDir()), nil)
			if err := fileSvc.Delete(context.Background(), hash, "alice"); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Resolve(ctx, share.ShareToken, ""); !errors.Is(err, ErrShareFileGone) {
				t.Errorf("err = %v, want ErrShareFileGone", err)
			}
		})

	})
}

// TestShareListHasPassword 验证列表接口 has_password 字段(PasswordHash 不下发)
func TestShareListHasPassword(t *testing.T) {
	svc, _, hash := newTestShareSvc(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "alice", hash, 7, "secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, "alice", hash, 7, ""); err != nil {
		t.Fatal(err)
	}

	shares, err := svc.List(context.Background(), "alice")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(shares) != 2 {
		t.Fatalf("len = %d, want 2", len(shares))
	}
	withPwd := 0
	for _, s := range shares {
		if s.PasswordHash != "" {
			t.Errorf("PasswordHash must not be exposed, got %q", s.PasswordHash)
		}
		if s.HasPassword {
			withPwd++
		}
	}
	if withPwd != 1 {
		t.Errorf("has_password count = %d, want 1", withPwd)
	}
}
