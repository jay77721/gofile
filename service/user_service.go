package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"gofile/model"
	"gofile/repository"
	"log/slog"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// UserService 用户业务逻辑
type UserService struct {
	userRepo  repository.UserRepository
	tokenRepo repository.TokenRepository
}

// NewUserService 创建用户服务
func NewUserService(userRepo repository.UserRepository, tokenRepo repository.TokenRepository) *UserService {
	return &UserService{userRepo: userRepo, tokenRepo: tokenRepo}
}

// Signup 用户注册
func (s *UserService) Signup(ctx context.Context, username, password string) error {
	hashedPwd, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("bcrypt hash failed: %w", err)
	}

	ok, err := s.userRepo.Create(ctx, username, string(hashedPwd))
	if err != nil {
		return fmt.Errorf("create user failed: %w", err)
	}
	if !ok {
		return fmt.Errorf("username already exists")
	}

	slog.InfoContext(ctx, "user registered", "username", username)
	return nil
}

// Signin 用户登录，返回 token
func (s *UserService) Signin(ctx context.Context, username, password string) (string, error) {
	// 获取密码哈希
	storedHash, err := s.userRepo.GetPasswordHash(ctx, username)
	if err != nil {
		slog.WarnContext(ctx, "login failed: user not found", "username", username)
		return "", fmt.Errorf("invalid credentials")
	}

	// 验证密码
	if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password)); err != nil {
		slog.WarnContext(ctx, "login failed: wrong password", "username", username)
		return "", fmt.Errorf("invalid credentials")
	}

	// 生成 token
	token, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("generate token failed: %w", err)
	}

	// 存储 token（24h 过期）
	expiredAt := time.Now().Add(24 * time.Hour)
	if _, err := s.tokenRepo.Upsert(ctx, username, token, expiredAt); err != nil {
		return "", fmt.Errorf("save token failed: %w", err)
	}

	slog.InfoContext(ctx, "user logged in", "username", username)
	return token, nil
}

// GetUserInfo 获取用户信息
func (s *UserService) GetUserInfo(ctx context.Context, username string) (model.User, error) {
	return s.userRepo.GetInfo(ctx, username)
}

// Logout 登出:删除服务端 token(客户端 Cookie 由 handler 清除)
func (s *UserService) Logout(ctx context.Context, username string) error {
	return s.tokenRepo.Delete(ctx, username)
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
