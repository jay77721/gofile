package model

import "time"

// User 用户信息
type User struct {
	Username     string    `json:"Username"`
	Email        string    `json:"Email"`
	Phone        string    `json:"Phone"`
	SignupAt     time.Time `json:"SignupAt"`
	LastActiveAt time.Time `json:"LastActiveAt"`
	Status       int       `json:"Status"`
}