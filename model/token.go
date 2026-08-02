package model

import "time"

// Token 用户会话 token
type Token struct {
	Username  string
	Token     string
	ExpiredAt time.Time
}