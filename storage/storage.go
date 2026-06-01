package storage

import (
	"context"
	"io"
)

// Storage 文件存储接口，支持本地文件系统和 MinIO 对象存储
type Storage interface {
	// Put 将文件写入存储
	Put(ctx context.Context, key string, reader io.Reader, size int64) error
	// Get 从存储读取文件
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Exists 检查文件是否存在
	Exists(ctx context.Context, key string) (bool, error)
	// Delete 删除文件
	Delete(ctx context.Context, key string) error
}
