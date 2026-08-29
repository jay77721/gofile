package repository

import (
	"context"
	"fmt"
	"gofile/internal/domain"
	"time"

	"gorm.io/gorm"
)

// TokenRepository is the user token data access interface
type TokenRepository interface {
	// Upsert creates or updates a token
	Upsert(ctx context.Context, username, token string, expiredAt time.Time) (bool, error)
	// Get retrieves a user token
	Get(ctx context.Context, username string) (model.Token, error)
	// Delete deletes a user token (logout)
	Delete(ctx context.Context, username string) error
}

// mysqlTokenRepo is the GORM implementation of TokenRepository
type mysqlTokenRepo struct {
	db *gorm.DB
}

// NewTokenRepository creates a GORM token repository
func NewTokenRepository(db *gorm.DB) TokenRepository {
	return &mysqlTokenRepo{db: db}
}

func (r *mysqlTokenRepo) Upsert(ctx context.Context, username, token string, expiredAt time.Time) (bool, error) {
	t := model.Token{
		Username:  username,
		Token:     token,
		UpdateAt:  time.Now(),
		ExpiredAt: expiredAt,
	}
	// Use Save to implement upsert (update if primary key exists, create otherwise)
	if err := r.db.WithContext(ctx).Save(&t).Error; err != nil {
		return false, fmt.Errorf("upsert token failed: %w", err)
	}
	return true, nil
}

func (r *mysqlTokenRepo) Get(ctx context.Context, username string) (model.Token, error) {
	var t model.Token
	err := r.db.WithContext(ctx).Where("user_name = ?", username).First(&t).Error
	if err != nil {
		return model.Token{}, fmt.Errorf("get token failed: %w", err)
	}
	return t, nil
}

func (r *mysqlTokenRepo) Delete(ctx context.Context, username string) error {
	if err := r.db.WithContext(ctx).Where("user_name = ?", username).Delete(&model.Token{}).Error; err != nil {
		return fmt.Errorf("delete token failed: %w", err)
	}
	return nil
}

// ---- Mock implementation ----

// mockTokenRepo is an in-memory mock token repository
type mockTokenRepo struct {
	tokens map[string]model.Token
}

// NewMockTokenRepository creates a mock token repository
func NewMockTokenRepository() TokenRepository {
	return &mockTokenRepo{tokens: make(map[string]model.Token)}
}

func (m *mockTokenRepo) Upsert(ctx context.Context, username, token string, expiredAt time.Time) (bool, error) {
	m.tokens[username] = model.Token{
		Username:  username,
		Token:     token,
		ExpiredAt: expiredAt,
	}
	return true, nil
}

func (m *mockTokenRepo) Get(ctx context.Context, username string) (model.Token, error) {
	t, ok := m.tokens[username]
	if !ok {
		return model.Token{}, fmt.Errorf("token not found")
	}
	return t, nil
}

func (m *mockTokenRepo) Delete(ctx context.Context, username string) error {
	delete(m.tokens, username)
	return nil
}

// Ensure interface implementation is checked at compile time
var _ TokenRepository = (*mysqlTokenRepo)(nil)
var _ TokenRepository = (*mockTokenRepo)(nil)
