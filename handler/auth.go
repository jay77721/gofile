package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware Gin 认证中间件，验证用户 Cookie 登录状态
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		username, _ := c.Cookie("username")
		token, _ := c.Cookie("token")

		if username == "" || token == "" || len(username) < 3 || !isTokenValid(username, token) {
			slog.Warn("auth failed", "username", username, "path", c.Request.URL.Path)
			c.JSON(http.StatusUnauthorized, gin.H{"code": 1, "msg": "请先登录", "data": nil})
			c.Abort()
			return
		}

		c.Set("username", username)
		c.Next()
	}
}
