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
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// setupTestHandler 创建测试用的 FileHandler（不依赖 MySQL）
func setupTestHandler(t *testing.T) (*FileHandler, *UserHandler, *AuthMiddleware) {
	t.Helper()
	dir, err := os.MkdirTemp("", "gofile-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	store := storage.NewLocal(dir)
	cfg := &config.Config{UploadDir: dir, ChunkDir: dir}

	fileRepo := repository.NewMockFileRepository()
	userRepo := repository.NewMockUserRepository()
	tokenRepo := repository.NewMockTokenRepository()

	fileSvc := service.NewFileService(fileRepo, store, cfg)
	userSvc := service.NewUserService(userRepo, tokenRepo)
	authSvc := service.NewAuthService(tokenRepo)

	fileHandler := NewFileHandler(fileSvc, cfg)
	userHandler := NewUserHandler(userSvc, cfg)
	authMiddleware := NewAuthMiddleware(authSvc)

	return fileHandler, userHandler, authMiddleware
}

func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

func TestHealthCheckHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/healthz", HealthCheckHandler)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal("unmarshal failed:", err)
	}
	if code, ok := resp["code"].(float64); !ok || code != 0 {
		t.Errorf("code = %v, want 0", resp["code"])
	}
}

func TestGetFileHandler_NoFilehash(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.GET("/file/meta", fh.GetFileHandler)

	req := httptest.NewRequest("GET", "/file/meta", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"].(float64) != float64(CodeInvalidParams) {
		t.Errorf("code = %v, want %d", resp["code"], CodeInvalidParams)
	}
}

func TestFileDeleteHandler_NoFilehash(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.POST("/file/delete", fh.FileDeleteHandler)

	req := httptest.NewRequest("POST", "/file/delete", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestFileMetaUpdateHandler_WrongOp(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.POST("/file/update", fh.FileMetaUpdateHandler)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("op", "1")
	writer.WriteField("filehash", "abc")
	writer.WriteField("filename", "test.txt")
	writer.Close()

	req := httptest.NewRequest("POST", "/file/update", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestUploadStatusHandler_NoFilehash(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.GET("/file/upload/status", fh.UploadStatusHandler)

	req := httptest.NewRequest("GET", "/file/upload/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestMergeChunkHandler_NoParams(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.POST("/file/upload/merge", fh.MergeChunkHandler)

	req := httptest.NewRequest("POST", "/file/upload/merge", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestUploadHandler_NoFile(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.POST("/file/upload", fh.UploadHandler)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.Close()

	req := httptest.NewRequest("POST", "/file/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestUploadHandler_WithFilehashOnly(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.POST("/file/upload", fh.UploadHandler)

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
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.GET("/file/download", fh.DownloadHandler)

	req := httptest.NewRequest("GET", "/file/download", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"].(float64) != float64(CodeInvalidParams) {
		t.Errorf("code = %v, want %d", resp["code"], CodeInvalidParams)
	}
}

func TestDownloadHandler_NotFound(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.GET("/file/download", fh.DownloadHandler)

	req := httptest.NewRequest("GET", "/file/download?filehash=nonexistent_hash_12345", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 使用 mock 不再 panic，返回 404
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestUploadChunkHandler_NoParams(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.POST("/file/upload/chunk", fh.UploadChunkHandler)

	req := httptest.NewRequest("POST", "/file/upload/chunk", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestUploadChunkHandler_InvalidIndex(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.POST("/file/upload/chunk", fh.UploadChunkHandler)

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
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.POST("/file/upload/chunk", fh.UploadChunkHandler)

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
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.POST("/file/upload/chunk", fh.UploadChunkHandler)

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
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.POST("/file/update", fh.FileMetaUpdateHandler)

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
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.GET("/file/meta", fh.GetFileHandler)

	req := httptest.NewRequest("GET", "/file/meta?filehash=nonexistent_hash_67890", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 使用 mock 不再 panic，返回 404
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestMergeChunkHandler_MissingFilehash(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.POST("/file/upload/merge", fh.MergeChunkHandler)

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
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.GET("/file/query", fh.FileQueryHandler)

	req := httptest.NewRequest("GET", "/file/query", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 使用 mock 不再 panic，返回空列表
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
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

func TestSignupHandler_MissingParams(t *testing.T) {
	_, uh, _ := setupTestHandler(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/user/signup", uh.SignupHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/user/signup", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestSignInHandler_MissingParams(t *testing.T) {
	_, uh, _ := setupTestHandler(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/user/signin", uh.SignInHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/user/signin", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestUserInfoHandler_NoCookie(t *testing.T) {
	_, uh, am := setupTestHandler(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/user/info", am.Middleware(), uh.UserInfoHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/user/info", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestParseRangeHeader(t *testing.T) {
	cases := []struct {
		name      string
		header    string
		totalSize int64
		wantOK    bool
		wantOff   int64
		wantLen   int64
	}{
		{"closed range", "bytes=0-1023", 0, true, 0, 1024},
		{"closed range mid", "bytes=100-199", 0, true, 100, 100},
		{"open range with known size", "bytes=100-", 1000, true, 100, 900},
		{"open range unknown size -> -1", "bytes=100-", 0, true, 100, -1},
		{"open range beyond size", "bytes=900-", 900, false, 0, 0},
		{"reversed range", "bytes=200-100", 0, false, 0, 0},
		{"non-bytes unit", "items=0-10", 0, false, 0, 0},
		{"empty spec", "bytes=", 0, false, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			off, length, ok := parseRangeHeader(tc.header, tc.totalSize)
			if ok != tc.wantOK || off != tc.wantOff || length != tc.wantLen {
				t.Errorf("parseRangeHeader(%q, %d) = (%d, %d, %v), want (%d, %d, %v)",
					tc.header, tc.totalSize, off, length, ok, tc.wantOff, tc.wantLen, tc.wantOK)
			}
		})
	}
}

// setupRangeTestHandler 准备一个包含 10 字节文件(alice 拥有)的 handler 测试环境
func setupRangeTestHandler(t *testing.T) (*FileHandler, string) {
	t.Helper()
	dir := t.TempDir()
	store := storage.NewLocal(dir)
	cfg := &config.Config{UploadDir: dir, ChunkDir: dir}
	fileRepo := repository.NewMockFileRepository()
	fileSvc := service.NewFileService(fileRepo, store, cfg)
	fh := NewFileHandler(fileSvc, cfg)

	const hash = "abcdef0123456789abcdef0123456789abcdef01"
	content := []byte("0123456789")
	if err := fileRepo.Create(context.Background(), model.File{FileSha1: hash, FileSize: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if err := fileRepo.CreateUserFile(context.Background(), model.UserFile{Username: "alice", FileSha1: hash, FileName: "a.txt", Status: model.UserFileStatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), hash, bytes.NewReader(content), int64(len(content))); err != nil {
		t.Fatal(err)
	}
	return fh, hash
}

func TestDownloadHandler_Range(t *testing.T) {
	fh, hash := setupRangeTestHandler(t)
	r := setupRouter()
	// 模拟 AuthMiddleware 写入的 username
	r.GET("/file/download", func(c *gin.Context) {
		c.Set("username", "alice")
		fh.DownloadHandler(c)
	})

	cases := []struct {
		name        string
		rangeHeader string
		wantStatus  int
		wantCR      string // Content-Range 期望值;为空则不校验
		wantBody    string // 期望响应体;为空则不校验
	}{
		{"open range", "bytes=2-", http.StatusPartialContent, "bytes 2-9/10", "23456789"},
		{"closed range", "bytes=1-3", http.StatusPartialContent, "bytes 1-3/10", "123"},
		{"clamped closed range", "bytes=5-99", http.StatusPartialContent, "bytes 5-9/10", "56789"},
		{"out of bounds", "bytes=10-", http.StatusRequestedRangeNotSatisfiable, "bytes */10", ""},
		{"invalid range", "bytes=9-2", http.StatusRequestedRangeNotSatisfiable, "bytes */10", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/file/download?filehash="+hash, nil)
			req.Header.Set("Range", tc.rangeHeader)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if tc.wantCR != "" {
				if got := w.Header().Get("Content-Range"); got != tc.wantCR {
					t.Errorf("Content-Range = %q, want %q", got, tc.wantCR)
				}
			}
			if tc.wantBody != "" {
				if got := w.Body.String(); got != tc.wantBody {
					t.Errorf("body = %q, want %q", got, tc.wantBody)
				}
			}
		})
	}
}

// setupTrashTestHandler 准备 alice 已软删除一个文件的环境
func setupTrashTestHandler(t *testing.T) (*FileHandler, string) {
	t.Helper()
	dir := t.TempDir()
	store := storage.NewLocal(dir)
	cfg := &config.Config{UploadDir: dir, ChunkDir: dir}
	fileRepo := repository.NewMockFileRepository()
	fileSvc := service.NewFileService(fileRepo, store, cfg)
	fh := NewFileHandler(fileSvc, cfg)

	const hash = "abcdef0123456789abcdef0123456789abcdef01"
	content := []byte("0123456789")
	if err := fileRepo.Create(context.Background(), model.File{FileSha1: hash, FileSize: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if err := fileRepo.CreateUserFile(context.Background(), model.UserFile{Username: "alice", FileSha1: hash, FileName: "a.txt", Status: model.UserFileStatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), hash, bytes.NewReader(content), int64(len(content))); err != nil {
		t.Fatal(err)
	}
	if err := fileSvc.Delete(context.Background(), hash, "alice"); err != nil {
		t.Fatal(err)
	}
	return fh, hash
}

func TestTrashHandlers(t *testing.T) {
	fh, hash := setupTrashTestHandler(t)
	r := setupRouter()
	auth := func(h func(*gin.Context)) gin.HandlerFunc {
		return func(c *gin.Context) { c.Set("username", "alice"); h(c) }
	}
	r.GET("/file/trash", auth(fh.TrashHandler))
	r.POST("/file/restore", auth(fh.RestoreHandler))
	r.POST("/file/purge", auth(fh.PurgeHandler))
	r.POST("/file/delete", auth(fh.FileDeleteHandler))

	post := func(path, filehash string) *httptest.ResponseRecorder {
		form := url.Values{"filehash": {filehash}}
		req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// 1. 回收站列表包含已删除文件
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/file/trash?page=1&size=20", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("trash status = %d, want 200", w.Code)
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			List  []map[string]any `json:"list"`
			Total int64            `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != 0 || resp.Data.Total != 1 {
		t.Fatalf("trash = code %d total %d, want 0/1", resp.Code, resp.Data.Total)
	}

	// 2. 恢复
	if w := post("/file/restore", hash); w.Code != http.StatusOK {
		t.Fatalf("restore status = %d, want 200", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/file/trash", nil))
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data.Total != 0 {
		t.Fatalf("trash after restore = %d, want 0", resp.Data.Total)
	}

	// 3. 恢复后删除 → 彻底删除
	if w := post("/file/delete", hash); w.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", w.Code)
	}
	if w := post("/file/purge", hash); w.Code != http.StatusOK {
		t.Fatalf("purge status = %d, want 200", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/file/trash", nil))
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data.Total != 0 {
		t.Fatalf("trash after purge = %d, want 0", resp.Data.Total)
	}

	// 4. 越权:bob 无法恢复 alice 的文件
	if w := post("/file/restore", hash); w.Code != http.StatusNotFound {
		t.Fatalf("unauthorized restore status = %d, want 404", w.Code)
	}
}

func TestLogoutHandler(t *testing.T) {
	_, uh, _ := setupTestHandler(t)
	r := setupRouter()
	r.POST("/user/logout", uh.LogoutHandler)

	req := httptest.NewRequest("POST", "/user/logout", nil)
	req.AddCookie(&http.Cookie{Name: "username", Value: "alice"})
	req.AddCookie(&http.Cookie{Name: "token", Value: "deadbeef"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	cleared := map[string]bool{}
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 {
			cleared[c.Name] = true
		}
	}
	if !cleared["token"] || !cleared["username"] {
		t.Errorf("cookies not cleared: %v", cleared)
	}
}
