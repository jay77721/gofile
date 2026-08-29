package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"gofile/internal/domain"
	"gofile/internal/port"
	"log/slog"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// UserService handles user business logic.
type UserService struct {
	userRepo  port.UserRepository
	tokenRepo port.TokenRepository
}

// NewUserService creates the user service.
func NewUserService(userRepo port.UserRepository, tokenRepo port.TokenRepository) *UserService {
	return &UserService{userRepo: userRepo, tokenRepo: tokenRepo}
}

// Signup registers a user.
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

// Signin logs a user in and returns a token.
func (s *UserService) Signin(ctx context.Context, username, password string) (string, error) {
	// get password hash
	storedHash, err := s.userRepo.GetPasswordHash(ctx, username)
	if err != nil {
		slog.WarnContext(ctx, "login failed: user not found", "username", username)
		return "", fmt.Errorf("invalid credentials")
	}

	// verify password
	if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password)); err != nil {
		slog.WarnContext(ctx, "login failed: wrong password", "username", username)
		return "", fmt.Errorf("invalid credentials")
	}

	// generate token
	token, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("generate token failed: %w", err)
	}

	// store token (expires in 24h)
	expiredAt := time.Now().Add(24 * time.Hour)
	if _, err := s.tokenRepo.Upsert(ctx, username, token, expiredAt); err != nil {
		return "", fmt.Errorf("save token failed: %w", err)
	}

	slog.InfoContext(ctx, "user logged in", "username", username)
	return token, nil
}

// GetUserInfo gets user information.
func (s *UserService) GetUserInfo(ctx context.Context, username string) (model.User, error) {
	return s.userRepo.GetInfo(ctx, username)
}

// Logout logs out: deletes the server-side token (client cookie is cleared by handler).
func (s *UserService) Logout(ctx context.Context, username string) error {
	return s.tokenRepo.Delete(ctx, username)
}

// generateToken generates a secure random token (64-char hex).
func generateToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
