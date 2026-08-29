package metrics

import (
	"log/slog"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// MetricsMiddleware collect HTTP metrics (counter + duration histogram) and emit a structured access log.
//
// path is taken from c.FullPath() (Gin route template, e.g., /file/download), naturally without query parameters or file hash, no cardinality explosion.
// When no route is matched (404/405), FullPath is empty and falls back to "unknown".
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

		// Business metrics
		RecordHTTPRequest(c.Request.Method, path, strconv.Itoa(status), dur.Seconds())

		// Access log: request_id is injected into the request context by RequestIDMiddleware and automatically attached by slog
		slog.InfoContext(c.Request.Context(), "access",
			"method", c.Request.Method,
			"path", path,
			"status", status,
			"latency_ms", dur.Milliseconds(),
			"ip", c.ClientIP(),
		)
	}
}
