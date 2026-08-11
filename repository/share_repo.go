package repository

import (
	"context"
	"fmt"
	"gofile/model"
	"sync"
	"time"

	"gorm.io/gorm"
)

// ShareRepository 文件分享数据访问接口
type ShareRepository interface {
	// CreateShare 创建分享
	CreateShare(ctx context.Context, s *model.Share) error
	// GetShareByToken 按令牌查询分享
	GetShareByToken(ctx context.Context, token string) (*model.Share, error)
	// ListShares 列出用户的分享
	ListShares(ctx context.Context, username string) ([]model.Share, error)
	// DeleteShare 撤销分享（校验归属）
	DeleteShare(ctx context.Context, token, username string) (bool, error)
	// DeleteExpired 清理过期分享
	DeleteExpired(ctx context.Context, before time.Time) error
}

// mysqlShareRepo GORM 实现的 ShareRepository
type mysqlShareRepo struct {
	db *gorm.DB
}

// NewShareRepository 创建 GORM 分享仓库
func NewShareRepository(db *gorm.DB) ShareRepository {
	return &mysqlShareRepo{db: db}
}

func (r *mysqlShareRepo) CreateShare(ctx context.Context, s *model.Share) error {
	if err := r.db.WithContext(ctx).Create(s).Error; err != nil {
		return fmt.Errorf("create share failed: %w", err)
	}
	return nil
}

func (r *mysqlShareRepo) GetShareByToken(ctx context.Context, token string) (*model.Share, error) {
	var s model.Share
	if err := r.db.WithContext(ctx).Where("share_token = ?", token).First(&s).Error; err != nil {
		return nil, fmt.Errorf("share not found: %w", err)
	}
	return &s, nil
}

func (r *mysqlShareRepo) ListShares(ctx context.Context, username string) ([]model.Share, error) {
	var shares []model.Share
	if err := r.db.WithContext(ctx).Where("user_name = ?", username).Order("create_at DESC").Find(&shares).Error; err != nil {
		return nil, fmt.Errorf("list shares failed: %w", err)
	}
	return shares, nil
}

func (r *mysqlShareRepo) DeleteShare(ctx context.Context, token, username string) (bool, error) {
	res := r.db.WithContext(ctx).Where("share_token = ? AND user_name = ?", token, username).Delete(&model.Share{})
	if res.Error != nil {
		return false, fmt.Errorf("delete share failed: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

func (r *mysqlShareRepo) DeleteExpired(ctx context.Context, before time.Time) error {
	if err := r.db.WithContext(ctx).Where("expire_at < ?", before).Delete(&model.Share{}).Error; err != nil {
		return fmt.Errorf("delete expired shares failed: %w", err)
	}
	return nil
}

// ---- Mock 实现 ----

// mockShareRepo 内存 mock 分享仓库
type mockShareRepo struct {
	mu     sync.Mutex
	shares map[string]*model.Share // key: share_token
}

// NewMockShareRepository 创建 mock 分享仓库
func NewMockShareRepository() ShareRepository {
	return &mockShareRepo{shares: make(map[string]*model.Share)}
}

func (m *mockShareRepo) CreateShare(ctx context.Context, s *model.Share) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	m.shares[s.ShareToken] = &cp
	return nil
}

func (m *mockShareRepo) GetShareByToken(ctx context.Context, token string) (*model.Share, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.shares[token]
	if !ok {
		return nil, fmt.Errorf("share not found")
	}
	cp := *s
	return &cp, nil
}

func (m *mockShareRepo) ListShares(ctx context.Context, username string) ([]model.Share, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []model.Share
	for _, s := range m.shares {
		if s.UserName == username {
			out = append(out, *s)
		}
	}
	return out, nil
}

func (m *mockShareRepo) DeleteShare(ctx context.Context, token, username string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.shares[token]
	if !ok || s.UserName != username {
		return false, nil
	}
	delete(m.shares, token)
	return true, nil
}

func (m *mockShareRepo) DeleteExpired(ctx context.Context, before time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, s := range m.shares {
		if s.ExpireAt.Before(before) {
			delete(m.shares, k)
		}
	}
	return nil
}

// 确保编译时检查接口实现
var _ ShareRepository = (*mysqlShareRepo)(nil)
var _ ShareRepository = (*mockShareRepo)(nil)
