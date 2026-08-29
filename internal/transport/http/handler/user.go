package handler

import (
	"gofile/internal/application/service"
	"gofile/internal/config"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// UserHandler user HTTP handler, injected with UserService
type UserHandler struct {
	userSvc *service.UserService
	cfg     *config.Config
}

// NewUserHandler create the user handler
func NewUserHandler(userSvc *service.UserService, cfg *config.Config) *UserHandler {
	return &UserHandler{userSvc: userSvc, cfg: cfg}
}

// passwordStrength validate password strength: at least 8 chars, containing at least three of uppercase, lowercase, digits, and special chars
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

// SignupHandler handle user registration requests
// @Summary Register user
// @Description Register a new user; password must be at least 8 chars and contain at least three of uppercase/lowercase/digits/special chars
// @Tags User
// @Accept application/x-www-form-urlencoded
// @Produce json
// @Param username formData string true "Username (at least 3 chars)"
// @Param password formData string true "Password (at least 8 chars, at least three of uppercase/lowercase/digits/special)"
// @Success 200 {object} map[string]any{code=int,msg=string,data=nil} "Registered successfully"
// @Failure 400 {object} map[string]any{code=int,msg=string,data=nil} "Invalid params"
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
		// User already exists
		respondError(c, http.StatusOK, CodeUserExists, "用户名已存在")
		return
	}

	slog.InfoContext(c.Request.Context(), "user registered", "username", username)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "注册成功", "data": nil})
}

// SignInHandler handle login
// @Summary User login
// @Description Validate username/password and set token via Set-Cookie on success (HttpOnly)
// @Tags User
// @Accept application/x-www-form-urlencoded
// @Produce json
// @Param username formData string true "Username"
// @Param password formData string true "Password"
// @Success 200 {object} map[string]any{code=int,msg=string,data=object{Location=string,Username=string}} "Login succeeded"
// @Failure 400 {object} map[string]any{code=int,msg=string,data=nil} "Invalid params"
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

	// Set cookie (1h expiry; token itself expires in 24h in DB)
	// Secure: enforce HTTPS in production; SameSite=Lax: mitigate CSRF (cross-site requests do not carry cookies)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("token", token, 3600, "/", "", h.cfg.CookieSecure, true)
	c.SetCookie("username", username, 3600, "/", "", h.cfg.CookieSecure, true)

	slog.InfoContext(c.Request.Context(), "user logged in", "username", username)

	// Location Keep Location scheme consistent with Cookie Secure (avoid falling back to http on HTTPS deployment)
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

// LogoutHandler handle logout
// @Summary handle logout
// @Description Delete server-side token and clear client cookie
// @Tags User
// @Produce json
// @Success 200 {object} map[string]any{code=int,msg=string,data=nil} "Logged out"
// @Router /user/logout [post]
func (h *UserHandler) LogoutHandler(c *gin.Context) {
	username, _ := c.Cookie("username")
	if username != "" {
		if err := h.userSvc.Logout(c.Request.Context(), username); err != nil {
			slog.WarnContext(c.Request.Context(), "logout: delete token failed", "error", err, "username", username)
		}
	}
	// Clear client cookies regardless of server success (idempotent)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("token", "", -1, "/", "", h.cfg.CookieSecure, true)
	c.SetCookie("username", "", -1, "/", "", h.cfg.CookieSecure, true)
	slog.InfoContext(c.Request.Context(), "user logged out", "username", username)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已退出登录", "data": nil})
}

// UserInfoHandler query user information
// @Summary Get user info
// @Tags User
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} map[string]any{code=int,msg=string,data=object} "User info"
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
