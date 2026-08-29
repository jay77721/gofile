package service

import (
	"bytes"
	"context"
	hashutil "gofile/internal/common/hash"
	"gofile/internal/config"
	"gofile/internal/domain"
	"gofile/internal/infrastructure/persistence/repository"
	"gofile/internal/infrastructure/storage"
	"gofile/internal/port"
	"strings"
	"testing"
	"time"
)

type mockMultipartStorage struct {
	*storage.LocalStorage
	inited           bool
	complete         bool
	aborted          bool
	completedContent []byte
}

func (m *mockMultipartStorage) InitMultipart(ctx context.Context, key string) (string, error) {
	m.inited = true
	return "upload-xyz-123", nil
}

func (m *mockMultipartStorage) PresignPartPut(ctx context.Context, key, uploadID string, partNumber int, expiry time.Duration) (string, error) {
	return "http://mock-minio/part?partNumber=1", nil
}

func (m *mockMultipartStorage) CompleteMultipart(ctx context.Context, key, uploadID string, parts []port.CompletePart) error {
	m.complete = true
	if len(m.completedContent) > 0 {
		return m.LocalStorage.Put(ctx, key, bytes.NewReader(m.completedContent), int64(len(m.completedContent)))
	}
	return nil
}

func (m *mockMultipartStorage) AbortMultipart(ctx context.Context, key, uploadID string) error {
	m.aborted = true
	return nil
}

func TestS3Multipart_InitUpload(t *testing.T) {
	repo := repository.NewMockFileRepository()
	multipartRepo := repository.NewMockMultipartRepository()
	mockStore := &mockMultipartStorage{LocalStorage: storage.NewLocal(t.TempDir())}
	svc := NewFileService(repo, mockStore, nil).WithMultipart(multipartRepo)
	ctx := context.Background()

	const hash = "1234567890123456789012345678901234567890"

	t.Run("init normal multipart upload", func(t *testing.T) {
		resp, err := svc.InitMultipartUpload(ctx, "alice", model.MultipartInitReq{
			FileSha1:  hash,
			FileName:  "bigfile.zip",
			FileSize:  25 * 1024 * 1024, // 25MB -> 3 chunks with 10MB chunkSize
			ChunkSize: 10 * 1024 * 1024,
		})
		if err != nil {
			t.Fatalf("InitMultipartUpload failed: %v", err)
		}
		if resp.FastUpload {
			t.Fatal("expected fast_upload=false")
		}
		if resp.UploadID != "upload-xyz-123" || resp.ChunkCount != 3 || len(resp.PartURLs) != 3 {
			t.Fatalf("unexpected init response: %+v", resp)
		}
		if !mockStore.inited {
			t.Fatal("expected storage.InitMultipart to be called")
		}

		// 检查 repository 记录已创建
		record, err := multipartRepo.GetByUploadID(ctx, resp.UploadID, "alice")
		if err != nil || record.Status != model.MultipartStatusUploading {
			t.Fatalf("unexpected multipart record: %+v, err: %v", record, err)
		}
	})

	t.Run("chunk size clamping", func(t *testing.T) {
		// 0 chunkSize -> defaults to 10MB
		resp1, err := svc.InitMultipartUpload(ctx, "alice", model.MultipartInitReq{
			FileSha1:  "2222222222222222222222222222222222222222",
			FileName:  "default_chunk.bin",
			FileSize:  15 * 1024 * 1024,
			ChunkSize: 0,
		})
		if err != nil || resp1.ChunkSize != 10*1024*1024 {
			t.Errorf("expected default chunkSize 10MB, got %d (err=%v)", resp1.ChunkSize, err)
		}

		// < 5MB chunkSize -> clamped to 5MB
		resp2, err := svc.InitMultipartUpload(ctx, "alice", model.MultipartInitReq{
			FileSha1:  "3333333333333333333333333333333333333333",
			FileName:  "clamped_chunk.bin",
			FileSize:  15 * 1024 * 1024,
			ChunkSize: 2 * 1024 * 1024,
		})
		if err != nil || resp2.ChunkSize != 5*1024*1024 {
			t.Errorf("expected clamped chunkSize 5MB, got %d (err=%v)", resp2.ChunkSize, err)
		}
	})

	t.Run("fast upload hit path", func(t *testing.T) {
		const existingHash = "4444444444444444444444444444444444444444"
		// 先在全局仓储与存储中存入该文件
		_ = repo.Create(ctx, model.File{FileSha1: existingHash, FileSize: 16})
		_ = mockStore.Put(ctx, existingHash, strings.NewReader("existing content"), 16)

		resp, err := svc.InitMultipartUpload(ctx, "bob", model.MultipartInitReq{
			FileSha1: existingHash,
			FileName: "existing.txt",
			FileSize: 16,
		})
		if err != nil {
			t.Fatalf("InitMultipartUpload failed: %v", err)
		}
		if !resp.FastUpload {
			t.Fatal("expected fast_upload=true for existing file")
		}

		// 检查 Bob 的 UserFile 已建立
		userFile, err := repo.GetByHash(ctx, existingHash, "bob")
		if err != nil || userFile.FileName != "existing.txt" {
			t.Fatalf("bob userfile not found or wrong name: %v", err)
		}
	})

	t.Run("with parent folder", func(t *testing.T) {
		folder, _ := svc.CreateFolder(ctx, "alice", model.FolderCreateReq{Name: "TargetFolder", ParentID: 0})

		resp, err := svc.InitMultipartUpload(ctx, "alice", model.MultipartInitReq{
			FileSha1: "5555555555555555555555555555555555555555",
			FileName: "target.bin",
			FileSize: 10 * 1024 * 1024,
			ParentID: uint64(folder.ID),
		})
		if err != nil {
			t.Fatalf("init with parent failed: %v", err)
		}
		if resp.UploadID == "" {
			t.Fatal("expected valid upload ID")
		}

		// 无效 parentID 报错
		_, err = svc.InitMultipartUpload(ctx, "alice", model.MultipartInitReq{
			FileSha1: "6666666666666666666666666666666666666666",
			FileName: "target.bin",
			FileSize: 10 * 1024 * 1024,
			ParentID: 99999,
		})
		if err == nil {
			t.Fatal("expected error for non-existent parentID, got nil")
		}
	})

	t.Run("unconfigured multipart repo fails", func(t *testing.T) {
		unconfiguredSvc := NewFileService(repo, mockStore, nil)
		_, err := unconfiguredSvc.InitMultipartUpload(ctx, "alice", model.MultipartInitReq{
			FileSha1: hash,
			FileName: "a.bin",
			FileSize: 1024,
		})
		if err == nil {
			t.Fatal("expected error when multipart repo is nil, got nil")
		}
	})
}

func TestS3Multipart_CompleteUpload(t *testing.T) {
	repo := repository.NewMockFileRepository()
	multipartRepo := repository.NewMockMultipartRepository()
	mockStore := &mockMultipartStorage{LocalStorage: storage.NewLocal(t.TempDir())}
	svc := NewFileService(repo, mockStore, nil).WithMultipart(multipartRepo)
	ctx := context.Background()

	content := bytes.Repeat([]byte{0x42}, 25*1024*1024)
	hash := hashutil.Sha1(content)
	mockStore.completedContent = content

	initResp, err := svc.InitMultipartUpload(ctx, "alice", model.MultipartInitReq{
		FileSha1:  hash,
		FileName:  "bigfile.zip",
		FileSize:  25 * 1024 * 1024,
		ChunkSize: 10 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("InitMultipartUpload failed: %v", err)
	}

	t.Run("complete success", func(t *testing.T) {
		parts := []port.CompletePart{
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
			t.Fatalf("unexpected meta: %+v", meta)
		}
		if !mockStore.complete {
			t.Fatal("expected CompleteMultipart to be called on storage")
		}

		// 检查分片会话状态已更新为 Completed (2)
		record, err := multipartRepo.GetByUploadID(ctx, initResp.UploadID, "alice")
		if err != nil || record.Status != model.MultipartStatusCompleted {
			t.Fatalf("expected status completed, got record: %+v", record)
		}
	})

	t.Run("complete already completed session fails", func(t *testing.T) {
		parts := []port.CompletePart{{PartNumber: 1, ETag: "etag1"}}
		_, err := svc.CompleteMultipartUpload(ctx, "alice", model.MultipartCompleteReq{
			UploadID: initResp.UploadID,
			Parts:    parts,
		})
		if err == nil {
			t.Fatal("expected error on completing already finished upload, got nil")
		}
	})

	t.Run("complete non-existent session fails", func(t *testing.T) {
		_, err := svc.CompleteMultipartUpload(ctx, "alice", model.MultipartCompleteReq{
			UploadID: "non-existent-upload-id",
			Parts:    []port.CompletePart{},
		})
		if err == nil {
			t.Fatal("expected error on non-existent session, got nil")
		}
	})
}

func TestS3Multipart_AbortUpload(t *testing.T) {
	repo := repository.NewMockFileRepository()
	multipartRepo := repository.NewMockMultipartRepository()
	mockStore := &mockMultipartStorage{LocalStorage: storage.NewLocal(t.TempDir())}
	svc := NewFileService(repo, mockStore, nil).WithMultipart(multipartRepo)
	ctx := context.Background()

	const hash = "1234567890123456789012345678901234567890"

	initResp, err := svc.InitMultipartUpload(ctx, "alice", model.MultipartInitReq{
		FileSha1:  hash,
		FileName:  "abort_target.zip",
		FileSize:  10 * 1024 * 1024,
		ChunkSize: 5 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("InitMultipartUpload failed: %v", err)
	}

	t.Run("abort success", func(t *testing.T) {
		err := svc.AbortMultipartUpload(ctx, "alice", initResp.UploadID)
		if err != nil {
			t.Fatalf("AbortMultipartUpload failed: %v", err)
		}
		if !mockStore.aborted {
			t.Fatal("expected storage.AbortMultipart to be called")
		}

		record, err := multipartRepo.GetByUploadID(ctx, initResp.UploadID, "alice")
		if err != nil || record.Status != model.MultipartStatusAborted {
			t.Fatalf("expected status aborted (3), got %+v", record)
		}
	})

	t.Run("abort non-existent session fails", func(t *testing.T) {
		err := svc.AbortMultipartUpload(ctx, "alice", "unknown-session")
		if err == nil {
			t.Fatal("expected error on unknown session, got nil")
		}
	})
}

func TestTraditionalChunk_UploadAndMerge(t *testing.T) {
	tempDir := t.TempDir()
	repo := repository.NewMockFileRepository()
	store := storage.NewLocal(tempDir)
	cfg := &config.Config{UploadDir: tempDir, ChunkDir: tempDir}
	svc := NewFileService(repo, store, cfg)
	ctx := context.Background()

	chunk0 := []byte("Hello, ")
	chunk1 := []byte("World! ")
	chunk2 := []byte("Traditional chunk test.")
	hash := hashutil.Sha1(bytes.Join([][]byte{chunk0, chunk1, chunk2}, nil))

	t.Run("upload chunks and query status", func(t *testing.T) {
		if err := svc.UploadChunk(ctx, hash, 0, bytes.NewReader(chunk0), "alice"); err != nil {
			t.Fatalf("UploadChunk 0 failed: %v", err)
		}
		if err := svc.UploadChunk(ctx, hash, 1, bytes.NewReader(chunk1), "alice"); err != nil {
			t.Fatalf("UploadChunk 1 failed: %v", err)
		}
		if err := svc.UploadChunk(ctx, hash, 2, bytes.NewReader(chunk2), "alice"); err != nil {
			t.Fatalf("UploadChunk 2 failed: %v", err)
		}

		// 重复上传 chunk 0 幂等
		if err := svc.UploadChunk(ctx, hash, 0, bytes.NewReader(chunk0), "alice"); err != nil {
			t.Fatalf("UploadChunk 0 duplicate failed: %v", err)
		}

		// 查询已上传分片列表
		chunks, err := svc.GetChunkStatus(hash, "alice")
		if err != nil {
			t.Fatalf("GetChunkStatus failed: %v", err)
		}
		if len(chunks) != 3 || chunks[0] != "0" || chunks[1] != "1" || chunks[2] != "2" {
			t.Fatalf("unexpected chunk status: %v", chunks)
		}
	})

	t.Run("merge with chunk count mismatch fails", func(t *testing.T) {
		_, err := svc.MergeChunks(ctx, hash, "merged.txt", "alice", "5") // expected 5, but only 3
		if err == nil {
			t.Fatal("expected error on chunk count mismatch, got nil")
		}
	})

	t.Run("merge success", func(t *testing.T) {
		meta, err := svc.MergeChunks(ctx, hash, "merged.txt", "alice", "3")
		if err != nil {
			t.Fatalf("MergeChunks failed: %v", err)
		}
		expectedSize := int64(len(chunk0) + len(chunk1) + len(chunk2))
		if meta.FileSize != expectedSize || meta.FileSha1 != hash {
			t.Fatalf("unexpected meta after merge: %+v, want size %d", meta, expectedSize)
		}

		// 验证存储层文件存在
		exists, err := store.Exists(ctx, hash)
		if err != nil || !exists {
			t.Fatalf("storage file does not exist after merge: %v", err)
		}

		// 验证用户拥有该文件
		fMeta, err := svc.GetMeta(ctx, hash, "alice")
		if err != nil || fMeta.FileName != "merged.txt" {
			t.Fatalf("user does not own merged file: %v", err)
		}
	})
}
