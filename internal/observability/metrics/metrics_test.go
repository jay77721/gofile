package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// 每个测试使用唯一的 path 标签，避免跨测试累加导致断言漂移。

func TestMetricsMiddleware_RecordsCounter(t *testing.T) {
	Register() // 幂等，确保指标已注册
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(MetricsMiddleware())
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	req := httptest.NewRequest("GET", "/ping", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	got := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/ping", "200"))
	if got != 1 {
		t.Errorf("http_requests_total = %v, want 1", got)
	}
}

func TestMetricsMiddleware_UnknownPath(t *testing.T) {
	Register()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(MetricsMiddleware())
	r.GET("/ok", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	// 请求未注册路由 → 404，FullPath 为空 → path 标签回退 "unknown"
	req := httptest.NewRequest("GET", "/no-such-route", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}

	got := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "unknown", "404"))
	if got != 1 {
		t.Errorf("http_requests_total{path=\"unknown\"} = %v, want 1", got)
	}
}

func TestMetricsHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("content-type = %q, want text/plain", ct)
	}
	body := w.Body.String()
	// 默认注册表附带 go 运行时指标
	if !strings.Contains(body, "go_goroutines") {
		t.Error("metrics body missing go_goroutines")
	}
	if !strings.Contains(body, "http_requests_total") {
		t.Error("metrics body missing http_requests_total")
	}
}
