package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gofile/internal/port"
)

// MinIOStorage is a MinIO object storage implementation (S3-compatible)
type MinIOStorage struct {
	client *minio.Client
	core   *minio.Core
	bucket string
}

// NewMinIO creates a MinIO storage instance, automatically creating the bucket if it does not exist
func NewMinIO(endpoint, accessKey, secretKey, bucket string, useSSL bool) (*MinIOStorage, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client failed: %w", err)
	}

	// Automatically create bucket
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

	core := &minio.Core{Client: client}
	slog.Info("minio connected", "endpoint", endpoint, "bucket", bucket)
	return &MinIOStorage{client: client, core: core, bucket: bucket}, nil
}

// Put uploads a file to MinIO
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

// Get reads a file from MinIO
func (s *MinIOStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get object failed: %w", err)
	}
	return obj, nil
}

// GetRange reads a MinIO file by byte range (supports HTTP Range downloads)
func (s *MinIOStorage) GetRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	opts := minio.GetObjectOptions{}
	opts.SetRange(offset, offset+length-1)
	obj, err := s.client.GetObject(ctx, s.bucket, key, opts)
	if err != nil {
		return nil, fmt.Errorf("get range object failed: %w", err)
	}
	return obj, nil
}

// FileSize retrieves the MinIO file size
func (s *MinIOStorage) FileSize(ctx context.Context, key string) (int64, error) {
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return 0, fmt.Errorf("stat object failed: %w", err)
	}
	return info.Size, nil
}

// Exists checks whether a file exists in MinIO
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

// Delete deletes a file from MinIO
func (s *MinIOStorage) Delete(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("remove object failed: %w", err)
	}
	return nil
}

// PresignPut issues a presigned upload URL (frontend PUTs directly to MinIO, bypassing the app server)
func (s *MinIOStorage) PresignPut(ctx context.Context, key string, expiry time.Duration) (string, error) {
	url, err := s.client.PresignedPutObject(ctx, s.bucket, key, expiry)
	if err != nil {
		return "", fmt.Errorf("presign put failed: %w", err)
	}
	return url.String(), nil
}

// PresignGet issues a presigned download URL (frontend downloads directly from MinIO, bypassing the app server)
func (s *MinIOStorage) PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	url, err := s.client.PresignedGetObject(ctx, s.bucket, key, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("presign get failed: %w", err)
	}
	return url.String(), nil
}

// InitMultipart initializes S3 multipart upload and returns UploadID
func (s *MinIOStorage) InitMultipart(ctx context.Context, key string) (string, error) {
	uploadID, err := s.core.NewMultipartUpload(ctx, s.bucket, key, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return "", fmt.Errorf("minio new multipart failed: %w", err)
	}
	return uploadID, nil
}

// PresignPartPut issues a presigned URL for a specific multipart chunk
func (s *MinIOStorage) PresignPartPut(ctx context.Context, key, uploadID string, partNumber int, expiry time.Duration) (string, error) {
	reqParams := make(url.Values)
	reqParams.Set("uploadId", uploadID)
	reqParams.Set("partNumber", fmt.Sprintf("%d", partNumber))

	u, err := s.client.Presign(ctx, "PUT", s.bucket, key, expiry, reqParams)
	if err != nil {
		return "", fmt.Errorf("minio presign part put failed: %w", err)
	}
	return u.String(), nil
}

// CompleteMultipart merges multipart chunks at the storage layer
func (s *MinIOStorage) CompleteMultipart(ctx context.Context, key, uploadID string, parts []port.CompletePart) error {
	var minioParts []minio.CompletePart
	for _, p := range parts {
		minioParts = append(minioParts, minio.CompletePart{
			PartNumber: p.PartNumber,
			ETag:       p.ETag,
		})
	}
	_, err := s.core.CompleteMultipartUpload(ctx, s.bucket, key, uploadID, minioParts, minio.PutObjectOptions{})
	if err != nil {
		return fmt.Errorf("minio complete multipart failed: %w", err)
	}
	return nil
}

// AbortMultipart aborts a multipart upload and cleans up temporary storage chunks
func (s *MinIOStorage) AbortMultipart(ctx context.Context, key, uploadID string) error {
	if err := s.core.AbortMultipartUpload(ctx, s.bucket, key, uploadID); err != nil {
		return fmt.Errorf("minio abort multipart failed: %w", err)
	}
	return nil
}
