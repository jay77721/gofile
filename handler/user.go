package handler

import (
	"gofile/service"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// UserHandler 用户 HTTP 处理器，依赖注入 UserService
type UserHandler struct {
	userSvc *service.UserService
}

// NewUserHandler 创建用户处理器
func NewUserHandler(userSvc *service.UserService) *UserHandler {
	return &UserHandler{userSvc: userSvc}
}

// SignupHandler 处理用户注册请求
func (h *UserHandler) SignupHandler(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")

	if len(username) < 3 || len(password) < 5 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "用户名至少3位，密码至少5位", "data": nil})
		return
	}

	if err := h.userSvc.Signup(username, password); err != nil {
		// 用户已存在
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "用户名已存在", "data": nil})
		return
	}

	slog.Info("user registered", "username", username)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "注册成功", "data": nil})
}

// SignInHandler 登录接口
func (h *UserHandler) SignInHandler(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")

	if username == "" || password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "用户名和密码不能为空", "data": nil})
		return
	}

	token, err := h.userSvc.Signin(username, password)
	if err != nil {
		slog.Warn("login failed", "username", username, "reason", "invalid credentials")
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "用户名或密码错误", "data": nil})
		return
	}

	// 设置 cookie（1h 有效期，token 本身在 DB 中 24h 过期）
	c.SetCookie("token", token, 3600, "/", "", false, true)
	c.SetCookie("username", username, 3600, "/", "", false, true)

	slog.Info("user logged in", "username", username)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{
			"Location": "http://" + c.Request.Host + "/static/index.html",
			"Username": username,
		},
	})
}

// UserInfoHandler 查询用户信息
func (h *UserHandler) UserInfoHandler(c *gin.Context) {
	username, _ := c.Cookie("username")
	if username == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 1, "msg": "缺少登录信息", "data": nil})
		return
	}

	user, err := h.userSvc.GetUserInfo(username)
	if err != nil {
		slog.Error("get user info failed", "error", err, "username", username)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "获取用户信息失败", "data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": user})
}