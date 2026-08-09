package metrics

import (
	"log/slog"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// MetricsMiddleware 采集 HTTP 指标（计数器 + 耗时直方图），并输出一条结构化访问日志。
//
// path 取 c.FullPath()（gin 路由模板，如 /file/download），天然不含 query 参数与文件 hash，无基数爆炸。
// 未匹配到路由（404/405）时 FullPath 为空，回退为 "unknown"。
func MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		path := c.FullPath()
		if path == "" {
			path = "unknown"
		}
		status := c.Writer.Status()
		dur := time.Since(start)

		// 业务指标
		RecordHTTPRequest(c.Request.Method, path, strconv.Itoa(status), dur.Seconds())

		// 访问日志：request_id 由 RequestIDMiddleware 注入 request context，slog 自动附加
		slog.InfoContext(c.Request.Context(), "access",
			"method", c.Request.Method,
			"path", path,
			"status", status,
			"latency_ms", dur.Milliseconds(),
			"ip", c.ClientIP(),
		)
	}
}
