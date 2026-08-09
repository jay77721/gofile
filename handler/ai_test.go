package handler

import (
	"encoding/json"
	"gofile/ai"
	"gofile/repository"
	"gofile/service"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAIHandler_SearchHandler_MissingQ(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewAIHandler(&service.AIService{})
	r := gin.New()
	r.GET("/ai/search", withAuth(h.SearchHandler))

	req := httptest.NewRequest("GET", "/ai/search", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing q, got %d", w.Code)
	}
}

func TestAIHandler_SearchHandler_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 注入 mock provider，使 Embed 走确定性向量（indexer 为 nil 时走 fallbackLike）
	provider := ai.NewMockProvider(16)
	repo := repository.NewMockFileRepository()
	h := NewAIHandler(service.NewAIService(nil, provider, repo))
	r := gin.New()
	r.GET("/ai/search", withAuth(h.SearchHandler))

	req := httptest.NewRequest("GET", "/ai/search?q=test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp["code"].(float64) != 0 {
		t.Errorf("expected code 0, got %v", resp["code"])
	}
}

func TestAIHandler_SimilarHandler_MissingFilehash(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewAIHandler(&service.AIService{})
	r := gin.New()
	r.GET("/ai/similar", withAuth(h.SimilarHandler))

	req := httptest.NewRequest("GET", "/ai/similar", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing filehash, got %d", w.Code)
	}
}

func TestAIHandler_DuplicatesHandler_MissingFilehash(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewAIHandler(&service.AIService{})
	r := gin.New()
	r.GET("/ai/duplicates", withAuth(h.DuplicatesHandler))

	req := httptest.NewRequest("GET", "/ai/duplicates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing filehash, got %d", w.Code)
	}
}

// withAuth 注入测试用的 username（模拟 AuthMiddleware 行为）
func withAuth(h gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("username", "testuser")
		h(c)
	}
}
