package handler

import (
	"gofile/internal/observability/metrics"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestObservabilityChain 端到端验证可观测性链路：
// RequestID → Metrics → Recovery 全链请求后，/metrics 中能看到该路由的指标行。
func TestObservabilityChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// 与 main.go 一致的中间件顺序
	r.Use(metrics.RequestIDMiddleware())
	r.Use(metrics.MetricsMiddleware())
	r.Use(gin.Recovery())
	r.GET("/healthz", HealthCheckHandler)

	// 真实请求：验证 X-Request-ID 头
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", w.Code)
	}
	if w.Header().Get("X-Request-ID") == "" {
		t.Error("missing X-Request-ID response header")
	}

	// 抓取 /metrics，断言该路由的指标行已记录
	req2 := httptest.NewRequest("GET", "/metrics", nil)
	w2 := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200", w2.Code)
	}
	body := w2.Body.String()
	want := `http_requests_total{method="GET",path="/healthz",status="200"}`
	if !strings.Contains(body, want) {
		t.Errorf("metrics body missing route line %q:\n%s", want, body)
	}
}
