package metrics

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ctxKey 私有 context key 类型，防止外部注入同名 key
type ctxKey struct{}

// RequestIDKey 暴露给外部（如测试）从 context 读取 request_id
var RequestIDKey = ctxKey{}

// FromContext 从 context 读取 request_id，不存在时返回空字符串
func FromContext(ctx context.Context) string {
	if id, ok := ctx.Value(RequestIDKey).(string); ok {
		return id
	}
	return ""
}

// RequestIDMiddleware 为每个请求生成 UUID：
//  1. 写入请求 context，供 service 层日志通过 slog.InfoContext(ctx) 串联
//  2. 写入响应头 X-Request-ID，供客户端/日志系统对账
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := uuid.New().String()
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), RequestIDKey, id))
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

// ContextHandler 包装 slog.Handler，在 Handle 内从 context 提取 request_id 附加到每条记录。
//
// 关键设计：从 Handle 的 ctx 读取，而不是用 slog.With 在 base handler 上打标签——
// 否则后台 goroutine（chunk cleanup / GC）的日志会被错误打上上一个请求的 request_id。
type ContextHandler struct {
	next slog.Handler
}

// NewContextHandler 创建 request_id 增强的 slog handler
func NewContextHandler(next slog.Handler) *ContextHandler {
	return &ContextHandler{next: next}
}

// Enabled 委托给底层 handler
func (h *ContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle 从 ctx 提取 request_id 附加到记录后交给底层 handler
func (h *ContextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := FromContext(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.next.Handle(ctx, r)
}

// WithAttrs 委托给底层 handler
func (h *ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ContextHandler{next: h.next.WithAttrs(attrs)}
}

// WithGroup 委托给底层 handler
func (h *ContextHandler) WithGroup(name string) slog.Handler {
	return &ContextHandler{next: h.next.WithGroup(name)}
}
