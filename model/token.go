package model

import "time"

// Token 用户会话 token，对应 tbl_user_token 表
type Token struct {
	Username  string    `gorm:"column:user_name;primaryKey;size:64"`
	Token     string    `gorm:"column:user_token;size:64;not null;default:''"`
	UpdateAt  time.Time `gorm:"column:update_at;autoCreateTime;autoUpdateTime"`
	ExpiredAt time.Time `gorm:"column:expired_at;index"`
}

func (Token) TableName() string { return "tbl_user_token" }