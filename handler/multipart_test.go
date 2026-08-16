package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"gofile/config"
	"gofile/model"
	"gofile/repository"
	"gofile/service"
	"gofile/storage"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type mockMultipartStorageForHandler struct {
	*storage.LocalStorage
	inited   bool
	complete bool
	aborted  bool
}

func (m *mockMultipartStorageForHandler) InitMultipart(ctx context.Context, key string) (string, error) {
	m.inited = true
	return "upload-handler-id-123", nil
}

func (m *mockMultipartStorageForHandler) PresignPartPut(ctx context.Context, key, uploadID string, partNumber int, expiry time.Duration) (string, error) {
	return "http://mock-minio/part?partNumber=1", nil
}

func (m *mockMultipartStorageForHandler) CompleteMultipart(ctx context.Context, key, uploadID string, parts []storage.CompletePart) error {
	m.complete = true
	return nil
}

func (m *mockMultipartStorageForHandler) AbortMultipart(ctx context.Context, key, uploadID string) error {
	m.aborted = true
	return nil
}

func setupMultipartRouter(fh *FileHandler, username string) *gin.Engine {
	r := setupRouter()
	r.Use(func(c *gin.Context) {
		c.Set("username", username)
		c.Next()
	})
	r.POST("/file/upload/multipart/init", fh.MultipartInitHandler)
	r.POST("/file/upload/multipart/complete", fh.MultipartCompleteHandler)
	r.POST("/file/upload/multipart/abort", fh.MultipartAbortHandler)

	r.POST("/file/upload/chunk", fh.UploadChunkHandler)
	r.GET("/file/upload/status", fh.UploadStatusHandler)
	r.POST("/file/upload/merge", fh.MergeChunkHandler)
	return r
}

func setupMultipartTestHandler(t *testing.T) (*FileHandler, repository.FileRepository, *mockMultipartStorageForHandler) {
	t.Helper()
	dir, err := os.MkdirTemp("", "gofile-mp-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	fileRepo := repository.NewMockFileRepository()
	multipartRepo := repository.NewMockMultipartRepository()
	localStorage := storage.NewLocal(dir)
	mockStore := &mockMultipartStorageForHandler{LocalStorage: localStorage}

	cfg := &config.Config{UploadDir: dir, ChunkDir: dir}
	fileSvc := service.NewFileService(fileRepo, mockStore, cfg).WithMultipart(multipartRepo)
	fh := NewFileHandler(fileSvc, cfg)
	return fh, fileRepo, mockStore
}

func TestS3Multipart_Handlers(t *testing.T) {
	fh, fileRepo, mockStore := setupMultipartTestHandler(t)
	r := setupMultipartRouter(fh, "alice")
	ctx := context.Background()

	const validHash = "1111222233334444555566667777888899990000"

	t.Run("init normal multipart upload", func(t *testing.T) {
		initBody, _ := json.Marshal(model.MultipartInitReq{
			FileSha1:  validHash,
			FileName:  "archive.tar.gz",
			FileSize:  20 * 1024 * 1024,
			ChunkSize: 10 * 1024 * 1024,
		})
		req := httptest.NewRequest("POST", "/file/upload/multipart/init", bytes.NewReader(initBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("init multipart status = %d, want 200", w.Code)
		}

		var initResp struct {
			Code int                     `json:"code"`
			Data model.MultipartInitResp `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &initResp)
		if initResp.Data.UploadID != "upload-handler-id-123" || initResp.Data.ChunkCount != 2 || len(initResp.Data.PartURLs) != 2 {
			t.Fatalf("unexpected init response: %+v", initResp)
		}
	})

	t.Run("init fast upload hit path", func(t *testing.T) {
		const existingHash = "5555555555555555555555555555555555555555"
		_ = fileRepo.Create(ctx, model.File{FileSha1: existingHash, FileSize: 1024})
		_ = mockStore.Put(ctx, existingHash, strings.NewReader("fast content"), 1024)

		initBody, _ := json.Marshal(model.MultipartInitReq{
			FileSha1: existingHash,
			FileName: "fast.zip",
			FileSize: 1024,
		})
		req := httptest.NewRequest("POST", "/file/upload/multipart/init", bytes.NewReader(initBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("fast upload init status = %d, want 200", w.Code)
		}
		var initResp struct {
			Code int                     `json:"code"`
			Data model.MultipartInitResp `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &initResp)
		if !initResp.Data.FastUpload {
			t.Fatal("expected fast_upload=true")
		}
	})

	t.Run("init invalid params", func(t *testing.T) {
		// 无效 hash
		body, _ := json.Marshal(model.MultipartInitReq{FileSha1: "invalid-hash", FileName: "test.zip", FileSize: 1024})
		req := httptest.NewRequest("POST", "/file/upload/multipart/init", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("invalid hash status = %d, want 400", w.Code)
		}

		// 危险扩展名
		body, _ = json.Marshal(model.MultipartInitReq{FileSha1: validHash, FileName: "malicious.exe", FileSize: 1024})
		req = httptest.NewRequest("POST", "/file/upload/multipart/init", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("dangerous ext status = %d, want 400", w.Code)
		}
	})

	t.Run("complete multipart upload", func(t *testing.T) {
		compBody, _ := json.Marshal(model.MultipartCompleteReq{
			UploadID: "upload-handler-id-123",
			Parts: []storage.CompletePart{
				{PartNumber: 1, ETag: "etag1"},
				{PartNumber: 2, ETag: "etag2"},
			},
		})
		req := httptest.NewRequest("POST", "/file/upload/multipart/complete", bytes.NewReader(compBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("complete multipart status = %d, want 200", w.Code)
		}
	})

	t.Run("complete invalid params", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/file/upload/multipart/complete", bytes.NewReader([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("missing params status = %d, want 400", w.Code)
		}
	})

	t.Run("abort multipart upload", func(t *testing.T) {
		// 先初始化一个新的会话供取消
		initBody, _ := json.Marshal(model.MultipartInitReq{
			FileSha1: "6666666666666666666666666666666666666666",
			FileName: "to_abort.zip",
			FileSize: 10 * 1024 * 1024,
		})
		reqInit := httptest.NewRequest("POST", "/file/upload/multipart/init", bytes.NewReader(initBody))
		reqInit.Header.Set("Content-Type", "application/json")
		wInit := httptest.NewRecorder()
		r.ServeHTTP(wInit, reqInit)

		abortBody, _ := json.Marshal(model.MultipartAbortReq{UploadID: "upload-handler-id-123"})
		req := httptest.NewRequest("POST", "/file/upload/multipart/abort", bytes.NewReader(abortBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("abort multipart status = %d, want 200", w.Code)
		}
	})
}

func TestTraditionalChunk_Handlers(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupMultipartRouter(fh, "alice")

	const validHash = "abcdef0123456789abcdef0123456789abcdef01"

	t.Run("upload chunk missing params", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/file/upload/chunk", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("missing params status = %d, want 400", w.Code)
		}
	})

	t.Run("upload chunk invalid hash or index", func(t *testing.T) {
		// Invalid hash
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		writer.WriteField("filehash", "badhash")
		writer.WriteField("index", "0")
		writer.Close()
		req := httptest.NewRequest("POST", "/file/upload/chunk", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("bad hash status = %d, want 400", w.Code)
		}

		// Invalid index
		body = &bytes.Buffer{}
		writer = multipart.NewWriter(body)
		writer.WriteField("filehash", validHash)
		writer.WriteField("index", "-1")
		writer.Close()
		req = httptest.NewRequest("POST", "/file/upload/chunk", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("negative index status = %d, want 400", w.Code)
		}
	})

	t.Run("upload chunk success and query status", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		writer.WriteField("filehash", validHash)
		writer.WriteField("index", "0")
		part, _ := writer.CreateFormFile("file", "chunk0")
		part.Write([]byte("chunk 0 content"))
		writer.Close()

		req := httptest.NewRequest("POST", "/file/upload/chunk", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("upload chunk status = %d, want 200", w.Code)
		}

		// 查询已上传分块状态
		statusReq := httptest.NewRequest("GET", "/file/upload/status?filehash="+validHash, nil)
		statusW := httptest.NewRecorder()
		r.ServeHTTP(statusW, statusReq)
		if statusW.Code != http.StatusOK {
			t.Fatalf("status query status = %d, want 200", statusW.Code)
		}
	})

	t.Run("merge chunk validation", func(t *testing.T) {
		// Missing filehash
		req := httptest.NewRequest("POST", "/file/upload/merge", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("merge without params status = %d, want 400", w.Code)
		}

		// Dangerous extension
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		writer.WriteField("filehash", validHash)
		writer.WriteField("filename", "trojan.exe")
		writer.Close()
		req = httptest.NewRequest("POST", "/file/upload/merge", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("dangerous merge filename status = %d, want 400", w.Code)
		}
	})
}
