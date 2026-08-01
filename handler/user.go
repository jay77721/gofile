package handler

import (
	"crypto/rand"
	"encoding/hex"
	dblayer "filestore-server/db"
	mydb "filestore-server/db/mysql"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// SignupHandler 处理用户注册请求
func SignupHandler(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")

	if len(username) < 3 || len(password) < 5 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "用户名至少3位，密码至少5位", "data": nil})
		return
	}

	// 使用 bcrypt 哈希密码
	hashedPwd, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("bcrypt hash failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "注册失败，请稍后重试", "data": nil})
		return
	}

	suc := dblayer.UserSignup(username, string(hashedPwd))
	if suc {
		slog.Info("user registered", "username", username)
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "注册成功", "data": nil})
	} else {
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "用户名已存在", "data": nil})
	}
}

// SignInHandler 登录接口
func SignInHandler(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")

	if username == "" || password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "用户名和密码不能为空", "data": nil})
		return
	}

	// 从数据库获取哈希密码并验证
	if !checkPassword(username, password) {
		slog.Warn("login failed", "username", username, "reason", "invalid credentials")
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "用户名或密码错误", "data": nil})
		return
	}

	// 生成安全的随机 token
	token, err := generateToken()
	if err != nil {
		slog.Error("generate token failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "登录失败，请稍后重试", "data": nil})
		return
	}

	upRes := dblayer.UpdateToken(username, token)
	if !upRes {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "登录失败，请稍后重试", "data": nil})
		return
	}

	c.SetCookie("token", token, 3600, "/", "", false, true)
	c.SetCookie("username", username, 3600, "/", "", false, true)

	slog.Info("user logged in", "username", username)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{
			"Location": "http://" + c.Request.Host + "/static/view/home.html",
			"Username": username,
		},
	})
}

// UserInfoHandler 查询用户信息
func UserInfoHandler(c *gin.Context) {
	username, _ := c.Cookie("username")
	if username == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 1, "msg": "缺少登录信息", "data": nil})
		return
	}

	user, err := dblayer.GetUserInfo(username)
	if err != nil {
		slog.Error("get user info failed", "error", err, "username", username)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "获取用户信息失败", "data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": user})
}

// checkPassword 验证用户密码（bcrypt）
func checkPassword(username, password string) bool {
	db := mydb.DBConn()
	stmt, err := db.Prepare("SELECT user_pwd FROM tbl_user WHERE user_name=? LIMIT 1")
	if err != nil {
		slog.Error("prepare statement failed", "error", err, "op", "checkPassword")
		return false
	}
	defer stmt.Close()

	var storedHash string
	err = stmt.QueryRow(username).Scan(&storedHash)
	if err != nil {
		return false
	}

	// bcrypt 验证
	err = bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password))
	return err == nil
}

// generateToken 生成安全的随机 token（64 位十六进制）
func generateToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// isTokenValid 验证 token 是否有效
func isTokenValid(username string, token string) bool {
	stmt, err := mydb.DBConn().Prepare(
		"SELECT user_token, expired_at FROM tbl_user_token WHERE user_name=? LIMIT 1")
	if err != nil {
		slog.Error("prepare statement failed", "error", err, "op", "isTokenValid")
		return false
	}
	defer stmt.Close()

	var expiredAt time.Time
	var userToken string

	err = stmt.QueryRow(username).Scan(&userToken, &expiredAt)
	if err != nil {
		return false
	}

	if userToken != token {
		slog.Warn("token mismatch", "username", username)
		return false
	}

	if expiredAt.Before(time.Now()) {
		slog.Warn("token expired", "username", username, "expired_at", expiredAt)
		return false
	}

	return true
}

