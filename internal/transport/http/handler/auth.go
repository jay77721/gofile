package handler

import (
	"gofile/internal/application/service"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware 认证中间件，依赖注入 AuthService
type AuthMiddleware struct {
	authSvc *service.AuthService
}

// NewAuthMiddleware 创建认证中间件
func NewAuthMiddleware(authSvc *service.AuthService) *AuthMiddleware {
	return &AuthMiddleware{authSvc: authSvc}
}

// Middleware 返回 Gin 认证中间件
func (m *AuthMiddleware) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		username, _ := c.Cookie("username")
		token, _ := c.Cookie("token")

		if username == "" || token == "" || len(username) < 3 || !m.authSvc.ValidateToken(c.Request.Context(), username, token) {
			slog.WarnContext(c.Request.Context(), "auth failed", "username", username, "path", c.Request.URL.Path)
			respondError(c, http.StatusUnauthorized, CodeUnauthorized, "请先登录")
			c.Abort()
			return
		}

		c.Set("username", username)
		c.Next()
	}
}
