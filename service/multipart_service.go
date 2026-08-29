package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"gofile/metrics"
	"gofile/model"
	"gofile/storage"
	"gofile/util"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// UploadChunk 上传分片（用户隔离）
func (s *FileService) UploadChunk(ctx context.Context, fileHash string, index int, file io.Reader, username string) error {
	if index < 0 {
		return fmt.Errorf("invalid chunk index: %d", index)
	}
	// 已上传过的分片直接返回
	if util.ChunkExists(s.cfg.ChunkDir, username, fileHash, index) {
		return nil
	}

	dir := filepath.Join(s.cfg.ChunkDir, filepath.Base(username), filepath.Base(fileHash))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create chunk directory failed: %w", err)
	}

	chunkPath := filepath.Join(dir, strconv.Itoa(index))
	tmpPath := chunkPath + ".tmp-" + strings.ReplaceAll(uuid.New().String(), "-", "")
	dst, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create chunk failed: %w", err)
	}
	if _, err := io.Copy(dst, file); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write chunk failed: %w", err)
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close chunk failed: %w", err)
	}
	if err := os.Link(tmpPath, chunkPath); err != nil {
		_ = os.Remove(tmpPath)
		if _, statErr := os.Stat(chunkPath); statErr == nil {
			return nil
		}
		return fmt.Errorf("publish chunk failed: %w", err)
	}

	slog.InfoContext(ctx, "chunk uploaded", "filehash", fileHash, "index", index, "username", username)
	return nil
}

// GetChunkStatus 获取已上传的分片索引列表（用户隔离）
func (s *FileService) GetChunkStatus(fileHash, username string) ([]string, error) {
	chunks, err := util.GetUploadedChunks(s.cfg.ChunkDir, username, fileHash)
	if err != nil {
		return nil, err
	}

	sort.Slice(chunks, func(i, j int) bool {
		ii, _ := strconv.Atoi(chunks[i])
		jj, _ := strconv.Atoi(chunks[j])
		return ii < jj
	})

	return chunks, nil
}

// MergeChunks 合并分片（用户隔离，UUID 临时文件防冲突）
func (s *FileService) MergeChunks(ctx context.Context, fileHash, fileName, username, totalStr string) (model.FileMeta, error) {
	// 获取分布式锁（防止同一 hash 并发 merge）
	if s.cache != nil {
		lockToken := uuid.New().String()
		locked, lockErr := s.cache.AcquireLock(ctx, "gofile:lock:merge:"+fileHash, lockToken, 2*time.Minute)
		if lockErr != nil {
			return model.FileMeta{}, fmt.Errorf("acquire merge lock failed: %w", lockErr)
		}
		if !locked {
			return model.FileMeta{}, fmt.Errorf("merge in progress, please try again later")
		}
		defer func() {
			if err := s.cache.ReleaseLock(ctx, "gofile:lock:merge:"+fileHash, lockToken); err != nil {
				slog.WarnContext(ctx, "release merge lock failed", "filehash", fileHash, "error", err)
			}
		}()
	}

	chunkDir := filepath.Join(s.cfg.ChunkDir, filepath.Base(username), filepath.Base(fileHash))

	files, err := readChunkDir(chunkDir, totalStr)
	if err != nil {
		return model.FileMeta{}, err
	}

	// 使用 UUID 防止并发 merge 临时文件冲突
	uuidSuffix := strings.ReplaceAll(uuid.New().String(), "-", "")
	tmpPath := filepath.Join(s.cfg.ChunkDir, fileHash+"."+uuidSuffix+".tmp")
	totalSize, actualHash, err := mergeChunksToTemp(chunkDir, files, tmpPath)
	if err != nil {
		return model.FileMeta{}, err
	}
	if actualHash != strings.ToLower(fileHash) {
		_ = os.Remove(tmpPath)
		return model.FileMeta{}, fmt.Errorf("%w: expected %s, got %s", ErrFileFingerprintMismatch, fileHash, actualHash)
	}

	// 上传到存储层
	if err := saveMergedFile(ctx, s.store, fileHash, tmpPath, totalSize); err != nil {
		slog.ErrorContext(ctx, "store merged file failed", "error", err)
		return model.FileMeta{}, fmt.Errorf("store merged file failed: %w", err)
	}

	// 注册全局文件（幂等）
	if err := s.fileRepo.Create(ctx, model.File{FileSha1: fileHash, FileSize: totalSize, FileAddr: fileHash}); err != nil {
		slog.WarnContext(ctx, "save global file meta failed, rolling back", "filehash", fileHash)
		return model.FileMeta{}, errors.Join(
			fmt.Errorf("save file meta failed: %w", err),
			s.deleteStoredFile(ctx, fileHash, false),
			os.RemoveAll(chunkDir),
		)
	}

	// 建立用户拥有关系
	if err := s.fileRepo.CreateUserFile(ctx, model.UserFile{Username: username, FileSha1: fileHash, FileName: fileName, Status: model.UserFileStatusActive}); err != nil {
		slog.WarnContext(ctx, "save user file relation failed", "error", err, "filehash", fileHash)
		return model.FileMeta{}, errors.Join(
			fmt.Errorf("save user file relation failed: %w", err),
			s.deleteStoredFile(ctx, fileHash, true),
			os.RemoveAll(chunkDir),
		)
	}
	if err := os.RemoveAll(chunkDir); err != nil {
		slog.WarnContext(ctx, "remove merged chunk directory failed", "path", chunkDir, "error", err)
	}

	// 记入 Redis 缓存
	s.cacheMark(ctx, fileHash)

	// 触发 AI 异步分析
	s.enqueue(ctx, fileHash, fileName, username)

	// 业务指标：分片合并成功也计入上传字节
	metrics.AddUploadBytes(totalSize)
	slog.InfoContext(ctx, "chunks merged", "filehash", fileHash, "filename", fileName, "size", totalSize, "username", username)
	return model.FileMeta{FileSha1: fileHash, FileName: fileName, FileSize: totalSize, Username: username}, nil
}

// readChunkDir 读取并排序 chunk 文件，校验分块数量
func readChunkDir(chunkDir, totalStr string) ([]os.DirEntry, error) {
	files, err := os.ReadDir(chunkDir)
	if err != nil {
		return nil, fmt.Errorf("read chunk directory failed: %w", err)
	}
	validFiles := make([]os.DirEntry, 0, len(files))
	for _, file := range files {
		if file.Type().IsRegular() {
			if _, parseErr := strconv.Atoi(file.Name()); parseErr == nil {
				validFiles = append(validFiles, file)
			}
		}
	}
	files = validFiles
	if len(files) == 0 {
		return nil, fmt.Errorf("chunks not found")
	}
	total, err := strconv.Atoi(totalStr)
	if err != nil || total <= 0 {
		return nil, fmt.Errorf("invalid chunk count: %q", totalStr)
	}
	if len(files) != total {
		return nil, fmt.Errorf("chunk count mismatch: expected %d, got %d", total, len(files))
	}

	sort.Slice(files, func(i, j int) bool {
		iIndex, _ := strconv.Atoi(files[i].Name())
		jIndex, _ := strconv.Atoi(files[j].Name())
		return iIndex < jIndex
	})
	for i, f := range files {
		index, err := strconv.Atoi(f.Name())
		if err != nil || index != i || !f.Type().IsRegular() {
			return nil, fmt.Errorf("invalid or non-contiguous chunk index: %q", f.Name())
		}
	}

	return files, nil
}

// mergeChunksToTemp 将分片合并到临时文件
func mergeChunksToTemp(chunkDir string, files []os.DirEntry, tmpPath string) (int64, string, error) {
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return 0, "", fmt.Errorf("create temp file failed: %w", err)
	}
	defer tmpFile.Close()

	h := sha1.New()
	writer := io.MultiWriter(tmpFile, h)
	var totalSize int64
	for _, f := range files {
		chunkPath := filepath.Join(chunkDir, f.Name())
		chunkFile, err := os.Open(chunkPath)
		if err != nil {
			os.Remove(tmpPath)
			return 0, "", fmt.Errorf("open chunk failed: %w", err)
		}

		written, err := io.Copy(writer, chunkFile)
		chunkFile.Close()
		if err != nil {
			os.Remove(tmpPath)
			return 0, "", fmt.Errorf("merge chunk failed: %w", err)
		}
		totalSize += written
	}

	return totalSize, hex.EncodeToString(h.Sum(nil)), nil
}

// saveMergedFile 将合并后的临时文件上传到存储层
func saveMergedFile(ctx context.Context, store storage.Storage, fileHash, tmpPath string, totalSize int64) error {
	tmpReader, err := os.Open(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("open merged temp file failed")
	}
	defer tmpReader.Close()
	defer os.Remove(tmpPath)

	return store.Put(ctx, fileHash, tmpReader, totalSize)
}

// ---- S3 Multipart 直传直合 ----

// InitMultipartUpload 初始化分片直传，包含秒传判定与批量预签名 URL 签发
func (s *FileService) InitMultipartUpload(ctx context.Context, username string, req model.MultipartInitReq) (model.MultipartInitResp, error) {
	if s.multipartRepo == nil {
		return model.MultipartInitResp{}, fmt.Errorf("multipart repository not configured")
	}
	if err := validateFileHash(req.FileSha1); err != nil {
		return model.MultipartInitResp{}, err
	}
	if req.FileSize <= 0 {
		return model.MultipartInitResp{}, fmt.Errorf("invalid file size: %d", req.FileSize)
	}
	if req.FileName == "" {
		return model.MultipartInitResp{}, fmt.Errorf("file name is required")
	}

	// 默认分块大小 10MB，最小 5MB（除最后一个分块外）
	chunkSize := req.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 10 * 1024 * 1024
	}
	if chunkSize < 5*1024*1024 {
		chunkSize = 5 * 1024 * 1024
	}

	// 计算目标物化路径
	targetDirPath := "/"
	if req.ParentID != 0 {
		parent, err := s.fileRepo.GetUserFileByID(ctx, uint(req.ParentID), username)
		if err != nil || parent.IsDir != 1 || parent.Status != model.UserFileStatusActive {
			return model.MultipartInitResp{}, fmt.Errorf("invalid target directory")
		}
		targetDirPath = parent.DirPath
	}

	// 1. 秒传判定：Redis 缓存 O(1) 过滤 + 存储层确认
	var isFastUpload bool
	if s.cache != nil {
		seen, cacheErr := s.cache.HashExists(ctx, req.FileSha1)
		if cacheErr != nil {
			slog.WarnContext(ctx, "cache hash lookup failed", "hash", req.FileSha1, "error", cacheErr)
		}
		if seen {
			exists, err := s.store.Exists(ctx, req.FileSha1)
			if err != nil {
				return model.MultipartInitResp{}, fmt.Errorf("check existing file failed: %w", err)
			}
			if exists {
				if err := s.validateStoredSize(ctx, req.FileSha1, req.FileSize); err != nil {
					return model.MultipartInitResp{}, err
				}
				isFastUpload = true
			}
		}
	}
	if !isFastUpload {
		exists, err := s.store.Exists(ctx, req.FileSha1)
		if err != nil {
			return model.MultipartInitResp{}, fmt.Errorf("check existing file failed: %w", err)
		}
		if exists {
			if err := s.validateStoredSize(ctx, req.FileSha1, req.FileSize); err != nil {
				return model.MultipartInitResp{}, err
			}
			isFastUpload = true
		}
	}

	if isFastUpload {
		// 秒传命中：直接绑定用户关系并触发 AI 任务
		if err := s.fileRepo.CreateUserFile(ctx, model.UserFile{
			Username: username,
			ParentID: req.ParentID,
			FileSha1: req.FileSha1,
			FileName: req.FileName,
			DirPath:  targetDirPath,
			Status:   model.UserFileStatusActive,
		}); err != nil {
			return model.MultipartInitResp{}, fmt.Errorf("save user file relation failed: %w", err)
		}
		s.cacheMark(ctx, req.FileSha1)
		s.enqueue(ctx, req.FileSha1, req.FileName, username)
		slog.InfoContext(ctx, "multipart init: fast upload hit", "filehash", req.FileSha1, "username", username)
		return model.MultipartInitResp{FastUpload: true}, nil
	}

	// 2. 未命中秒传：调用 S3 初始化 Multipart Upload
	uploadID, err := s.store.InitMultipart(ctx, req.FileSha1)
	if err != nil {
		return model.MultipartInitResp{}, fmt.Errorf("init s3 multipart failed: %w", err)
	}

	// 计算分片总数
	chunkCount := int((req.FileSize + int64(chunkSize) - 1) / int64(chunkSize))
	if chunkCount <= 0 || chunkCount > 10000 {
		return model.MultipartInitResp{}, errors.Join(fmt.Errorf("invalid chunk count: %d", chunkCount), s.store.AbortMultipart(ctx, req.FileSha1, uploadID))
	}
	partURLs := make([]string, chunkCount)
	for i := 1; i <= chunkCount; i++ {
		u, err := s.store.PresignPartPut(ctx, req.FileSha1, uploadID, i, 24*time.Hour)
		if err != nil {
			return model.MultipartInitResp{}, errors.Join(fmt.Errorf("presign part url failed: %w", err), s.store.AbortMultipart(ctx, req.FileSha1, uploadID))
		}
		partURLs[i-1] = u
	}

	// 持久化分片任务记录
	record := model.MultipartUpload{
		UploadID:   uploadID,
		FileSha1:   req.FileSha1,
		FileName:   req.FileName,
		FileSize:   req.FileSize,
		ChunkSize:  chunkSize,
		ChunkCount: chunkCount,
		Username:   username,
		ParentID:   req.ParentID,
		Status:     model.MultipartStatusUploading,
		ExpiredAt:  time.Now().Add(24 * time.Hour),
	}
	if err := s.multipartRepo.Create(ctx, record); err != nil {
		return model.MultipartInitResp{}, errors.Join(fmt.Errorf("save multipart record failed: %w", err), s.store.AbortMultipart(ctx, req.FileSha1, uploadID))
	}

	slog.InfoContext(ctx, "multipart init: s3 session created", "upload_id", uploadID, "chunks", chunkCount, "filehash", req.FileSha1)
	return model.MultipartInitResp{
		FastUpload: false,
		UploadID:   uploadID,
		ChunkSize:  chunkSize,
		ChunkCount: chunkCount,
		PartURLs:   partURLs,
	}, nil
}

// CompleteMultipartUpload 完成分片上传并在存储层原子合并
func validateCompleteParts(parts []storage.CompletePart, expected int) error {
	if expected <= 0 || len(parts) != expected {
		return fmt.Errorf("chunk count mismatch: expected %d, got %d", expected, len(parts))
	}
	for i, part := range parts {
		if part.PartNumber != i+1 {
			return fmt.Errorf("invalid part number at position %d: expected %d, got %d", i, i+1, part.PartNumber)
		}
		if strings.TrimSpace(part.ETag) == "" {
			return fmt.Errorf("missing etag for part %d", part.PartNumber)
		}
	}
	return nil
}

func (s *FileService) CompleteMultipartUpload(ctx context.Context, username string, req model.MultipartCompleteReq) (model.FileMeta, error) {
	if s.multipartRepo == nil {
		return model.FileMeta{}, fmt.Errorf("multipart repository not configured")
	}

	mu, err := s.multipartRepo.GetByUploadID(ctx, req.UploadID, username)
	if err != nil {
		return model.FileMeta{}, fmt.Errorf("multipart upload session not found: %w", err)
	}
	if mu.Status != model.MultipartStatusUploading {
		return model.FileMeta{}, fmt.Errorf("upload session is not active")
	}
	if err := validateCompleteParts(req.Parts, mu.ChunkCount); err != nil {
		return model.FileMeta{}, err
	}

	// 1. 在存储层完成分片合并（零后端带宽与磁盘 I/O）
	if err := s.store.CompleteMultipart(ctx, mu.FileSha1, mu.UploadID, req.Parts); err != nil {
		slog.ErrorContext(ctx, "s3 complete multipart failed", "upload_id", mu.UploadID, "error", err)
		abortErr := s.store.AbortMultipart(ctx, mu.FileSha1, mu.UploadID)
		return model.FileMeta{}, errors.Join(fmt.Errorf("s3 complete multipart failed: %w", err), abortErr)
	}
	actualSize, actualHash, err := s.inspectStoredFile(ctx, mu.FileSha1)
	if err != nil {
		return model.FileMeta{}, errors.Join(
			fmt.Errorf("verify completed multipart file failed: %w", err),
			s.deleteStoredFile(ctx, mu.FileSha1, false),
			s.multipartRepo.UpdateStatus(ctx, mu.UploadID, username, model.MultipartStatusAborted),
		)
	}
	if actualSize != mu.FileSize {
		return model.FileMeta{}, errors.Join(
			fmt.Errorf("%w: expected %d, got %d", ErrFileSizeMismatch, mu.FileSize, actualSize),
			s.deleteStoredFile(ctx, mu.FileSha1, false),
			s.multipartRepo.UpdateStatus(ctx, mu.UploadID, username, model.MultipartStatusAborted),
		)
	}
	if actualHash != strings.ToLower(mu.FileSha1) {
		return model.FileMeta{}, errors.Join(
			fmt.Errorf("%w: expected %s, got %s", ErrFileFingerprintMismatch, mu.FileSha1, actualHash),
			s.deleteStoredFile(ctx, mu.FileSha1, false),
			s.multipartRepo.UpdateStatus(ctx, mu.UploadID, username, model.MultipartStatusAborted),
		)
	}

	// 2. 注册全局文件
	if err := s.fileRepo.Create(ctx, model.File{
		FileSha1: mu.FileSha1,
		FileName: mu.FileName,
		FileSize: mu.FileSize,
		FileAddr: mu.FileSha1,
	}); err != nil {
		slog.WarnContext(ctx, "save global file meta failed", "filehash", mu.FileSha1, "error", err)
		return model.FileMeta{}, errors.Join(fmt.Errorf("save file meta failed: %w", err), s.deleteStoredFile(ctx, mu.FileSha1, false))
	}

	// 计算物化路径
	targetDirPath := "/"
	if mu.ParentID != 0 {
		if parent, err := s.fileRepo.GetUserFileByID(ctx, uint(mu.ParentID), username); err == nil {
			targetDirPath = parent.DirPath
		}
	}

	// 3. 建立用户关联关系
	if err := s.fileRepo.CreateUserFile(ctx, model.UserFile{
		Username: username,
		ParentID: mu.ParentID,
		FileSha1: mu.FileSha1,
		FileName: mu.FileName,
		DirPath:  targetDirPath,
		Status:   model.UserFileStatusActive,
	}); err != nil {
		slog.WarnContext(ctx, "create user file relation failed", "error", err)
		return model.FileMeta{}, errors.Join(fmt.Errorf("create user file relation failed: %w", err), s.deleteStoredFile(ctx, mu.FileSha1, true))
	}

	// 4. 更新分片元数据状态
	if err := s.multipartRepo.UpdateStatus(ctx, mu.UploadID, username, model.MultipartStatusCompleted); err != nil {
		return model.FileMeta{}, fmt.Errorf("update multipart status failed: %w", err)
	}

	// 5. 缓存标记 + 投递 AI 分析任务
	s.cacheMark(ctx, mu.FileSha1)
	s.enqueue(ctx, mu.FileSha1, mu.FileName, username)
	metrics.AddUploadBytes(mu.FileSize)

	slog.InfoContext(ctx, "multipart completed successfully", "upload_id", mu.UploadID, "filehash", mu.FileSha1, "username", username)
	return model.FileMeta{
		FileSha1: mu.FileSha1,
		FileName: mu.FileName,
		FileSize: mu.FileSize,
		Username: username,
		ParentID: mu.ParentID,
		DirPath:  targetDirPath,
	}, nil
}

// AbortMultipartUpload 取消并清理 S3 分片会话
func (s *FileService) AbortMultipartUpload(ctx context.Context, username, uploadID string) error {
	if s.multipartRepo == nil {
		return fmt.Errorf("multipart repository not configured")
	}

	mu, err := s.multipartRepo.GetByUploadID(ctx, uploadID, username)
	if err != nil {
		return fmt.Errorf("upload session not found: %w", err)
	}

	if mu.Status == model.MultipartStatusAborted {
		return nil
	}
	if mu.Status != model.MultipartStatusUploading {
		return fmt.Errorf("upload session is not active")
	}
	if err := s.store.AbortMultipart(ctx, mu.FileSha1, uploadID); err != nil {
		return fmt.Errorf("abort multipart in storage failed: %w", err)
	}
	if err := s.multipartRepo.UpdateStatus(ctx, uploadID, username, model.MultipartStatusAborted); err != nil {
		return fmt.Errorf("update multipart status failed after abort: %w", err)
	}
	slog.InfoContext(ctx, "multipart aborted", "upload_id", uploadID, "username", username)
	return nil
}
