package handler

import (
	"crypto/rand"
	"encoding/hex"
	dblayer "filestore-server/db"
	mydb "filestore-server/db/mysql"
	"filestore-server/util"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// SignupHandler 处理用户注册请求
func SignupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		http.ServeFile(w, r, "./static/view/signup.html")
		return
	}

	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, 1, "请求参数解析失败", nil)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if len(username) < 3 || len(password) < 5 {
		writeJSON(w, http.StatusBadRequest, 1, "用户名至少3位，密码至少5位", nil)
		return
	}

	// 使用 bcrypt 哈希密码
	hashedPwd, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("bcrypt hash failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, 1, "注册失败，请稍后重试", nil)
		return
	}

	suc := dblayer.UserSignup(username, string(hashedPwd))
	if suc {
		slog.Info("user registered", "username", username)
		writeJSON(w, http.StatusOK, 0, "注册成功", nil)
	} else {
		writeJSON(w, http.StatusOK, 1, "用户名已存在", nil)
	}
}

// SignInHandler 登录接口
func SignInHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, 1, "请求参数解析失败", nil)
		return
	}

	username := r.PostFormValue("username")
	password := r.PostFormValue("password")
	if len(username) == 0 || len(password) == 0 {
		writeJSON(w, http.StatusBadRequest, 1, "用户名和密码不能为空", nil)
		return
	}

	// 从数据库获取哈希密码并验证
	if !checkPassword(username, password) {
		slog.Warn("login failed", "username", username, "reason", "invalid credentials")
		writeJSON(w, http.StatusOK, 1, "用户名或密码错误", nil)
		return
	}

	// 生成安全的随机 token
	token, err := generateToken()
	if err != nil {
		slog.Error("generate token failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, 1, "登录失败，请稍后重试", nil)
		return
	}

	upRes := dblayer.UpdateToken(username, token)
	if !upRes {
		writeJSON(w, http.StatusInternalServerError, 1, "登录失败，请稍后重试", nil)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   3600,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "username",
		Value:    username,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   3600,
	})

	slog.Info("user logged in", "username", username)

	resp := util.RespMsg{
		Code: 0,
		Msg:  "ok",
		Data: struct {
			Location string `json:"Location"`
			Username string `json:"Username"`
			Token    string `json:"Token"`
		}{
			Location: "http://" + r.Host + "/static/view/home.html",
			Username: username,
			Token:    token,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(resp.JSONBytes())
}

// UserInfoHandler 查询用户信息
func UserInfoHandler(w http.ResponseWriter, r *http.Request) {
	usernameCookie, err := r.Cookie("username")
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, 1, "缺少登录信息", nil)
		return
	}

	username := usernameCookie.Value

	user, err := dblayer.GetUserInfo(username)
	if err != nil {
		slog.Error("get user info failed", "error", err, "username", username)
		writeJSON(w, http.StatusInternalServerError, 1, "获取用户信息失败", nil)
		return
	}

	resp := util.RespMsg{
		Code: 0,
		Msg:  "ok",
		Data: user,
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(resp.JSONBytes())
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

// writeJSON 统一 JSON 响应
func writeJSON(w http.ResponseWriter, statusCode int, code int, msg string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	resp := util.RespMsg{Code: code, Msg: msg, Data: data}
	w.Write(resp.JSONBytes())
}

// GenToken 保留导出函数供其他包使用（兼容）
func GenToken(username string) string {
	token, _ := generateToken()
	return token
}
