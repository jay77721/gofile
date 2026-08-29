package repository

import (
	"gofile/internal/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AIConfigRepository 用户 AI Provider 配置的数据访问接口
type AIConfigRepository interface {
	// Get 获取用户配置,不存在返回 gorm.ErrRecordNotFound
	Get(username string) (*model.AIConfig, error)
	// Upsert 保存用户配置(存在则整行更新,幂等)
	Upsert(cfg *model.AIConfig) error
	// Delete 删除用户配置(回退 env/mock)
	Delete(username string) error
}

type mysqlAIConfigRepo struct {
	db *gorm.DB
}

// NewAIConfigRepository 创建 GORM 用户 AI 配置仓库
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

// ---- Mock 实现 ----

// mockAIConfigRepo 内存 mock 用户 AI 配置仓库
type mockAIConfigRepo struct {
	cfgs map[string]model.AIConfig
}

// NewMockAIConfigRepository 创建 mock 用户 AI 配置仓库
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

// 确保编译时检查接口实现
var _ AIConfigRepository = (*mysqlAIConfigRepo)(nil)
var _ AIConfigRepository = (*mockAIConfigRepo)(nil)
