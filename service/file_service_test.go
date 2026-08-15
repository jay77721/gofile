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
	"time"
)

// newTestFileService 组装 mock repo + 真实本地存储的 FileService
func newTestFileService(t *testing.T, hash, content string) *FileService {
	t.Helper()
	repo := repository.NewMockFileRepository()
	if err := repo.Create(context.Background(), model.File{FileSha1: hash, FileName: "a.txt", FileSize: int64(len(content))}); err != nil {
		t.Fatalf("repo.Create failed: %v", err)
	}
	if err := repo.CreateUserFile(context.Background(), model.UserFile{Username: "alice", FileSha1: hash, FileName: "a.txt", Status: model.UserFileStatusActive}); err != nil {
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
	if err := svc.Delete(context.Background(), hash, "alice"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	// 删除后正常列表不可见
	if files, _, err := svc.ListByUserPaged(context.Background(), "alice", 1, 10); err != nil || len(files) != 0 {
		t.Errorf("active list after delete = %d files (err=%v), want 0", len(files), err)
	}
	// 回收站可见
	trash, total, err := svc.ListTrash(context.Background(), "alice", 1, 10)
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
	if files, _, _ := svc.ListByUserPaged(context.Background(), "alice", 1, 10); len(files) != 1 {
		t.Errorf("active list after restore = %d, want 1", len(files))
	}
	if _, total, _ := svc.ListTrash(context.Background(), "alice", 1, 10); total != 0 {
		t.Errorf("trash after restore = %d, want 0", total)
	}

	// 恢复后再次删除 → 彻底删除
	if err := svc.Delete(context.Background(), hash, "alice"); err != nil {
		t.Fatalf("second Delete failed: %v", err)
	}
	if err := svc.Purge(ctx, hash, "alice"); err != nil {
		t.Fatalf("Purge failed: %v", err)
	}
	// 彻底删除后列表与回收站都为空
	if files, _, _ := svc.ListByUserPaged(context.Background(), "alice", 1, 10); len(files) != 0 {
		t.Errorf("active list after purge = %d, want 0", len(files))
	}
	if _, total, _ := svc.ListTrash(context.Background(), "alice", 1, 10); total != 0 {
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

// TestFastUploadOwnership 秒传路径必须为当前用户建立所有权关联(跨用户场景)
func TestFastUploadOwnership(t *testing.T) {
	const hash = "fedcba9876543210fedcba9876543210fedcba98"
	content := "shared file content"
	svc := newTestFileService(t, hash, content)
	ctx := context.Background()

	// 用户 bob 秒传 alice 已上传的文件(存储层已存在,bob 无关联)
	exists, err := svc.FastUpload(ctx, hash, "bob")
	if err != nil {
		t.Fatalf("FastUpload failed: %v", err)
	}
	if !exists {
		t.Fatal("expected exists=true")
	}

	// bob 现在拥有该文件:可查询、可下载
	meta, err := svc.GetMeta(context.Background(), hash, "bob")
	if err != nil {
		t.Fatalf("bob should own file after fast upload: %v", err)
	}
	if meta.FileName != "a.txt" {
		t.Fatalf("expected filename inherited from global record, got %q", meta.FileName)
	}
	rc, _, err := svc.Download(ctx, hash, "bob")
	if err != nil {
		t.Fatalf("bob download failed: %v", err)
	}
	rc.Close()

	// 幂等:重复秒传不报错
	if _, err := svc.FastUpload(ctx, hash, "bob"); err != nil {
		t.Fatalf("second fast upload should be idempotent: %v", err)
	}
}

// TestFastUploadMiss 存储层不存在时返回 false,不建关联
func TestFastUploadMiss(t *testing.T) {
	const hash = "0000000000000000000000000000000000000000"
	svc := newTestFileService(t, "abcdef0123456789abcdef0123456789abcdef01", "x")
	exists, err := svc.FastUpload(context.Background(), hash, "bob")
	if err != nil {
		t.Fatalf("FastUpload should not error on miss, got %v", err)
	}
	if exists {
		t.Fatal("expected exists=false for missing file")
	}
	if _, err := svc.GetMeta(context.Background(), hash, "bob"); err == nil {
		t.Fatal("bob should not own a file that was never uploaded")
	}
}

func TestVFSFolderManagement(t *testing.T) {
	repo := repository.NewMockFileRepository()
	store := storage.NewLocal(t.TempDir())
	svc := NewFileService(repo, store, nil)
	ctx := context.Background()

	// 1. 创建根文件夹
	folderA, err := svc.CreateFolder(ctx, "alice", model.FolderCreateReq{
		Name:     "学习资料",
		ParentID: 0,
	})
	if err != nil {
		t.Fatalf("CreateFolder failed: %v", err)
	}
	if folderA.DirPath != "/学习资料/" {
		t.Fatalf("expected dirPath /学习资料/, got %q", folderA.DirPath)
	}

	// 2. 创建子文件夹
	folderB, err := svc.CreateFolder(ctx, "alice", model.FolderCreateReq{
		Name:     "Go源码",
		ParentID: uint64(folderA.ID),
	})
	if err != nil {
		t.Fatalf("create subfolder failed: %v", err)
	}
	if folderB.DirPath != "/学习资料/Go源码/" {
		t.Fatalf("expected dirPath /学习资料/Go源码/, got %q", folderB.DirPath)
	}

	// 3. 重命名文件夹
	err = svc.RenameFolderOrFile(ctx, "alice", model.FolderRenameReq{
		FileID:  folderA.ID,
		NewName: "核心资料",
	})
	if err != nil {
		t.Fatalf("RenameFolderOrFile failed: %v", err)
	}

	// 4. 防循环移动测试：禁止将父目录移入自己的子目录
	err = svc.MoveFolderOrFile(ctx, "alice", model.FolderMoveReq{
		FileID:         folderA.ID,
		TargetParentID: uint64(folderB.ID),
	})
	if err == nil {
		t.Fatal("expected error when moving folder into its own subfolder, got nil")
	}

	// 5. 目录与面包屑查询
	_, _, crumbs, err := svc.QueryDirectory(ctx, "alice", uint64(folderB.ID), 0, 10)
	if err != nil {
		t.Fatalf("QueryDirectory failed: %v", err)
	}
	if len(crumbs) < 2 {
		t.Fatalf("expected breadcrumbs, got %+v", crumbs)
	}
}

type mockMultipartStorage struct {
	*storage.LocalStorage
	inited   bool
	complete bool
}

func (m *mockMultipartStorage) InitMultipart(ctx context.Context, key string) (string, error) {
	m.inited = true
	return "upload-xyz-123", nil
}

func (m *mockMultipartStorage) PresignPartPut(ctx context.Context, key, uploadID string, partNumber int, expiry time.Duration) (string, error) {
	return "http://mock-minio/part?partNumber=1", nil
}

func (m *mockMultipartStorage) CompleteMultipart(ctx context.Context, key, uploadID string, parts []storage.CompletePart) error {
	m.complete = true
	return nil
}

func (m *mockMultipartStorage) AbortMultipart(ctx context.Context, key, uploadID string) error {
	return nil
}

func TestMultipartService(t *testing.T) {
	repo := repository.NewMockFileRepository()
	multipartRepo := repository.NewMockMultipartRepository()
	localStorage := storage.NewLocal(t.TempDir())
	mockStore := &mockMultipartStorage{LocalStorage: localStorage}

	svc := NewFileService(repo, mockStore, nil).WithMultipart(multipartRepo)
	ctx := context.Background()

	const hash = "1234567890123456789012345678901234567890"

	// 1. 初始化直传（未命中秒传）
	initResp, err := svc.InitMultipartUpload(ctx, "alice", model.MultipartInitReq{
		FileSha1:  hash,
		FileName:  "bigfile.zip",
		FileSize:  25 * 1024 * 1024, // 25MB -> 3 parts (10MB each)
		ChunkSize: 10 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("InitMultipartUpload failed: %v", err)
	}
	if initResp.FastUpload {
		t.Fatal("expected fast_upload=false")
	}
	if initResp.UploadID != "upload-xyz-123" || initResp.ChunkCount != 3 || len(initResp.PartURLs) != 3 {
		t.Fatalf("unexpected init response: %+v", initResp)
	}

	// 2. 完成合并
	parts := []storage.CompletePart{
		{PartNumber: 1, ETag: "etag1"},
		{PartNumber: 2, ETag: "etag2"},
		{PartNumber: 3, ETag: "etag3"},
	}
	meta, err := svc.CompleteMultipartUpload(ctx, "alice", model.MultipartCompleteReq{
		UploadID: initResp.UploadID,
		Parts:    parts,
	})
	if err != nil {
		t.Fatalf("CompleteMultipartUpload failed: %v", err)
	}
	if meta.FileSha1 != hash || meta.FileName != "bigfile.zip" {
		t.Fatalf("unexpected completed meta: %+v", meta)
	}
	if !mockStore.complete {
		t.Fatal("expected complete multipart to be called on storage")
	}
}
