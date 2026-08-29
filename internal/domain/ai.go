package model

import "time"

// AITask AI 异步分析任务状态机
// 对应 tbl_ai_task 表
//
// 状态流转：
//
//	0 pending    → 待处理
//	1 processing → worker 正在处理
//	2 done       → 分析成功（幂等锚点：已分析则跳过）
//	3 failed     → 失败，retry_count < 上限时由补偿任务重新入队
type AITask struct {
	ID         uint      `gorm:"primaryKey;autoIncrement"`
	FileSha1   string    `gorm:"column:file_sha1;size:40;not null;uniqueIndex:uk_sha1_user"`
	Username   string    `gorm:"column:user_name;size:64;not null;uniqueIndex:uk_sha1_user"`
	Status     int       `gorm:"column:status;default:0;index"`
	RetryCount int       `gorm:"column:retry_count;default:0"`
	ErrorMsg   string    `gorm:"column:error_msg;size:512;default:''"`
	ExpiredAt  time.Time `gorm:"column:expired_at;index:idx_task_expired_at"`
	CreateAt   time.Time `gorm:"column:create_at;autoCreateTime"`
	UpdateAt   time.Time `gorm:"column:update_at;autoUpdateTime"`
}

func (AITask) TableName() string { return "tbl_ai_task" }

// AIConfig 用户级 AI Provider 配置(前端自定义 OpenAI 协议 baseURL/API key)
// 对应 tbl_ai_config 表
//
// 生效优先级:用户配置(本表)→ env 系统配置 → mock 降级
type AIConfig struct {
	Username   string    `gorm:"column:user_name;primaryKey;size:64"`
	BaseURL    string    `gorm:"column:base_url;size:512;default:''"`    // OpenAI 协议端点,空 = 使用系统默认
	APIKeyEnc  string    `gorm:"column:api_key_enc;size:512;default:''"` // API key AES-GCM 密文(base64),明文不下发
	Model      string    `gorm:"column:model;size:128;default:''"`       // 对话模型名,空 = 默认
	EmbedModel string    `gorm:"column:embed_model;size:128;default:''"` // embedding 模型名,空 = 默认
	UpdateAt   time.Time `gorm:"column:update_at;autoUpdateTime"`
}

func (AIConfig) TableName() string { return "tbl_ai_config" }

// AIConfigView 下发给前端的配置视图(API key 仅掩码展示)
type AIConfigView struct {
	Configured bool   `json:"configured"`   // 是否配置了用户级真实 provider
	BaseURL    string `json:"base_url"`     // 用户自定义端点,空 = 系统默认
	HasKey     bool   `json:"has_key"`      // 是否已保存 API key(前端决定是否显示掩码)
	APIKeyMask string `json:"api_key_mask"` // sk-****abcd,便于前端回显
	Model      string `json:"model"`        // 对话模型
	EmbedModel string `json:"embed_model"`  // embedding 模型
	Mode       string `json:"mode"`         // 当前生效模式:openai | mock
}
