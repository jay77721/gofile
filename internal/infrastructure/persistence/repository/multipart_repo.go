package repository

import (
	"context"
	"fmt"
	"gofile/internal/domain"
	"sync"
	"time"

	"gorm.io/gorm"
)

// MultipartRepository 分片直传元数据仓储接口
type MultipartRepository interface {
	Create(ctx context.Context, mu model.MultipartUpload) error
	GetByUploadID(ctx context.Context, uploadID, username string) (model.MultipartUpload, error)
	UpdateStatus(ctx context.Context, uploadID, username string, status int) error
	ListExpired(ctx context.Context, before time.Time) ([]model.MultipartUpload, error)
	Delete(ctx context.Context, uploadID string) error
}

type mysqlMultipartRepo struct {
	db *gorm.DB
}

// NewMultipartRepository 创建 GORM 分片直传仓库
func NewMultipartRepository(db *gorm.DB) MultipartRepository {
	return &mysqlMultipartRepo{db: db}
}

func (r *mysqlMultipartRepo) Create(ctx context.Context, mu model.MultipartUpload) error {
	if err := r.db.WithContext(ctx).Create(&mu).Error; err != nil {
		return fmt.Errorf("create multipart upload record failed: %w", err)
	}
	return nil
}

func (r *mysqlMultipartRepo) GetByUploadID(ctx context.Context, uploadID, username string) (model.MultipartUpload, error) {
	var mu model.MultipartUpload
	db := r.db.WithContext(ctx).Where("upload_id = ?", uploadID)
	if username != "" {
		db = db.Where("user_name = ?", username)
	}
	if err := db.First(&mu).Error; err != nil {
		return model.MultipartUpload{}, fmt.Errorf("get multipart upload failed: %w", err)
	}
	return mu, nil
}

func (r *mysqlMultipartRepo) UpdateStatus(ctx context.Context, uploadID, username string, status int) error {
	db := r.db.WithContext(ctx).Model(&model.MultipartUpload{}).Where("upload_id = ?", uploadID)
	if username != "" {
		db = db.Where("user_name = ?", username)
	}
	if err := db.Update("status", status).Error; err != nil {
		return fmt.Errorf("update multipart status failed: %w", err)
	}
	return nil
}

func (r *mysqlMultipartRepo) ListExpired(ctx context.Context, before time.Time) ([]model.MultipartUpload, error) {
	var list []model.MultipartUpload
	if err := r.db.WithContext(ctx).Where("expired_at < ? AND status = ?", before, model.MultipartStatusUploading).
		Find(&list).Error; err != nil {
		return nil, fmt.Errorf("list expired multipart uploads failed: %w", err)
	}
	return list, nil
}

func (r *mysqlMultipartRepo) Delete(ctx context.Context, uploadID string) error {
	if err := r.db.WithContext(ctx).Where("upload_id = ?", uploadID).Delete(&model.MultipartUpload{}).Error; err != nil {
		return fmt.Errorf("delete multipart upload record failed: %w", err)
	}
	return nil
}

// MockMultipartRepository 内存 Mock 分片直传仓储
type MockMultipartRepository struct {
	mu      sync.RWMutex
	records map[string]model.MultipartUpload
}

// NewMockMultipartRepository 创建内存 Mock 分片仓储
func NewMockMultipartRepository() *MockMultipartRepository {
	return &MockMultipartRepository{
		records: make(map[string]model.MultipartUpload),
	}
}

func (m *MockMultipartRepository) Create(ctx context.Context, mu model.MultipartUpload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[mu.UploadID] = mu
	return nil
}

func (m *MockMultipartRepository) GetByUploadID(ctx context.Context, uploadID, username string) (model.MultipartUpload, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mu, ok := m.records[uploadID]
	if !ok {
		return model.MultipartUpload{}, fmt.Errorf("record not found")
	}
	if username != "" && mu.Username != username {
		return model.MultipartUpload{}, fmt.Errorf("record not found for user")
	}
	return mu, nil
}

func (m *MockMultipartRepository) UpdateStatus(ctx context.Context, uploadID, username string, status int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	mu, ok := m.records[uploadID]
	if !ok {
		return fmt.Errorf("record not found")
	}
	if username != "" && mu.Username != username {
		return fmt.Errorf("record not found for user")
	}
	mu.Status = status
	m.records[uploadID] = mu
	return nil
}

func (m *MockMultipartRepository) ListExpired(ctx context.Context, before time.Time) ([]model.MultipartUpload, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []model.MultipartUpload
	for _, mu := range m.records {
		if mu.ExpiredAt.Before(before) && mu.Status == model.MultipartStatusUploading {
			list = append(list, mu)
		}
	}
	return list, nil
}

func (m *MockMultipartRepository) Delete(ctx context.Context, uploadID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.records, uploadID)
	return nil
}
