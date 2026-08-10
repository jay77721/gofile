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
