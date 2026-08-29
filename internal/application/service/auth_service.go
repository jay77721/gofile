package service

import (
	"context"
	"gofile/internal/port"
	"log/slog"
	"time"
)

// AuthService handles authentication business logic.
type AuthService struct {
	tokenRepo port.TokenRepository
}

// NewAuthService creates the authentication service.
func NewAuthService(tokenRepo port.TokenRepository) *AuthService {
	return &AuthService{tokenRepo: tokenRepo}
}

// ValidateToken validates whether a token is valid.
func (s *AuthService) ValidateToken(ctx context.Context, username, token string) bool {
	t, err := s.tokenRepo.Get(ctx, username)
	if err != nil {
		slog.WarnContext(ctx, "token validation failed: not found", "username", username)
		return false
	}

	if t.Token != token {
		slog.WarnContext(ctx, "token mismatch", "username", username)
		return false
	}

	if t.ExpiredAt.Before(time.Now()) {
		slog.WarnContext(ctx, "token expired", "username", username, "expired_at", t.ExpiredAt)
		return false
	}

	return true
}
