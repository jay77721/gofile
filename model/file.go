package model

import "time"

// File 全局文件注册表，按 SHA1 去重
// 对应 tbl_file 表
type File struct {
	FileSha1 string    `gorm:"column:file_sha1;primaryKey;size:40"`
	FileSize int64     `gorm:"column:file_size;default:0"`
	FileAddr string    `gorm:"column:file_addr;size:512;default:''"`
	Summary  string    `gorm:"column:file_summary;type:text"`   // AI 生成的内容摘要
	Tags     string    `gorm:"column:tags;size:255;default:''"` // AI 生成的标签，逗号分隔
	CreateAt time.Time `gorm:"column:create_at;autoCreateTime"`
}

func (File) TableName() string { return "tbl_file" }

// UserFile 用户拥有关系，每个用户一行
// 对应 tbl_user_file 表
type UserFile struct {
	ID       uint      `gorm:"primaryKey;autoIncrement"`
	Username string    `gorm:"column:user_name;size:64;not null;uniqueIndex:idx_user_file"`
	FileSha1 string    `gorm:"column:file_sha1;size:40;not null;uniqueIndex:idx_user_file"`
	FileName string    `gorm:"column:file_name;size:256;default:''"`
	CreateAt time.Time `gorm:"column:create_at;autoCreateTime"`
	Status   int       `gorm:"column:status;default:1;index"`
}

func (UserFile) TableName() string { return "tbl_user_file" }

// FileMeta 文件元信息 DTO，用于 handler 返回 JSON
type FileMeta struct {
	FileSha1 string `json:"filehash"`
	FileName string `json:"filename"`
	FileSize int64  `json:"size"`
	Username string `json:"username"`
	UploadAt string `json:"upload_time"` // 字符串格式，方便前端直接展示
	Summary  string `json:"summary"`     // AI 生成摘要，未分析时为空
	Tags     string `json:"tags"`        // AI 生成标签，逗号分隔
}