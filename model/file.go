package model

import "time"

// FileMeta 文件元信息，与 handler JSON 响应兼容
type FileMeta struct {
	FileSha1 string    `json:"filehash"`
	FileName string    `json:"filename"`
	FileSize int64     `json:"size"`
	Location string    `json:"-"` // 存储路径，不暴露给前端
	UploadAt time.Time `json:"upload_time"`
	Username string    `json:"username"`
}