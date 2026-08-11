package repository

import (
	"fmt"
	"gofile/model"
	"log/slog"
	"time"

	"gorm.io/gorm"
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

// mysqlUserRepo GORM 实现的 UserRepository
type mysqlUserRepo struct {
	db *gorm.DB
}

// NewUserRepository 创建 GORM 用户仓库
func NewUserRepository(db *gorm.DB) UserRepository {
	return &mysqlUserRepo{db: db}
}

func (r *mysqlUserRepo) Create(username, hashedPassword string) (bool, error) {
	user := model.User{
		Username: username,
		Password: hashedPassword,
	}
	res := r.db.Create(&user)
	if res.Error != nil {
		// 主键冲突（用户已存在）
		return false, nil
	}
	if res.RowsAffected == 0 {
		slog.Info("user already exists", "username", username)
		return false, nil
	}
	return true, nil
}

func (r *mysqlUserRepo) GetPasswordHash(username string) (string, error) {
	var user model.User
	err := r.db.Select("user_pwd").
		Where("user_name = ?", username).
		First(&user).Error
	if err != nil {
		return "", fmt.Errorf("get password hash failed: %w", err)
	}
	return user.Password, nil
}

func (r *mysqlUserRepo) GetInfo(username string) (model.User, error) {
	var user model.User
	err := r.db.Where("user_name = ?", username).First(&user).Error
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
