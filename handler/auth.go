package handler

import (
	"gofile/service"
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

		if username == "" || token == "" || len(username) < 3 || !m.authSvc.ValidateToken(username, token) {
			slog.Warn("auth failed", "username", username, "path", c.Request.URL.Path)
			c.JSON(http.StatusUnauthorized, gin.H{"code": 1, "msg": "请先登录", "data": nil})
			c.Abort()
			return
		}

		c.Set("username", username)
		c.Next()
	}
}