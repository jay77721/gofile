package model

import "time"

// User is user information, corresponding to tbl_user.
type User struct {
	Username     string    `gorm:"column:user_name;primaryKey;size:64"`
	Password     string    `gorm:"column:user_pwd;size:60;not null;default:''" json:"-"`
	SignupAt     time.Time `gorm:"column:signup_at;autoCreateTime"`
	LastActiveAt time.Time `gorm:"column:last_active_at;default:null"`
	Status       int       `gorm:"column:status;default:0"`
}

func (User) TableName() string { return "tbl_user" }
