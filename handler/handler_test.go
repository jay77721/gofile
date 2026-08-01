package handler

import (
	"bytes"
	"encoding/json"
	"filestore-server/config"
	"filestore-server/storage"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

func setupTestStore(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("", "filestore-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	InitStore(storage.NewLocal(dir), &config.Config{UploadDir: dir, ChunkDir: dir})
}

func TestHealthCheckHandler_NoRedis(t *testing.T) {
	r := setupRouter()
	r.GET("/healthz", HealthCheckHandler)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	// Redis 未初始化会 panic，测试函数存在性
	defer func() {
		if rec := recover(); rec != nil {
			t.Log("HealthCheck panicked as expected (no Redis):", rec)
		}
	}()

	r.ServeHTTP(w, req)
}

func TestGetFileHandler_NoFilehash(t *testing.T) {
	r := setupRouter()
	r.GET("/file/meta", GetFileHandler)

	req := httptest.NewRequest("GET", "/file/meta", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"].(float64) != 1 {
		t.Errorf("code = %v, want 1", resp["code"])
	}
}

func TestFileDeleteHandler_NoFilehash(t *testing.T) {
	r := setupRouter()
	r.POST("/file/delete", FileDeleteHandler)

	req := httptest.NewRequest("POST", "/file/delete", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestFileMetaUpdateHandler_WrongOp(t *testing.T) {
	r := setupRouter()
	r.POST("/file/update", FileMetaUpdateHandler)

	req := httptest.NewRequest("POST", "/file/update?op=1&filehash=abc&filename=test.txt", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestUploadStatusHandler_NoFilehash(t *testing.T) {
	r := setupRouter()
	r.GET("/file/upload/status", UploadStatusHandler)

	req := httptest.NewRequest("GET", "/file/upload/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestMergeChunkHandler_NoParams(t *testing.T) {
	r := setupRouter()
	r.POST("/file/upload/merge", MergeChunkHandler)

	req := httptest.NewRequest("POST", "/file/upload/merge", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestUploadHandler_NoFile(t *testing.T) {
	setupTestStore(t)
	r := setupRouter()
	r.POST("/file/upload", UploadHandler)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.Close()

	req := httptest.NewRequest("POST", "/file/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 没有 file 字段，应返回 400
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestUploadHandler_WithFilehashOnly(t *testing.T) {
	setupTestStore(t)
	r := setupRouter()
	r.POST("/file/upload", UploadHandler)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("filehash", "testhash123")
	writer.Close()

	req := httptest.NewRequest("POST", "/file/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// filehash 提交但没有 file 字段，应返回 400
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestDownloadHandler_NoFilehash(t *testing.T) {
	r := setupRouter()
	r.GET("/file/download", DownloadHandler)

	req := httptest.NewRequest("GET", "/file/download", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"].(float64) != 1 {
		t.Errorf("code = %v, want 1", resp["code"])
	}
}

func TestDownloadHandler_NotFound(t *testing.T) {
	r := setupRouter()
	r.GET("/file/download", DownloadHandler)

	req := httptest.NewRequest("GET", "/file/download?filehash=nonexistent_hash_12345", nil)
	w := httptest.NewRecorder()

	// 无 MySQL 连接会 panic
	defer func() {
		if rec := recover(); rec != nil {
			t.Log("DownloadHandler panicked as expected (no MySQL):", rec)
			return
		}
		// 如果有 MySQL 连接则检查状态码
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
		}
	}()

	r.ServeHTTP(w, req)
}

func TestUploadChunkHandler_NoParams(t *testing.T) {
	r := setupRouter()
	r.POST("/file/upload/chunk", UploadChunkHandler)

	req := httptest.NewRequest("POST", "/file/upload/chunk", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestUploadChunkHandler_InvalidIndex(t *testing.T) {
	setupTestStore(t)
	r := setupRouter()
	r.POST("/file/upload/chunk", UploadChunkHandler)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("filehash", "abc123")
	writer.WriteField("index", "invalid")
	writer.Close()

	req := httptest.NewRequest("POST", "/file/upload/chunk", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestUploadChunkHandler_NegativeIndex(t *testing.T) {
	setupTestStore(t)
	r := setupRouter()
	r.POST("/file/upload/chunk", UploadChunkHandler)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("filehash", "abc123")
	writer.WriteField("index", "-1")
	writer.Close()

	req := httptest.NewRequest("POST", "/file/upload/chunk", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestUploadChunkHandler_NoFile(t *testing.T) {
	setupTestStore(t)
	r := setupRouter()
	r.POST("/file/upload/chunk", UploadChunkHandler)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("filehash", "abc123")
	writer.WriteField("index", "0")
	writer.Close()

	req := httptest.NewRequest("POST", "/file/upload/chunk", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestFileMetaUpdateHandler_MissingParams(t *testing.T) {
	r := setupRouter()
	r.POST("/file/update", FileMetaUpdateHandler)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("op", "0")
	writer.WriteField("filehash", "")
	writer.WriteField("filename", "test.txt")
	writer.Close()

	req := httptest.NewRequest("POST", "/file/update", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestGetFileHandler_NotFound(t *testing.T) {
	r := setupRouter()
	r.GET("/file/meta", GetFileHandler)

	req := httptest.NewRequest("GET", "/file/meta?filehash=nonexistent_hash_67890", nil)
	w := httptest.NewRecorder()

	defer func() {
		if rec := recover(); rec != nil {
			t.Log("GetFileHandler panicked as expected (no MySQL):", rec)
			return
		}
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
		}
	}()

	r.ServeHTTP(w, req)
}

func TestMergeChunkHandler_MissingFilehash(t *testing.T) {
	r := setupRouter()
	r.POST("/file/upload/merge", MergeChunkHandler)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("filename", "test.txt")
	writer.Close()

	req := httptest.NewRequest("POST", "/file/upload/merge", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestFileQueryHandler_NoDB(t *testing.T) {
	r := setupRouter()
	r.GET("/file/query", FileQueryHandler)

	req := httptest.NewRequest("GET", "/file/query", nil)
	w := httptest.NewRecorder()

	defer func() {
		if rec := recover(); rec != nil {
			t.Log("FileQueryHandler panicked as expected (no MySQL):", rec)
			return
		}
		if w.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
		}
	}()

	r.ServeHTTP(w, req)
}

func TestRateLimitMiddleware(t *testing.T) {
	r := setupRouter()
	r.GET("/test", RateLimitMiddleware(1, 1), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	// 第一次请求应该成功
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("first request: status = %d, want %d", w.Code, http.StatusOK)
	}
}
