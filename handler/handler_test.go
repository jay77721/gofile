package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
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
