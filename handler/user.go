package handler

import (
	"gofile/config"
	"gofile/service"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// UserHandler 用户 HTTP 处理器，依赖注入 UserService
type UserHandler struct {
	userSvc *service.UserService
	cfg     *config.Config
}

// NewUserHandler 创建用户处理器
func NewUserHandler(userSvc *service.UserService, cfg *config.Config) *UserHandler {
	return &UserHandler{userSvc: userSvc, cfg: cfg}
}

// SignupHandler 处理用户注册请求
func (h *UserHandler) SignupHandler(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")

	if len(username) < 3 || len(password) < 5 {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "用户名至少3位，密码至少5位")
		return
	}

	if err := h.userSvc.Signup(c.Request.Context(), username, password); err != nil {
		// 用户已存在
		respondError(c, http.StatusOK, CodeUserExists, "用户名已存在")
		return
	}

	slog.InfoContext(c.Request.Context(), "user registered", "username", username)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "注册成功", "data": nil})
}

// SignInHandler 登录接口
func (h *UserHandler) SignInHandler(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")

	if username == "" || password == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "用户名和密码不能为空")
		return
	}

	token, err := h.userSvc.Signin(c.Request.Context(), username, password)
	if err != nil {
		slog.WarnContext(c.Request.Context(), "login failed", "username", username, "reason", "invalid credentials")
		respondError(c, http.StatusOK, CodeInvalidCreds, "用户名或密码错误")
		return
	}

	// 设置 cookie（1h 有效期，token 本身在 DB 中 24h 过期）
	// Secure: 生产环境强制 HTTPS;SameSite=Lax: 缓解 CSRF(跨站请求不携带 Cookie)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("token", token, 3600, "/", "", h.cfg.CookieSecure, true)
	c.SetCookie("username", username, 3600, "/", "", h.cfg.CookieSecure, true)

	slog.InfoContext(c.Request.Context(), "user logged in", "username", username)

	// Location 协议与 Cookie Secure 保持一致（HTTPS 部署时避免跳回 http）
	scheme := "http"
	if h.cfg.CookieSecure {
		scheme = "https"
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{
			"Location": scheme + "://" + c.Request.Host + "/static/index.html",
			"Username": username,
		},
	})
}

// UserInfoHandler 查询用户信息
func (h *UserHandler) UserInfoHandler(c *gin.Context) {
	username, _ := c.Cookie("username")
	if username == "" {
		respondError(c, http.StatusUnauthorized, CodeUnauthorized, "缺少登录信息")
		return
	}

	user, err := h.userSvc.GetUserInfo(username)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "get user info failed", "error", err, "username", username)
		respondError(c, http.StatusInternalServerError, CodeInternalError, "获取用户信息失败")
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": user})
}
