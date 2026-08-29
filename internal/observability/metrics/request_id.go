package metrics

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ctxKey private context key type to prevent external injection of the same key
type ctxKey struct{}

// RequestIDKey exposed for external callers (e.g., tests) to read request_id from context
var RequestIDKey = ctxKey{}

// FromContext read request_id from context, return empty string if not found
func FromContext(ctx context.Context) string {
	if id, ok := ctx.Value(RequestIDKey).(string); ok {
		return id
	}
	return ""
}

// RequestIDMiddleware generate a UUID for each request:
//  1. Write to request context for service-layer logs to correlate via slog.InfoContext(ctx)
//  2. Write to response header X-Request-ID for client/log reconciliation
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := uuid.New().String()
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), RequestIDKey, id))
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

// ContextHandler wrap slog.Handler and attach request_id extracted from context to each record in Handle.
//
// Key design: read from Handle's ctx instead of tagging the base handler with slog.With —
// otherwise background goroutines (chunk cleanup / GC) would be incorrectly tagged with the previous request's request_id.
type ContextHandler struct {
	next slog.Handler
}

// NewContextHandler create a request_id-enhanced slog handler
func NewContextHandler(next slog.Handler) *ContextHandler {
	return &ContextHandler{next: next}
}

// Enabled delegate to the underlying handler
func (h *ContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle extract request_id from ctx and attach it to the record before delegating to the underlying handler
func (h *ContextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := FromContext(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.next.Handle(ctx, r)
}

// WithAttrs delegate to the underlying handler
func (h *ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ContextHandler{next: h.next.WithAttrs(attrs)}
}

// WithGroup delegate to the underlying handler
func (h *ContextHandler) WithGroup(name string) slog.Handler {
	return &ContextHandler{next: h.next.WithGroup(name)}
}
