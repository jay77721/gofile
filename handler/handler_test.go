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

func TestRateLimitMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/test", RateLimitMiddleware(1, 1), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("first request: status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestSignupHandler_MissingParams(t *testing.T) {
	_, uh, _ := setupTestHandler(t)
	r := setupRouter()
	r.POST("/user/signup", uh.SignupHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/user/signup", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestSignupHandler_RejectsUnsafeUsername(t *testing.T) {
	_, uh, _ := setupTestHandler(t)
	r := setupRouter()
	r.POST("/user/signup", uh.SignupHandler)

	for _, username := range []string{"ab", "alice smith", "alice:admin", "alice` || true", "1alice", strings.Repeat("a", 65)} {
		form := url.Values{"username": {username}, "password": {"GoodPass1!"}}
		req := httptest.NewRequest("POST", "/user/signup", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("username %q: status = %d, want 400", username, w.Code)
		}
	}
}

func TestSignInHandler_RejectsUnsafeUsername(t *testing.T) {
	_, uh, _ := setupTestHandler(t)
	r := setupRouter()
	r.POST("/user/signin", uh.SignInHandler)

	form := url.Values{"username": {"alice:admin"}, "password": {"GoodPass1!"}}
	req := httptest.NewRequest("POST", "/user/signin", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestSignInHandler_MissingParams(t *testing.T) {
	_, uh, _ := setupTestHandler(t)
	r := setupRouter()
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
	r := setupRouter()
	r.GET("/user/info", am.Middleware(), uh.UserInfoHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/user/info", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
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

func TestUploadHandler_Validation(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.Use(func(c *gin.Context) {
		c.Set("username", "alice")
		c.Next()
	})
	r.POST("/file/upload", fh.UploadHandler)

	t.Run("no file in form returns 400", func(t *testing.T) {
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
	})

	t.Run("filehash only without file returns 400 when file does not exist", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		writer.WriteField("filehash", "nonexistenthash123")
		writer.Close()

		req := httptest.NewRequest("POST", "/file/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("dangerous extension returns 400", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", "script.sh")
		part.Write([]byte("echo evil"))
		writer.Close()

		req := httptest.NewRequest("POST", "/file/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("upload valid file success", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", "hello.txt")
		part.Write([]byte("hello world"))
		writer.Close()

		req := httptest.NewRequest("POST", "/file/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
	})
}

func TestFastUploadHandler(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.Use(func(c *gin.Context) {
		c.Set("username", "alice")
		c.Next()
	})
	r.POST("/file/fastupload", fh.FastUploadHandler)

	const validHash = "abcdef0123456789abcdef0123456789abcdef01"

	t.Run("missing hash returns 400", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/file/fastupload", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("miss fast upload returns 200 with code 0", func(t *testing.T) {
		form := url.Values{"filehash": {validHash}}
		req := httptest.NewRequest("POST", "/file/fastupload", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
	})
}

func TestPresignAndConfirmUploadHandler(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.Use(func(c *gin.Context) {
		c.Set("username", "alice")
		c.Next()
	})
	r.POST("/file/upload/presign", fh.PresignUploadHandler)
	r.POST("/file/upload/confirm", fh.ConfirmUploadHandler)

	const validHash = "abcdef0123456789abcdef0123456789abcdef01"

	t.Run("presign missing params", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/file/upload/presign", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("confirm dangerous extension", func(t *testing.T) {
		form := url.Values{
			"filehash": {validHash},
			"filename": {"trojan.exe"},
		}
		req := httptest.NewRequest("POST", "/file/upload/confirm", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})
}

func TestFileMetaAndQueryHandlers(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.Use(func(c *gin.Context) {
		c.Set("username", "alice")
		c.Next()
	})
	r.GET("/file/meta", fh.GetFileHandler)
	r.GET("/file/query", fh.FileQueryHandler)

	t.Run("meta missing filehash", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/file/meta", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("meta not found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/file/meta?filehash=nonexistent_hash_67890", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})

	t.Run("query empty", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/file/query", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", w.Code)
		}
	})

	t.Run("query paged", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/file/query?page=1&size=10", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", w.Code)
		}
	})
}

func TestFileMetaUpdateAndDeleteHandlers(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupRouter()
	r.Use(func(c *gin.Context) {
		c.Set("username", "alice")
		c.Next()
	})
	r.POST("/file/update", fh.FileMetaUpdateHandler)
	r.POST("/file/delete", fh.FileDeleteHandler)

	t.Run("update missing params", func(t *testing.T) {
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
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("update wrong op", func(t *testing.T) {
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
			t.Errorf("status = %d, want 403", w.Code)
		}
	})

	t.Run("delete missing filehash", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/file/delete", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})
}

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
	rBob := setupRouter()
	rBob.POST("/file/restore", func(c *gin.Context) { c.Set("username", "bob"); fh.RestoreHandler(c) })
	reqBob := httptest.NewRequest("POST", "/file/restore", strings.NewReader((url.Values{"filehash": {hash}}).Encode()))
	reqBob.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	wBob := httptest.NewRecorder()
	rBob.ServeHTTP(wBob, reqBob)
	if wBob.Code != http.StatusNotFound {
		t.Fatalf("unauthorized restore status = %d, want 404", wBob.Code)
	}
}
