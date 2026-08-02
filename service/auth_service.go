package service

import (
	"gofile/repository"
	"log/slog"
	"time"
)

// AuthService 认证业务逻辑
type AuthService struct {
	tokenRepo repository.TokenRepository
}

// NewAuthService 创建认证服务
func NewAuthService(tokenRepo repository.TokenRepository) *AuthService {
	return &AuthService{tokenRepo: tokenRepo}
}

// ValidateToken 验证 token 是否有效
func (s *AuthService) ValidateToken(username, token string) bool {
	t, err := s.tokenRepo.Get(username)
	if err != nil {
		slog.Warn("token validation failed: not found", "username", username)
		return false
	}

	if t.Token != token {
		slog.Warn("token mismatch", "username", username)
		return false
	}

	if t.ExpiredAt.Before(time.Now()) {
		slog.Warn("token expired", "username", username, "expired_at", t.ExpiredAt)
		return false
	}

	return true
}