package repository

import (
	"gofile/internal/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AIConfigRepository is the data access interface for user AI Provider configuration
type AIConfigRepository interface {
	// Get retrieves the user configuration, returns gorm.ErrRecordNotFound if not found
	Get(username string) (*model.AIConfig, error)
	// Upsert saves the user configuration (updates the entire row if exists, idempotent)
	Upsert(cfg *model.AIConfig) error
	// Delete removes the user configuration (fallback to env/mock)
	Delete(username string) error
}

type mysqlAIConfigRepo struct {
	db *gorm.DB
}

// NewAIConfigRepository creates a GORM user AI config repository
func NewAIConfigRepository(db *gorm.DB) AIConfigRepository {
	return &mysqlAIConfigRepo{db: db}
}

func (r *mysqlAIConfigRepo) Get(username string) (*model.AIConfig, error) {
	var cfg model.AIConfig
	if err := r.db.Where("user_name = ?", username).First(&cfg).Error; err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (r *mysqlAIConfigRepo) Upsert(cfg *model.AIConfig) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_name"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"base_url", "api_key_enc", "model", "embed_model", "update_at",
		}),
	}).Create(cfg).Error
}

func (r *mysqlAIConfigRepo) Delete(username string) error {
	return r.db.Where("user_name = ?", username).Delete(&model.AIConfig{}).Error
}

// ---- Mock implementation ----

// mockAIConfigRepo is an in-memory mock user AI config repository
type mockAIConfigRepo struct {
	cfgs map[string]model.AIConfig
}

// NewMockAIConfigRepository creates a mock user AI config repository
func NewMockAIConfigRepository() AIConfigRepository {
	return &mockAIConfigRepo{cfgs: make(map[string]model.AIConfig)}
}

func (m *mockAIConfigRepo) Get(username string) (*model.AIConfig, error) {
	cfg, ok := m.cfgs[username]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return &cfg, nil
}

func (m *mockAIConfigRepo) Upsert(cfg *model.AIConfig) error {
	m.cfgs[cfg.Username] = *cfg
	return nil
}

func (m *mockAIConfigRepo) Delete(username string) error {
	delete(m.cfgs, username)
	return nil
}

// Ensure interface implementation is checked at compile time
var _ AIConfigRepository = (*mysqlAIConfigRepo)(nil)
var _ AIConfigRepository = (*mockAIConfigRepo)(nil)
