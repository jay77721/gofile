package storage

import (
	"context"
	"fmt"
	"io"
	"time"
)

// ErrPresignNotSupported 本地存储不支持预签名 URL
var ErrPresignNotSupported = fmt.Errorf("presigned URL not supported for local storage")

// Storage 文件存储接口，支持本地文件系统和 MinIO 对象存储
type Storage interface {
	// Put 将文件写入存储
	Put(ctx context.Context, key string, reader io.Reader, size int64) error
	// Get 从存储读取文件
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// GetRange 按字节区间读取文件（支持 HTTP Range 下载）
	GetRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error)
	// FileSize 获取文件大小（字节数）
	FileSize(ctx context.Context, key string) (int64, error)
	// Exists 检查文件是否存在
	Exists(ctx context.Context, key string) (bool, error)
	// Delete 删除文件
	Delete(ctx context.Context, key string) error

	// PresignPut 签发预签名上传 URL（仅 S3/MinIO 实现，本地存储返回 ErrPresignNotSupported）
	PresignPut(ctx context.Context, key string, expiry time.Duration) (string, error)
	// PresignGet 签发预签名下载 URL
	PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error)
}