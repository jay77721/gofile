package handler

import (
	"gofile/internal/application/service"
	"gofile/internal/config"
	"log/slog"
	"net/http"
	"strings"

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

// passwordStrength 校验密码强度：至少 8 位，包含大写字母、小写字母、数字中的至少三类
func passwordStrength(password string) (bool, string) {
	if len(password) < 8 {
		return false, "密码至少 8 位"
	}
	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, c := range password {
		switch {
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		case c >= 'a' && c <= 'z':
			hasLower = true
		case c >= '0' && c <= '9':
			hasDigit = true
		default:
			hasSpecial = true
		}
	}
	category := 0
	if hasUpper {
		category++
	}
	if hasLower {
		category++
	}
	if hasDigit {
		category++
	}
	if hasSpecial {
		category++
	}
	if category < 3 {
		return false, "密码需包含大写字母、小写字母、数字、特殊字符中的至少三类"
	}
	return true, ""
}

func validUsername(username string) bool {
	if len(username) < 3 || len(username) > 64 {
		return false
	}
	for i, c := range username {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > 0 && c >= '0' && c <= '9') || (i > 0 && c == '_' || i > 0 && c == '-') {
			continue
		}
		return false
	}
	return true
}

// SignupHandler 处理用户注册请求
// @Summary 用户注册
// @Description 注册新用户，密码需至少 8 位且包含大写/小写/数字/特殊字符中的至少三类
// @Tags 用户
// @Accept application/x-www-form-urlencoded
// @Produce json
// @Param username formData string true "用户名（至少 3 位）"
// @Param password formData string true "密码（至少 8 位，含大写/小写/数字/特殊字符至少三类）"
// @Success 200 {object} map[string]any{code=int,msg=string,data=nil} "注册成功"
// @Failure 400 {object} map[string]any{code=int,msg=string,data=nil} "参数错误"
// @Router /user/signup [post]
func (h *UserHandler) SignupHandler(c *gin.Context) {
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")

	if !validUsername(username) {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "用户名需为3-64位，仅允许字母、数字、下划线或连字符，且首字符必须为字母")
		return
	}

	if ok, msg := passwordStrength(password); !ok {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, msg)
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
// @Summary 用户登录
// @Description 验证用户名密码，成功后通过 Set-Cookie 设置 token（HttpOnly）
// @Tags 用户
// @Accept application/x-www-form-urlencoded
// @Produce json
// @Param username formData string true "用户名"
// @Param password formData string true "密码"
// @Success 200 {object} map[string]any{code=int,msg=string,data=object{Location=string,Username=string}} "登录成功"
// @Failure 400 {object} map[string]any{code=int,msg=string,data=nil} "参数错误"
// @Router /user/signin [post]
func (h *UserHandler) SignInHandler(c *gin.Context) {
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")

	if !validUsername(username) || password == "" {
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

// LogoutHandler 退出登录
// @Summary 退出登录
// @Description 删除服务端 token 并清除客户端 Cookie
// @Tags 用户
// @Produce json
// @Success 200 {object} map[string]any{code=int,msg=string,data=nil} "已退出登录"
// @Router /user/logout [post]
func (h *UserHandler) LogoutHandler(c *gin.Context) {
	username, _ := c.Cookie("username")
	if username != "" {
		if err := h.userSvc.Logout(c.Request.Context(), username); err != nil {
			slog.WarnContext(c.Request.Context(), "logout: delete token failed", "error", err, "username", username)
		}
	}
	// 无论服务端是否成功,都清除客户端 Cookie(幂等)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("token", "", -1, "/", "", h.cfg.CookieSecure, true)
	c.SetCookie("username", "", -1, "/", "", h.cfg.CookieSecure, true)
	slog.InfoContext(c.Request.Context(), "user logged out", "username", username)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已退出登录", "data": nil})
}

// UserInfoHandler 查询用户信息
// @Summary 获取用户信息
// @Tags 用户
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} map[string]any{code=int,msg=string,data=object} "用户信息"
// @Router /user/info [get]
func (h *UserHandler) UserInfoHandler(c *gin.Context) {
	username, _ := c.Cookie("username")
	if username == "" {
		respondError(c, http.StatusUnauthorized, CodeUnauthorized, "缺少登录信息")
		return
	}

	user, err := h.userSvc.GetUserInfo(c.Request.Context(), username)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "get user info failed", "error", err, "username", username)
		respondError(c, http.StatusInternalServerError, CodeInternalError, "获取用户信息失败")
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": user})
}
