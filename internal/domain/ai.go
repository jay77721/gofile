package model

import "time"

// AITask is the AI async analysis task state machine.
// Corresponds to tbl_ai_task.
//
// State transitions:
//
//	0 pending    -> pending
//	1 processing -> worker is processing
//	2 done       -> analyzed successfully (idempotency anchor: skip if already analyzed)
//	3 failed     -> failed, re-enqueued by compensation job when retry_count < limit
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

// AIConfig is per-user AI Provider config (frontend custom OpenAI-compatible baseURL/API key).
// Corresponds to tbl_ai_config.
//
// Effective priority: user config (this table) -> env system config -> mock fallback.
type AIConfig struct {
	Username   string    `gorm:"column:user_name;primaryKey;size:64"`
	BaseURL    string    `gorm:"column:base_url;size:512;default:''"`    // OpenAI-compatible endpoint, empty = use system default
	APIKeyEnc  string    `gorm:"column:api_key_enc;size:512;default:''"` // API key AES-GCM ciphertext (base64), plaintext never sent to client
	Model      string    `gorm:"column:model;size:128;default:''"`       // chat model name, empty = default
	EmbedModel string    `gorm:"column:embed_model;size:128;default:''"` // embedding model name, empty = default
	UpdateAt   time.Time `gorm:"column:update_at;autoUpdateTime"`
}

func (AIConfig) TableName() string { return "tbl_ai_config" }

// AIConfigView is the config view sent to frontend (API key displayed as masked only).
type AIConfigView struct {
	Configured bool   `json:"configured"`   // whether a real per-user provider is configured
	BaseURL    string `json:"base_url"`     // user custom endpoint, empty = system default
	HasKey     bool   `json:"has_key"`      // whether API key is saved (frontend decides whether to show mask)
	APIKeyMask string `json:"api_key_mask"` // sk-****abcd, for frontend echo
	Model      string `json:"model"`        // chat model
	EmbedModel string `json:"embed_model"`  // embedding model
	Mode       string `json:"mode"`         // current effective mode: openai | mock
}
