package handler

import (
	"context"
	"encoding/json"
	"gofile/internal/application/service"
	"gofile/internal/domain"
	"gofile/internal/infrastructure/persistence/repository"
	"gofile/internal/infrastructure/storage"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// setupShareTestHandler assemble share handler test environment; alice owns a file
func setupShareTestHandler(t *testing.T) (*ShareHandler, *service.ShareService, string) {
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
	shareSvc := service.NewShareService(shareRepo, fileRepo)
	fileSvc := service.NewFileService(fileRepo, store, nil)
	return NewShareHandler(shareSvc, fileSvc), shareSvc, hash
}

func TestShareHandlers(t *testing.T) {
	sh, shareSvc, hash := setupShareTestHandler(t)
	r := setupRouter()
	auth := func(h gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) { c.Set("username", "alice"); h(c) }
	}
	r.POST("/file/share", auth(sh.CreateShareHandler))
	r.GET("/file/share/list", auth(sh.ShareListHandler))
	r.POST("/file/share/revoke", auth(sh.RevokeShareHandler))
	r.GET("/share/:token", sh.ShareDownloadHandler)

	post := func(path string, form url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// 1. Create share (with access code)
	w := post("/file/share", url.Values{"filehash": {hash}, "days": {"7"}, "password": {"secret"}})
	if w.Code != http.StatusOK {
		t.Fatalf("create share status = %d, want 200", w.Code)
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			ShareToken string `json:"share_token"`
			URL        string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != 0 || len(resp.Data.ShareToken) != 64 {
		t.Fatalf("create share = code %d token %q", resp.Code, resp.Data.ShareToken)
	}
	token := resp.Data.ShareToken

	// 2. Download without login: wrong access code → 403
	req := httptest.NewRequest("GET", "/share/"+token+"?pwd=wrong", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("wrong pwd status = %d, want 403", w.Code)
	}

	// 3. Download without login: correct access code → 200 + file content
	req = httptest.NewRequest("GET", "/share/"+token+"?pwd=secret", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("download status = %d, want 200", w.Code)
	}
	if w.Body.String() != "0123456789" {
		t.Errorf("body = %q, want %q", w.Body.String(), "0123456789")
	}

	// 4. Range download → 206
	req = httptest.NewRequest("GET", "/share/"+token+"?pwd=secret", nil)
	req.Header.Set("Range", "bytes=2-5")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusPartialContent {
		t.Fatalf("range status = %d, want 206", w.Code)
	}
	if w.Body.String() != "2345" {
		t.Errorf("range body = %q, want %q", w.Body.String(), "2345")
	}

	// 5. Share list
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/file/share/list", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", w.Code)
	}
	var listResp struct {
		Data []model.Share `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Data) != 1 {
		t.Errorf("share list len = %d, want 1", len(listResp.Data))
	}

	// 6. Unknown token → 404
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/share/"+strings.Repeat("0", 64), nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown token status = %d, want 404", w.Code)
	}

	// 7. After revoke → 404
	if w := post("/file/share/revoke", url.Values{"share_token": {token}}); w.Code != http.StatusOK {
		t.Fatalf("revoke status = %d, want 200", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/share/"+token+"?pwd=secret", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("download after revoke = %d, want 404", w.Code)
	}

	// 8. Unauthorized: others cannot revoke (verify ownership via direct service call)
	if err := shareSvc.Revoke(context.Background(), token, "bob"); err == nil {
		t.Errorf("bob revoke should fail")
	}
}
