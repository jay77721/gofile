package model

import (
	"time"
)

const (
	MultipartStatusUploading = 1
	MultipartStatusCompleted = 2
	MultipartStatusAborted   = 3
)

// MultipartUpload is metadata for direct multipart upload tasks.
// Corresponds to tbl_multipart_upload.
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

// MultipartInitReq is the init direct multipart upload request DTO.
type MultipartInitReq struct {
	FileSha1  string `json:"filehash" binding:"required,len=40"`
	FileName  string `json:"filename" binding:"required"`
	FileSize  int64  `json:"filesize" binding:"required,gt=0"`
	ChunkSize int    `json:"chunk_size"` // optional, default 10MB
	ParentID  uint64 `json:"parent_id"`  // target parent directory ID
}

// MultipartInitResp is the init direct multipart upload response DTO.
type MultipartInitResp struct {
	FastUpload bool     `json:"fast_upload"`           // whether fast dedup hit
	UploadID   string   `json:"upload_id,omitempty"`   // MinIO/S3 UploadID
	ChunkSize  int      `json:"chunk_size,omitempty"`  // chunk size (bytes)
	ChunkCount int      `json:"chunk_count,omitempty"` // total chunk count
	PartURLs   []string `json:"part_urls,omitempty"`   // batch presigned PUT URLs (1-based maps to index 0..N-1)
}

// MultipartCompleteReq is the complete multipart request DTO.
type MultipartCompletePart struct {
	PartNumber int    `json:"part_number"`
	ETag       string `json:"etag"`
}

type MultipartCompleteReq struct {
	UploadID string                  `json:"upload_id" binding:"required"`
	Parts    []MultipartCompletePart `json:"parts" binding:"required"`
}

// MultipartAbortReq is the abort multipart request DTO.
type MultipartAbortReq struct {
	UploadID string `json:"upload_id" binding:"required"`
}
