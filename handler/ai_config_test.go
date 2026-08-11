package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gofile/ai"
	"gofile/config"
	"gofile/repository"
	"gofile/service"

	"github.com/gin-gonic/gin"
)

// setupAIConfigHandler 创建测试用 AIConfigHandler + 路由(不依赖 MySQL)
func setupAIConfigHandler() (*gin.Engine, *AIConfigHandler) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{AIEmbedDim: 128, AllowPrivateAIURL: true}
	svc := service.NewAIConfigService(repository.NewMockAIConfigRepository(), cfg, ai.NewMockProvider(128))
	h := NewAIConfigHandler(svc)
	r := gin.New()

	// 模拟 AuthMiddleware:注入 username
	r.Use(func(c *gin.Context) { c.Set("username", "alice"); c.Next() })
	aiCfg := r.Group("/ai/config")
	{
		aiCfg.GET("", h.GetConfigHandler)
		aiCfg.POST("", h.SaveConfigHandler)
		aiCfg.DELETE("", h.DeleteConfigHandler)
		aiCfg.POST("/test", h.TestConfigHandler)
	}
	return r, h
}

func postJSON(t *testing.T, r *gin.Engine, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAIConfigHandlerFlow(t *testing.T) {
	r, _ := setupAIConfigHandler()

	// 1. 初始未配置
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ai/config", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"configured":false`) {
		t.Fatalf("expected unconfigured, got %d %s", w.Code, w.Body.String())
	}

	// 2. 保存配置
	w = postJSON(t, r, "/ai/config", map[string]any{
		"base_url": "https://8.8.8.8/v1", "api_key": "sk-test-abcdef1234", "model": "gpt-4o",
	})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"code":0`) {
		t.Fatalf("save failed: %d %s", w.Code, w.Body.String())
	}

	// 3. 读取:掩码,不泄露明文
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ai/config", nil))
	body := w.Body.String()
	if !strings.Contains(body, `"has_key":true`) || !strings.Contains(body, `"api_key_mask":"sk-t****1234"`) {
		t.Fatalf("unexpected view: %s", body)
	}
	if strings.Contains(body, "sk-test-abcdef1234") && !strings.Contains(body, "****") {
		t.Fatalf("view leaks plaintext key: %s", body)
	}

	// 4. 保存非法 URL → 1001
	w = postJSON(t, r, "/ai/config", map[string]any{"base_url": "ftp://x", "api_key": "k"})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"code":1001`) {
		t.Fatalf("expected 1001 for bad url, got %d %s", w.Code, w.Body.String())
	}

	// 5. 测试连接(指向本地立即失败的端点,应返回 ok:false 但 HTTP 200)
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer failSrv.Close()
	w = postJSON(t, r, "/ai/config/test", map[string]any{
		"base_url": failSrv.URL, "api_key": "sk-test",
	})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ok":false`) {
		t.Fatalf("expected test failure result, got %d %s", w.Code, w.Body.String())
	}

	// 6. 删除配置
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/ai/config", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("delete failed: %d", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ai/config", nil))
	if !strings.Contains(w.Body.String(), `"configured":false`) {
		t.Fatalf("expected unconfigured after delete: %s", w.Body.String())
	}
}

func TestAIConfigHandlerBadJSON(t *testing.T) {
	r, _ := setupAIConfigHandler()
	req := httptest.NewRequest(http.MethodPost, "/ai/config", strings.NewReader("{not-json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad json, got %d", w.Code)
	}
}
