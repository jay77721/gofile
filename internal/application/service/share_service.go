package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"gofile/internal/domain"
	"gofile/internal/port"
	"log/slog"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Sentinel errors for sharing; handlers map these to HTTP statuses.
var (
	ErrShareNotFound = errors.New("share not found or expired")
	ErrShareWrongPwd = errors.New("share password incorrect")
	ErrShareFileGone = errors.New("shared file unavailable")
)

// ShareService handles file sharing business logic.
type ShareService struct {
	shareRepo port.ShareRepository
	fileRepo  port.FileRepository
}

// NewShareService creates the sharing service.
func NewShareService(shareRepo port.ShareRepository, fileRepo port.FileRepository) *ShareService {
	return &ShareService{shareRepo: shareRepo, fileRepo: fileRepo}
}

// Create creates a share: verifies ownership, generates a 64-char hex token, optional password.
// days range is 1-30, defaults to 7.
func (s *ShareService) Create(ctx context.Context, username, filehash string, days int, password string) (*model.Share, error) {
	if _, err := s.fileRepo.GetByHash(ctx, filehash, username); err != nil {
		return nil, fmt.Errorf("file not found or no permission")
	}
	if days < 1 || days > 30 {
		days = 7
	}

	token, err := randomToken()
	if err != nil {
		return nil, fmt.Errorf("generate share token failed: %w", err)
	}

	share := &model.Share{
		ShareToken: token,
		FileSha1:   filehash,
		UserName:   username,
		ExpireAt:   time.Now().Add(time.Duration(days) * 24 * time.Hour),
	}
	if password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("hash share password failed: %w", err)
		}
		share.PasswordHash = string(hash)
	}

	if err := s.shareRepo.CreateShare(ctx, share); err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "share created", "filehash", filehash, "username", username, "days", days, "has_password", password != "")
	return share, nil
}

// List lists shares for the current user.
func (s *ShareService) List(ctx context.Context, username string) ([]model.Share, error) {
	shares, err := s.shareRepo.ListShares(ctx, username)
	if err != nil {
		return nil, err
	}
	// populate serialized fields and clear hash (PasswordHash is never sent; has_password is for frontend display)
	for i := range shares {
		shares[i].HasPassword = shares[i].PasswordHash != ""
		shares[i].PasswordHash = "" // defense in depth: never leak even if serialization is misconfigured
	}
	return shares, nil
}

// Revoke revokes a share (verifies ownership).
func (s *ShareService) Revoke(ctx context.Context, token, username string) error {
	ok, err := s.shareRepo.DeleteShare(ctx, token, username)
	if err != nil {
		return err
	}
	if !ok {
		return ErrShareNotFound
	}
	slog.InfoContext(ctx, "share revoked", "token", token, "username", username)
	return nil
}

// Resolve resolves a share token, validates expiration and password, and returns file metadata (for anonymous download).
// Returns ErrShareFileGone when the file has been soft-deleted or permanently removed.
func (s *ShareService) Resolve(ctx context.Context, token, password string) (model.FileMeta, error) {
	share, err := s.shareRepo.GetShareByToken(ctx, token)
	if err != nil {
		return model.FileMeta{}, ErrShareNotFound
	}
	if time.Now().After(share.ExpireAt) {
		return model.FileMeta{}, ErrShareNotFound // expired is treated as not found
	}
	if share.PasswordHash != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(share.PasswordHash), []byte(password)); err != nil {
			return model.FileMeta{}, ErrShareWrongPwd
		}
	}

	// verify ownership as the sharer: GetByHash fails after soft delete / permanent deletion
	fMeta, err := s.fileRepo.GetByHash(ctx, share.FileSha1, share.UserName)
	if err != nil {
		return model.FileMeta{}, ErrShareFileGone
	}
	return fMeta, nil
}

// randomToken generates a 64-char hex share token (crypto/rand, prevents enumeration).
func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
