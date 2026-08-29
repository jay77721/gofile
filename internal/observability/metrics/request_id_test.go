package metrics

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestRequestIDMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.GET("/", func(c *gin.Context) {
		id := FromContext(c.Request.Context())
		if id == "" {
			t.Error("request_id not found in request context")
		}
		c.String(http.StatusOK, id)
	})

	// First request: header set + context round-trip consistency + UUID format
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	headerID := w.Header().Get("X-Request-ID")
	if headerID == "" {
		t.Fatal("X-Request-ID header not set")
	}
	if _, err := uuid.Parse(headerID); err != nil {
		t.Errorf("X-Request-ID is not a valid UUID: %q", headerID)
	}
	if w.Body.String() != headerID {
		t.Errorf("context request_id %q != header %q", w.Body.String(), headerID)
	}

	// Second request: request_id must differ
	req2 := httptest.NewRequest("GET", "/", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Header().Get("X-Request-ID") == headerID {
		t.Error("request_id should be unique per request")
	}
}

func TestFromContext_Empty(t *testing.T) {
	if got := FromContext(context.Background()); got != "" {
		t.Errorf("FromContext(background) = %q, want empty", got)
	}
}

// TestContextHandler verify ContextHandler only attaches when context carries request_id
func TestContextHandler(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	old := slog.Default()
	slog.SetDefault(slog.New(NewContextHandler(base)))
	t.Cleanup(func() { slog.SetDefault(old) })

	// Context with request_id → output must contain it
	ctx := context.WithValue(context.Background(), RequestIDKey, "test-id-123")
	slog.InfoContext(ctx, "with id")
	if out := buf.String(); !strings.Contains(out, "test-id-123") {
		t.Errorf("InfoContext with id: output should contain request_id, got %q", out)
	}

	// Background context → should not attach request_id
	buf.Reset()
	slog.InfoContext(context.Background(), "no id")
	if out := buf.String(); strings.Contains(out, "request_id") {
		t.Errorf("InfoContext(background): unexpected request_id in %q", out)
	}

	// Bare slog.Info → should not attach request_id
	buf.Reset()
	slog.Info("bare")
	if out := buf.String(); strings.Contains(out, "request_id") {
		t.Errorf("bare slog.Info: unexpected request_id in %q", out)
	}
}
