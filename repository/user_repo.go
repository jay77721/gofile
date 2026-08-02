package repository

import (
	"database/sql"
	"fmt"
	"gofile/model"
	"log/slog"
	"time"
)

// UserRepository 用户数据访问接口
type UserRepository interface {
	// Create 创建用户
	Create(username, hashedPassword string) (bool, error)
	// GetPasswordHash 获取用户密码哈希
	GetPasswordHash(username string) (string, error)
	// GetInfo 获取用户信息
	GetInfo(username string) (model.User, error)
}

// mysqlUserRepo MySQL 实现的 UserRepository
type mysqlUserRepo struct {
	db *sql.DB
}

// NewUserRepository 创建 MySQL 用户仓库
func NewUserRepository(db *sql.DB) UserRepository {
	return &mysqlUserRepo{db: db}
}

func (r *mysqlUserRepo) Create(username, hashedPassword string) (bool, error) {
	stmt, err := r.db.Prepare(
		"INSERT IGNORE INTO tbl_user (`user_name`,`user_pwd`) VALUES (?,?)")
	if err != nil {
		return false, fmt.Errorf("prepare insert user failed: %w", err)
	}
	defer stmt.Close()

	ret, err := stmt.Exec(username, hashedPassword)
	if err != nil {
		return false, fmt.Errorf("exec insert user failed: %w", err)
	}
	rows, err := ret.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected failed: %w", err)
	}
	if rows == 0 {
		slog.Info("user already exists", "username", username)
		return false, nil
	}
	return true, nil
}

func (r *mysqlUserRepo) GetPasswordHash(username string) (string, error) {
	var hashedPwd string
	err := r.db.QueryRow(
		"SELECT user_pwd FROM tbl_user WHERE user_name=? LIMIT 1", username,
	).Scan(&hashedPwd)
	if err != nil {
		return "", fmt.Errorf("get password hash failed: %w", err)
	}
	return hashedPwd, nil
}

func (r *mysqlUserRepo) GetInfo(username string) (model.User, error) {
	stmt, err := r.db.Prepare(
		"SELECT user_name, signup_at FROM tbl_user WHERE user_name=? LIMIT 1")
	if err != nil {
		return model.User{}, fmt.Errorf("prepare get user info failed: %w", err)
	}
	defer stmt.Close()

	var user model.User
	err = stmt.QueryRow(username).Scan(&user.Username, &user.SignupAt)
	if err != nil {
		return model.User{}, fmt.Errorf("get user info failed: %w", err)
	}
	return user, nil
}

// ---- Mock 实现 ----

// mockUserRepo 内存 mock 用户仓库
type mockUserRepo struct {
	users    map[string]string // username -> hashedPassword
	userInfo map[string]model.User
}

// NewMockUserRepository 创建 mock 用户仓库
func NewMockUserRepository() UserRepository {
	return &mockUserRepo{
		users:    make(map[string]string),
		userInfo: make(map[string]model.User),
	}
}

func (m *mockUserRepo) Create(username, hashedPassword string) (bool, error) {
	if _, exists := m.users[username]; exists {
		return false, nil
	}
	m.users[username] = hashedPassword
	m.userInfo[username] = model.User{Username: username, SignupAt: time.Now()}
	return true, nil
}

func (m *mockUserRepo) GetPasswordHash(username string) (string, error) {
	pwd, ok := m.users[username]
	if !ok {
		return "", fmt.Errorf("user not found")
	}
	return pwd, nil
}

func (m *mockUserRepo) GetInfo(username string) (model.User, error) {
	user, ok := m.userInfo[username]
	if !ok {
		return model.User{}, fmt.Errorf("user not found")
	}
	return user, nil
}

// 确保编译时检查接口实现
var _ UserRepository = (*mysqlUserRepo)(nil)
var _ UserRepository = (*mockUserRepo)(nil)