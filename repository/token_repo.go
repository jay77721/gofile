package repository

import (
	"fmt"
	"gofile/model"
	"time"

	"gorm.io/gorm"
)

// TokenRepository 用户 token 数据访问接口
type TokenRepository interface {
	// Upsert 创建或更新 token
	Upsert(username, token string, expiredAt time.Time) (bool, error)
	// Get 获取用户 token
	Get(username string) (model.Token, error)
}

// mysqlTokenRepo GORM 实现的 TokenRepository
type mysqlTokenRepo struct {
	db *gorm.DB
}

// NewTokenRepository 创建 GORM token 仓库
func NewTokenRepository(db *gorm.DB) TokenRepository {
	return &mysqlTokenRepo{db: db}
}

func (r *mysqlTokenRepo) Upsert(username, token string, expiredAt time.Time) (bool, error) {
	t := model.Token{
		Username:  username,
		Token:     token,
		UpdateAt:  time.Now(),
		ExpiredAt: expiredAt,
	}
	// 使用 Save 实现 upsert（主键存在则更新，不存在则创建）
	if err := r.db.Save(&t).Error; err != nil {
		return false, fmt.Errorf("upsert token failed: %w", err)
	}
	return true, nil
}

func (r *mysqlTokenRepo) Get(username string) (model.Token, error) {
	var t model.Token
	err := r.db.Where("user_name = ?", username).First(&t).Error
	if err != nil {
		return model.Token{}, fmt.Errorf("get token failed: %w", err)
	}
	return t, nil
}

// ---- Mock 实现 ----

// mockTokenRepo 内存 mock token 仓库
type mockTokenRepo struct {
	tokens map[string]model.Token
}

// NewMockTokenRepository 创建 mock token 仓库
func NewMockTokenRepository() TokenRepository {
	return &mockTokenRepo{tokens: make(map[string]model.Token)}
}

func (m *mockTokenRepo) Upsert(username, token string, expiredAt time.Time) (bool, error) {
	m.tokens[username] = model.Token{
		Username:  username,
		Token:     token,
		ExpiredAt: expiredAt,
	}
	return true, nil
}

func (m *mockTokenRepo) Get(username string) (model.Token, error) {
	t, ok := m.tokens[username]
	if !ok {
		return model.Token{}, fmt.Errorf("token not found")
	}
	return t, nil
}

// 确保编译时检查接口实现
var _ TokenRepository = (*mysqlTokenRepo)(nil)
var _ TokenRepository = (*mockTokenRepo)(nil)