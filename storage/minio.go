package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinIOStorage MinIO 对象存储实现（S3 兼容）
type MinIOStorage struct {
	client *minio.Client
	bucket string
}

// NewMinIO 创建 MinIO 存储实例，自动创建 bucket（如不存在）
func NewMinIO(endpoint, accessKey, secretKey, bucket string, useSSL bool) (*MinIOStorage, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client failed: %w", err)
	}

	// 自动创建 bucket
	ctx := context.Background()
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket failed: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("create bucket failed: %w", err)
		}
		slog.Info("minio bucket created", "bucket", bucket)
	}

	slog.Info("minio connected", "endpoint", endpoint, "bucket", bucket)
	return &MinIOStorage{client: client, bucket: bucket}, nil
}

// Put 将文件上传到 MinIO
func (s *MinIOStorage) Put(ctx context.Context, key string, reader io.Reader, size int64) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, reader, size, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return fmt.Errorf("put object failed: %w", err)
	}
	slog.Info("file stored in minio", "key", key, "size", size)
	return nil
}

// Get 从 MinIO 读取文件
func (s *MinIOStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get object failed: %w", err)
	}
	return obj, nil
}

// Exists 检查文件是否存在于 MinIO
func (s *MinIOStorage) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" {
			return false, nil
		}
		return false, fmt.Errorf("stat object failed: %w", err)
	}
	return true, nil
}

// Delete 从 MinIO 删除文件
func (s *MinIOStorage) Delete(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("remove object failed: %w", err)
	}
	return nil
}

// PresignPut 签发预签名上传 URL（前端直接 PUT 文件到 MinIO，不经过应用服务器）
func (s *MinIOStorage) PresignPut(ctx context.Context, key string, expiry time.Duration) (string, error) {
	url, err := s.client.PresignedPutObject(ctx, s.bucket, key, expiry)
	if err != nil {
		return "", fmt.Errorf("presign put failed: %w", err)
	}
	return url.String(), nil
}

// PresignGet 签发预签名下载 URL（前端直接从 MinIO 下载，不经过应用服务器）
func (s *MinIOStorage) PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	url, err := s.client.PresignedGetObject(ctx, s.bucket, key, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("presign get failed: %w", err)
	}
	return url.String(), nil
}
