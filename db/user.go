package db

import (
	mydb "filestore-server/db/mysql"
	"log/slog"
	"time"
)

// UserSignup 通过用户名及密码完成 user 表的注册操作
func UserSignup(username string, password string) bool {
	stmt, err := mydb.DBConn().Prepare(
		"INSERT IGNORE INTO tbl_user (`user_name`,`user_pwd`) VALUES (?,?)")
	if err != nil {
		slog.Error("prepare statement failed", "error", err, "op", "UserSignup")
		return false
	}
	defer stmt.Close()

	ret, err := stmt.Exec(username, password)
	if err != nil {
		slog.Error("exec failed", "error", err, "op", "UserSignup")
		return false
	}

	rowsAffected, err := ret.RowsAffected()
	if err != nil {
		slog.Error("rows affected failed", "error", err, "op", "UserSignup")
		return false
	}

	if rowsAffected == 0 {
		slog.Info("user already exists", "username", username)
		return false
	}

	return true
}

// UserSignin 验证用户名和密码（已迁移至 handler 层 bcrypt 验证，此函数保留兼容）
func UserSignin(username string, encpwd string) bool {
	stmt, err := mydb.DBConn().Prepare("SELECT user_pwd FROM tbl_user WHERE user_name=? LIMIT 1")
	if err != nil {
		slog.Error("prepare statement failed", "error", err, "op", "UserSignin")
		return false
	}
	defer stmt.Close()

	var storedPwd string
	err = stmt.QueryRow(username).Scan(&storedPwd)
	if err != nil {
		return false
	}

	return storedPwd == encpwd
}

// UpdateToken 刷新用户登录的 token
func UpdateToken(username string, token string) bool {
	stmt, err := mydb.DBConn().Prepare(
		"REPLACE INTO tbl_user_token(`user_name`,`user_token`,update_at,expired_at) VALUES (?,?,?,?)")
	if err != nil {
		slog.Error("prepare statement failed", "error", err, "op", "UpdateToken")
		return false
	}
	defer stmt.Close()

	updateAt := time.Now()
	expireAt := updateAt.Add(24 * time.Hour) // token 有效期 24h

	_, err = stmt.Exec(username, token, updateAt, expireAt)
	if err != nil {
		slog.Error("exec failed", "error", err, "op", "UpdateToken")
		return false
	}
	return true
}

// User 用户信息结构
type User struct {
	Username     string `json:"Username"`
	Email        string `json:"Email"`
	Phone        string `json:"Phone"`
	SignupAt     string `json:"SignupAt"`
	LastActiveAt string `json:"LastActiveAt"`
	Status       int    `json:"Status"`
}

// GetUserInfo 查询用户信息
func GetUserInfo(username string) (User, error) {
	user := User{}

	stmt, err := mydb.DBConn().Prepare(
		"SELECT user_name, signup_at FROM tbl_user WHERE user_name=? LIMIT 1")
	if err != nil {
		slog.Error("prepare statement failed", "error", err, "op", "GetUserInfo")
		return user, err
	}
	defer stmt.Close()

	err = stmt.QueryRow(username).Scan(&user.Username, &user.SignupAt)
	if err != nil {
		return user, err
	}
	return user, nil
}
