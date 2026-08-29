package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// Each test uses a unique path label to avoid cross-test accumulation causing assertion drift.

func TestMetricsMiddleware_RecordsCounter(t *testing.T) {
	Register() // Idempotent, ensure metrics are registered
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

	// Request to unregistered route → 404，FullPath is empty → path label falls back to "unknown"
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
	// Default registry includes Go runtime metrics
	if !strings.Contains(body, "go_goroutines") {
		t.Error("metrics body missing go_goroutines")
	}
	if !strings.Contains(body, "http_requests_total") {
		t.Error("metrics body missing http_requests_total")
	}
}
