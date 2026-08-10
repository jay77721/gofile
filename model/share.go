package model

import "time"

// Share 文件分享
// 对应 tbl_share 表
type Share struct {
	ID           uint      `gorm:"primaryKey;autoIncrement"`
	ShareToken   string    `gorm:"column:share_token;size:64;not null;uniqueIndex"`
	FileSha1     string    `gorm:"column:file_sha1;size:40;not null;index:idx_share_file"`
	UserName     string    `gorm:"column:user_name;size:64;not null"`                          // 分享者(所有权校验基准)
	PasswordHash string    `gorm:"column:password_hash;size:255;not null;default:''" json:"-"` // 提取码 bcrypt 哈希,空 = 无密码(不下发前端)
	ExpireAt     time.Time `gorm:"column:expire_at;not null;index:idx_share_expire"`
	CreateAt     time.Time `gorm:"column:create_at;autoCreateTime"`
}

func (Share) TableName() string { return "tbl_share" }
