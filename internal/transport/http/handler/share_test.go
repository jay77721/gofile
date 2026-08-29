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

// setupShareTestHandler 组装分享 handler 测试环境,alice 拥有一个文件
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

	// 1. 创建分享(带提取码)
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

	// 2. 免登录下载:错误提取码 → 403
	req := httptest.NewRequest("GET", "/share/"+token+"?pwd=wrong", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("wrong pwd status = %d, want 403", w.Code)
	}

	// 3. 免登录下载:正确提取码 → 200 + 文件内容
	req = httptest.NewRequest("GET", "/share/"+token+"?pwd=secret", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("download status = %d, want 200", w.Code)
	}
	if w.Body.String() != "0123456789" {
		t.Errorf("body = %q, want %q", w.Body.String(), "0123456789")
	}

	// 4. Range 下载 → 206
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

	// 5. 分享列表
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

	// 6. 未知令牌 → 404
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/share/"+strings.Repeat("0", 64), nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown token status = %d, want 404", w.Code)
	}

	// 7. 撤销后 → 404
	if w := post("/file/share/revoke", url.Values{"share_token": {token}}); w.Code != http.StatusOK {
		t.Fatalf("revoke status = %d, want 200", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/share/"+token+"?pwd=secret", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("download after revoke = %d, want 404", w.Code)
	}

	// 8. 越权:他人无法撤销(用直接 service 调用验证归属)
	if err := shareSvc.Revoke(context.Background(), token, "bob"); err == nil {
		t.Errorf("bob revoke should fail")
	}
}
