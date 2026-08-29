package repository

import (
	"context"
	"fmt"
	"gofile/internal/domain"
	"log/slog"
	"time"

	"gorm.io/gorm"
)

// UserRepository is the user data access interface
type UserRepository interface {
	// Create creates a user
	Create(ctx context.Context, username, hashedPassword string) (bool, error)
	// GetPasswordHash retrieves the user password hash
	GetPasswordHash(ctx context.Context, username string) (string, error)
	// GetInfo retrieves user information
	GetInfo(ctx context.Context, username string) (model.User, error)
}

// mysqlUserRepo is the GORM implementation of UserRepository
type mysqlUserRepo struct {
	db *gorm.DB
}

// NewUserRepository creates a GORM user repository
func NewUserRepository(db *gorm.DB) UserRepository {
	return &mysqlUserRepo{db: db}
}

func (r *mysqlUserRepo) Create(ctx context.Context, username, hashedPassword string) (bool, error) {
	user := model.User{
		Username: username,
		Password: hashedPassword,
	}
	res := r.db.WithContext(ctx).Create(&user)
	if res.Error != nil {
		// Primary key conflict (user already exists)
		return false, nil
	}
	if res.RowsAffected == 0 {
		slog.Info("user already exists", "username", username)
		return false, nil
	}
	return true, nil
}

func (r *mysqlUserRepo) GetPasswordHash(ctx context.Context, username string) (string, error) {
	var user model.User
	err := r.db.WithContext(ctx).Select("user_pwd").
		Where("user_name = ?", username).
		First(&user).Error
	if err != nil {
		return "", fmt.Errorf("get password hash failed: %w", err)
	}
	return user.Password, nil
}

func (r *mysqlUserRepo) GetInfo(ctx context.Context, username string) (model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).Where("user_name = ?", username).First(&user).Error
	if err != nil {
		return model.User{}, fmt.Errorf("get user info failed: %w", err)
	}
	return user, nil
}

// ---- Mock implementation ----

// mockUserRepo is an in-memory mock user repository
type mockUserRepo struct {
	users    map[string]string // username -> hashedPassword
	userInfo map[string]model.User
}

// NewMockUserRepository creates a mock user repository
func NewMockUserRepository() UserRepository {
	return &mockUserRepo{
		users:    make(map[string]string),
		userInfo: make(map[string]model.User),
	}
}

func (m *mockUserRepo) Create(ctx context.Context, username, hashedPassword string) (bool, error) {
	if _, exists := m.users[username]; exists {
		return false, nil
	}
	m.users[username] = hashedPassword
	m.userInfo[username] = model.User{Username: username, SignupAt: time.Now()}
	return true, nil
}

func (m *mockUserRepo) GetPasswordHash(ctx context.Context, username string) (string, error) {
	pwd, ok := m.users[username]
	if !ok {
		return "", fmt.Errorf("user not found")
	}
	return pwd, nil
}

func (m *mockUserRepo) GetInfo(ctx context.Context, username string) (model.User, error) {
	user, ok := m.userInfo[username]
	if !ok {
		return model.User{}, fmt.Errorf("user not found")
	}
	return user, nil
}

// Ensure interface implementation is checked at compile time
var _ UserRepository = (*mysqlUserRepo)(nil)
var _ UserRepository = (*mockUserRepo)(nil)
