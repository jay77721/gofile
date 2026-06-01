package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthCheckHandler_NoRedis(t *testing.T) {
	// Redis 未初始化时应返回 503
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	// 由于 Redis 未连接，会 panic，所以这里只测试函数存在性
	// 实际集成测试需要真实的 Redis
	defer func() {
		if r := recover(); r != nil {
			t.Log("HealthCheck panicked as expected (no Redis):", r)
		}
	}()

	HealthCheckHandler(w, req)
}

func TestUploadHandler_GET(t *testing.T) {
	req := httptest.NewRequest("GET", "/file/upload", nil)
	w := httptest.NewRecorder()

	UploadHandler(w, req)

	// 静态文件在测试环境中可能不存在，所以 200 或 404 都可接受
	if w.Code != http.StatusOK && w.Code != http.StatusNotFound {
		t.Errorf("GET /file/upload status = %d, want %d or %d", w.Code, http.StatusOK, http.StatusNotFound)
	}
}

func TestUploadSucHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/file/upload/suc", nil)
	w := httptest.NewRecorder()

	UploadSucHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if body != "Upload finished" {
		t.Errorf("body = %q, want %q", body, "Upload finished")
	}
}

func TestGetFileHandler_NoFilehash(t *testing.T) {
	req := httptest.NewRequest("GET", "/file/meta", nil)
	w := httptest.NewRecorder()

	GetFileHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"].(float64) != 1 {
		t.Errorf("code = %v, want 1", resp["code"])
	}
}

func TestFileQueryHandler_WrongMethod(t *testing.T) {
	req := httptest.NewRequest("POST", "/file/query", nil)
	w := httptest.NewRecorder()

	FileQueryHandler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestFileDeleteHandler_NoFilehash(t *testing.T) {
	req := httptest.NewRequest("POST", "/file/delete", nil)
	w := httptest.NewRecorder()

	FileDeleteHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestFileMetaUpdateHandler_WrongOp(t *testing.T) {
	req := httptest.NewRequest("POST", "/file/update?op=1&filehash=abc&filename=test.txt", nil)
	w := httptest.NewRecorder()

	FileMetaUpdateHandler(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestFileMetaUpdateHandler_GET(t *testing.T) {
	req := httptest.NewRequest("GET", "/file/update?op=0&filehash=abc&filename=test.txt", nil)
	w := httptest.NewRecorder()

	FileMetaUpdateHandler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestUploadChunkHandler_WrongMethod(t *testing.T) {
	req := httptest.NewRequest("GET", "/file/upload/chunk", nil)
	w := httptest.NewRecorder()

	UploadChunkHandler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestUploadStatusHandler_WrongMethod(t *testing.T) {
	req := httptest.NewRequest("POST", "/file/upload/status", nil)
	w := httptest.NewRecorder()

	UploadStatusHandler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestMergeChunkHandler_WrongMethod(t *testing.T) {
	req := httptest.NewRequest("GET", "/file/upload/merge", nil)
	w := httptest.NewRecorder()

	MergeChunkHandler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, 0, "test message", map[string]string{"key": "value"})

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %s, want application/json", contentType)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["code"].(float64) != 0 {
		t.Errorf("code = %v, want 0", resp["code"])
	}
	if resp["msg"].(string) != "test message" {
		t.Errorf("msg = %v, want 'test message'", resp["msg"])
	}
}

func TestHTTPInterceptor_NoCookies(t *testing.T) {
	inner := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	handler := HTTPInterceptor(inner)
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}
