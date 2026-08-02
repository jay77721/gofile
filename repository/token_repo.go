package repository

import (
	"database/sql"
	"fmt"
	"gofile/model"
	"time"
)

// TokenRepository 用户 token 数据访问接口
type TokenRepository interface {
	// Upsert 创建或更新 token
	Upsert(username, token string, expiredAt time.Time) (bool, error)
	// Get 获取用户 token
	Get(username string) (model.Token, error)
}

// mysqlTokenRepo MySQL 实现的 TokenRepository
type mysqlTokenRepo struct {
	db *sql.DB
}

// NewTokenRepository 创建 MySQL token 仓库
func NewTokenRepository(db *sql.DB) TokenRepository {
	return &mysqlTokenRepo{db: db}
}

func (r *mysqlTokenRepo) Upsert(username, token string, expiredAt time.Time) (bool, error) {
	stmt, err := r.db.Prepare(
		"REPLACE INTO tbl_user_token(`user_name`,`user_token`,update_at,expired_at) VALUES (?,?,?,?)")
	if err != nil {
		return false, fmt.Errorf("prepare upsert token failed: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(username, token, time.Now(), expiredAt)
	if err != nil {
		return false, fmt.Errorf("exec upsert token failed: %w", err)
	}
	return true, nil
}

func (r *mysqlTokenRepo) Get(username string) (model.Token, error) {
	var t model.Token
	err := r.db.QueryRow(
		"SELECT user_token, expired_at FROM tbl_user_token WHERE user_name=? LIMIT 1", username,
	).Scan(&t.Token, &t.ExpiredAt)
	if err != nil {
		return model.Token{}, fmt.Errorf("get token failed: %w", err)
	}
	t.Username = username
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