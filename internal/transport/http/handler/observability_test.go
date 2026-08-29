package handler

import (
	"gofile/internal/observability/metrics"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestObservabilityChain verify the observability chain end-to-end:
// RequestID → Metrics → Recovery After a full-chain request via RequestID -> Metrics -> Recovery, the route's metric line should appear in /metrics.
func TestObservabilityChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Same middleware order as main.go
	r.Use(metrics.RequestIDMiddleware())
	r.Use(metrics.MetricsMiddleware())
	r.Use(gin.Recovery())
	r.GET("/healthz", HealthCheckHandler)

	// Real request: verify X-Request-ID header
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", w.Code)
	}
	if w.Header().Get("X-Request-ID") == "" {
		t.Error("missing X-Request-ID response header")
	}

	// Fetch /metrics and assert the route's metric line is recorded
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
