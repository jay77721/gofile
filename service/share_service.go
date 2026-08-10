package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"gofile/model"
	"gofile/repository"
	"log/slog"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// 分享相关哨兵错误,handler 据此映射 HTTP 状态
var (
	ErrShareNotFound = errors.New("share not found or expired")
	ErrShareWrongPwd = errors.New("share password incorrect")
	ErrShareFileGone = errors.New("shared file unavailable")
)

// ShareService 文件分享业务逻辑
type ShareService struct {
	shareRepo repository.ShareRepository
	fileRepo  repository.FileRepository
}

// NewShareService 创建分享服务
func NewShareService(shareRepo repository.ShareRepository, fileRepo repository.FileRepository) *ShareService {
	return &ShareService{shareRepo: shareRepo, fileRepo: fileRepo}
}

// Create 创建分享:校验所有权,生成 64 位 hex 令牌,可选提取码
// days 取值范围 1-30,默认 7
func (s *ShareService) Create(ctx context.Context, username, filehash string, days int, password string) (*model.Share, error) {
	if _, err := s.fileRepo.GetByHash(filehash, username); err != nil {
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

	if err := s.shareRepo.CreateShare(share); err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "share created", "filehash", filehash, "username", username, "days", days, "has_password", password != "")
	return share, nil
}

// List 列出当前用户的分享
func (s *ShareService) List(username string) ([]model.Share, error) {
	return s.shareRepo.ListShares(username)
}

// Revoke 撤销分享(校验归属)
func (s *ShareService) Revoke(ctx context.Context, token, username string) error {
	ok, err := s.shareRepo.DeleteShare(token, username)
	if err != nil {
		return err
	}
	if !ok {
		return ErrShareNotFound
	}
	slog.InfoContext(ctx, "share revoked", "token", token, "username", username)
	return nil
}

// Resolve 解析分享令牌,校验过期与提取码,返回文件元信息(用于免登录下载)
// 文件被软删除或已彻底删除时同样返回 ErrShareFileGone
func (s *ShareService) Resolve(ctx context.Context, token, password string) (model.FileMeta, error) {
	share, err := s.shareRepo.GetShareByToken(token)
	if err != nil {
		return model.FileMeta{}, ErrShareNotFound
	}
	if time.Now().After(share.ExpireAt) {
		return model.FileMeta{}, ErrShareNotFound // 过期视为不存在
	}
	if share.PasswordHash != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(share.PasswordHash), []byte(password)); err != nil {
			return model.FileMeta{}, ErrShareWrongPwd
		}
	}

	// 以分享者的身份校验所有权:软删除/彻底删除后 GetByHash 失败
	fMeta, err := s.fileRepo.GetByHash(share.FileSha1, share.UserName)
	if err != nil {
		return model.FileMeta{}, ErrShareFileGone
	}
	return fMeta, nil
}

// randomToken 生成 64 位 hex 分享令牌(crypto/rand,防枚举)
func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
