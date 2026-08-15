package model

import (
	"gofile/storage"
	"time"
)

const (
	MultipartStatusUploading = 1
	MultipartStatusCompleted = 2
	MultipartStatusAborted   = 3
)

// MultipartUpload 分片直传任务元数据
// 对应 tbl_multipart_upload 表
type MultipartUpload struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement"`
	UploadID   string    `gorm:"column:upload_id;size:128;not null;uniqueIndex:uk_upload_id"`
	FileSha1   string    `gorm:"column:file_sha1;size:40;not null;index:idx_user_sha1"`
	FileName   string    `gorm:"column:file_name;size:256;not null"`
	FileSize   int64     `gorm:"column:file_size;not null"`
	ChunkSize  int       `gorm:"column:chunk_size;not null"`
	ChunkCount int       `gorm:"column:chunk_count;not null"`
	Username   string    `gorm:"column:user_name;size:64;not null;index:idx_user_sha1"`
	ParentID   uint64    `gorm:"column:parent_id;default:0;not null"`
	Status     int       `gorm:"column:status;default:1;not null"`
	CreateAt   time.Time `gorm:"column:create_at;autoCreateTime"`
	ExpiredAt  time.Time `gorm:"column:expired_at;not null;index:idx_expired_at"`
}

func (MultipartUpload) TableName() string { return "tbl_multipart_upload" }

// MultipartInitReq 初始化分片直传请求 DTO
type MultipartInitReq struct {
	FileSha1  string `json:"filehash" binding:"required,len=40"`
	FileName  string `json:"filename" binding:"required"`
	FileSize  int64  `json:"filesize" binding:"required,gt=0"`
	ChunkSize int    `json:"chunk_size"` // 可选，默认 10MB
	ParentID  uint64 `json:"parent_id"`  // 目标父目录 ID
}

// MultipartInitResp 初始化分片直传响应 DTO
type MultipartInitResp struct {
	FastUpload bool     `json:"fast_upload"`           // 是否命中秒传
	UploadID   string   `json:"upload_id,omitempty"`   // MinIO/S3 UploadID
	ChunkSize  int      `json:"chunk_size,omitempty"`  // 分片大小 (bytes)
	ChunkCount int      `json:"chunk_count,omitempty"` // 总分片数
	PartURLs   []string `json:"part_urls,omitempty"`   // 批量预签名 PUT URL（1-based 对应下标 0..N-1）
}

// MultipartCompleteReq 合并分片请求 DTO
type MultipartCompleteReq struct {
	UploadID string                 `json:"upload_id" binding:"required"`
	Parts    []storage.CompletePart `json:"parts" binding:"required"`
}

// MultipartAbortReq 取消分片请求 DTO
type MultipartAbortReq struct {
	UploadID string `json:"upload_id" binding:"required"`
}
