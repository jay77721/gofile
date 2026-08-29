package model

import "time"

// Share is a file share.
// Corresponds to tbl_share.
type Share struct {
	ID           uint      `gorm:"primaryKey;autoIncrement"`
	ShareToken   string    `gorm:"column:share_token;size:64;not null;uniqueIndex"`
	FileSha1     string    `gorm:"column:file_sha1;size:40;not null;index:idx_share_file"`
	UserName     string    `gorm:"column:user_name;size:64;not null"`                          // sharer (ownership check baseline)
	PasswordHash string    `gorm:"column:password_hash;size:255;not null;default:''" json:"-"` // bcrypt hash of access code, empty = no password (never sent to frontend)
	HasPassword  bool      `gorm:"-" json:"has_password"`                                      // whether access code exists (for serialization, not a DB column)
	ExpireAt     time.Time `gorm:"column:expire_at;not null;index:idx_share_expire"`
	CreateAt     time.Time `gorm:"column:create_at;autoCreateTime"`
}

func (Share) TableName() string { return "tbl_share" }
