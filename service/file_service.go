package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"gofile/ai"
	"gofile/cache"
	"gofile/config"
	"gofile/metrics"
	"gofile/model"
	"gofile/repository"
	"gofile/storage"
	"gofile/util"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

// ErrRangeOutOfBounds Range 请求越界（offset >= 文件大小），handler 据此返回 416
var ErrRangeOutOfBounds = errors.New("range out of bounds")

var (
	ErrInvalidFileHash         = errors.New("invalid file hash")
	ErrFileSizeMismatch        = errors.New("file size mismatch")
	ErrFileFingerprintMismatch = errors.New("file fingerprint mismatch")
)

// FileService 文件业务逻辑
type FileService struct {
	fileRepo      repository.FileRepository
	multipartRepo repository.MultipartRepository // 可选，分片直传元数据仓库
	store         storage.Storage
	cfg           *config.Config
	cache         *cache.Client // 可选，Redis 不可用时为 nil
	ai            *ai.Processor // 可选，AI 功能关闭时为 nil
	indexer       ai.Indexer    // 可选，检索引擎（用于删除时清理索引）
}

// NewFileService 创建文件服务（cache 为 nil 时回退到原有逻辑）
func NewFileService(fileRepo repository.FileRepository, store storage.Storage, cfg *config.Config, c ...*cache.Client) *FileService {
	var cc *cache.Client
	if len(c) > 0 {
		cc = c[0]
	}
	return &FileService{fileRepo: fileRepo, store: store, cfg: cfg, cache: cc}
}

// WithMultipart 注入分片直传仓库
func (s *FileService) WithMultipart(r repository.MultipartRepository) *FileService {
	s.multipartRepo = r
	return s
}

// WithAI 注入 AI 异步编排器（main 组装时调用，可选）
func (s *FileService) WithAI(p *ai.Processor) *FileService {
	s.ai = p
	return s
}

// WithIndexer 注入检索引擎（删除时清理索引用，可选）
func (s *FileService) WithIndexer(idx ai.Indexer) *FileService {
	s.indexer = idx
	return s
}

// enqueue 触发 AI 异步分析（nil 安全，不阻断上传主链路）
func (s *FileService) enqueue(ctx context.Context, filehash, filename, username string) {
	if s.ai != nil {
		s.ai.Enqueue(context.WithoutCancel(ctx), filehash, filename, username)
	}
}

// cacheMark 静默将 hash 记入 Redis 缓存（失败不阻断业务）
func (s *FileService) cacheMark(ctx context.Context, hash string) {
	if s.cache != nil {
		if err := s.cache.MarkHash(ctx, hash); err != nil {
			slog.WarnContext(ctx, "cache mark hash failed", "hash", hash, "error", err)
		}
	}
}

// Upload 处理文件上传（含秒传检测）
func (s *FileService) Upload(ctx context.Context, file io.Reader, filename string, fileSize int64, username string) (model.FileMeta, error) {
	if file == nil {
		return model.FileMeta{}, fmt.Errorf("file is nil")
	}
	if fileSize < 0 {
		return model.FileMeta{}, fmt.Errorf("%w: expected %d", ErrFileSizeMismatch, fileSize)
	}
	tmp, err := os.CreateTemp("", "gofile-upload-*")
	if err != nil {
		return model.FileMeta{}, fmt.Errorf("create upload temp file failed: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	// 计算文件 hash
	sha1Stream := &util.Sha1Stream{}
	sha1Stream.Update(nil) // initialize the stream so empty files hash safely
	buf := make([]byte, 32*1024)
	var totalSize int64
	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			if _, err := tmp.Write(buf[:n]); err != nil {
				return model.FileMeta{}, fmt.Errorf("stage file failed: %w", err)
			}
			sha1Stream.Update(buf[:n])
			totalSize += int64(n)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return model.FileMeta{}, fmt.Errorf("file read failed: %w", readErr)
		}
	}
	if fileSize > 0 && fileSize != totalSize {
		return model.FileMeta{}, fmt.Errorf("%w: expected %d, got %d", ErrFileSizeMismatch, fileSize, totalSize)
	}
	if err := tmp.Sync(); err != nil {
		return model.FileMeta{}, fmt.Errorf("sync staged file failed: %w", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return model.FileMeta{}, fmt.Errorf("seek staged file failed: %w", err)
	}
	fileSha1 := sha1Stream.Sum()

	// 秒传检测：先查 Redis 缓存（O(1)），命中再问存储层确认
	if s.cache != nil {
		seen, cacheErr := s.cache.HashExists(ctx, fileSha1)
		if cacheErr != nil {
			slog.WarnContext(ctx, "cache hash lookup failed", "hash", fileSha1, "error", cacheErr)
		}
		if seen {
			// Redis 说见过，再确认存储层（防止误判）
			exists, err := s.store.Exists(ctx, fileSha1)
			if err != nil {
				return model.FileMeta{}, fmt.Errorf("check existing file failed: %w", err)
			}
			if exists {
				if err := s.validateStoredSize(ctx, fileSha1, totalSize); err != nil {
					return model.FileMeta{}, err
				}
				if err := s.fileRepo.CreateUserFile(ctx, model.UserFile{Username: username, FileSha1: fileSha1, FileName: filename, Status: model.UserFileStatusActive}); err != nil {
					return model.FileMeta{}, fmt.Errorf("save user file relation failed: %w", err)
				}
				s.enqueue(ctx, fileSha1, filename, username)
				slog.InfoContext(ctx, "fast upload (dedup, cache hit)", "filehash", fileSha1, "username", username)
				return model.FileMeta{FileSha1: fileSha1, FileName: filename, FileSize: totalSize, Username: username}, nil
			}
		}
	}

	// 传统秒传检测：直接问存储层
	exists, err := s.store.Exists(ctx, fileSha1)
	if err != nil {
		return model.FileMeta{}, fmt.Errorf("dedup check failed: %w", err)
	} else if exists {
		if err := s.validateStoredSize(ctx, fileSha1, totalSize); err != nil {
			return model.FileMeta{}, err
		}
		// 秒传成功，记入 Redis 缓存供后续快速判断
		s.cacheMark(ctx, fileSha1)
		if err := s.fileRepo.CreateUserFile(ctx, model.UserFile{Username: username, FileSha1: fileSha1, FileName: filename, Status: model.UserFileStatusActive}); err != nil {
			return model.FileMeta{}, fmt.Errorf("save user file relation failed: %w", err)
		}
		s.enqueue(ctx, fileSha1, filename, username)
		slog.InfoContext(ctx, "fast upload (dedup)", "filehash", fileSha1, "username", username)
		return model.FileMeta{FileSha1: fileSha1, FileName: filename, FileSize: totalSize, Username: username}, nil
	}

	// 重新定位文件指针
	// 上传到存储层
	if err := s.store.Put(ctx, fileSha1, tmp, totalSize); err != nil {
		return model.FileMeta{}, fmt.Errorf("store file failed: %w", err)
	}

	// 注册全局文件（INSERT IGNORE 幂等）
	if err := s.fileRepo.Create(ctx, model.File{FileSha1: fileSha1, FileName: filename, FileSize: totalSize, FileAddr: fileSha1}); err != nil {
		slog.WarnContext(ctx, "save global file meta failed, rolling back storage", "filehash", fileSha1)
		return model.FileMeta{}, errors.Join(fmt.Errorf("save file meta failed: %w", err), s.deleteStoredFile(ctx, fileSha1, false))
	}

	// 建立用户拥有关系
	if err := s.fileRepo.CreateUserFile(ctx, model.UserFile{Username: username, FileSha1: fileSha1, FileName: filename, Status: model.UserFileStatusActive}); err != nil {
		slog.WarnContext(ctx, "save user file relation failed", "error", err, "filehash", fileSha1)
		return model.FileMeta{}, errors.Join(fmt.Errorf("save user file relation failed: %w", err), s.deleteStoredFile(ctx, fileSha1, true))
	}

	// 记入 Redis 缓存，后续秒传直接命中
	s.cacheMark(ctx, fileSha1)

	// 触发 AI 异步分析（秒传命中会提前 return，不会走到这里）
	s.enqueue(ctx, fileSha1, filename, username)

	// 业务指标：累计真实上传字节（秒传命中会提前 return，不会走到这里）
	metrics.AddUploadBytes(totalSize)
	slog.InfoContext(ctx, "file uploaded", "filename", filename, "size", totalSize, "hash", fileSha1, "username", username)
	return model.FileMeta{FileSha1: fileSha1, FileName: filename, FileSize: totalSize, Username: username}, nil
}

// PresignUpload 生成预签名上传 URL
// 前端先算好文件 SHA1 传入 filehash，后端签发 URL + 秒传检测
func validateFileHash(fileHash string) error {
	if len(fileHash) != sha1.Size*2 || fileHash != strings.ToLower(fileHash) {
		return fmt.Errorf("%w: %q", ErrInvalidFileHash, fileHash)
	}
	if _, err := hex.DecodeString(fileHash); err != nil {
		return fmt.Errorf("%w: %q", ErrInvalidFileHash, fileHash)
	}
	return nil
}

func (s *FileService) validateStoredSize(ctx context.Context, fileHash string, expected int64) error {
	actual, err := s.store.FileSize(ctx, fileHash)
	if err != nil {
		return fmt.Errorf("get stored file size failed: %w", err)
	}
	if actual != expected {
		return fmt.Errorf("%w: expected %d, got %d", ErrFileSizeMismatch, expected, actual)
	}
	return nil
}

func (s *FileService) inspectStoredFile(ctx context.Context, fileHash string) (int64, string, error) {
	size, err := s.store.FileSize(ctx, fileHash)
	if err != nil {
		return 0, "", fmt.Errorf("get stored file size failed: %w", err)
	}
	r, err := s.store.Get(ctx, fileHash)
	if err != nil {
		return 0, "", fmt.Errorf("get stored file failed: %w", err)
	}
	defer r.Close()
	h := sha1.New()
	readSize, err := io.Copy(h, r)
	if err != nil {
		return 0, "", fmt.Errorf("read stored file failed: %w", err)
	}
	if readSize != size {
		return 0, "", fmt.Errorf("%w: expected %d, got %d", ErrFileSizeMismatch, size, readSize)
	}
	return size, hex.EncodeToString(h.Sum(nil)), nil
}

func (s *FileService) deleteStoredFile(ctx context.Context, fileHash string, removeGlobal bool) error {
	var cleanupErr error
	refs, err := s.fileRepo.CountRefs(ctx, fileHash)
	if err != nil {
		return fmt.Errorf("check stored file references failed: %w", err)
	}
	if refs > 0 {
		return nil
	}
	if err := s.store.Delete(ctx, fileHash); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete stored file failed: %w", err))
	}
	if removeGlobal {
		if err := s.fileRepo.RemoveOrphan(ctx, fileHash); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove orphan file metadata failed: %w", err))
		}
	}
	return cleanupErr
}

func (s *FileService) PresignUpload(ctx context.Context, fileHash, username string) (string, error) {
	// 秒传检测：Redis 缓存命中 → 再确认存储层
	if s.cache != nil {
		if seen, _ := s.cache.HashExists(ctx, fileHash); seen {
			if exists, _ := s.store.Exists(ctx, fileHash); exists {
				s.fileRepo.CreateUserFile(ctx, model.UserFile{Username: username, FileSha1: fileHash, FileName: "", Status: model.UserFileStatusActive})
				return "", fmt.Errorf("file already exists")
			}
		}
	}
	// 传统秒传检测
	if exists, _ := s.store.Exists(ctx, fileHash); exists {
		s.fileRepo.CreateUserFile(ctx, model.UserFile{Username: username, FileSha1: fileHash, FileName: "", Status: model.UserFileStatusActive})
		s.cacheMark(ctx, fileHash)
		return "", fmt.Errorf("file already exists")
	}

	// 签发预签名上传 URL（15 分钟有效）
	url, err := s.store.PresignPut(ctx, fileHash, 15*time.Minute)
	if err != nil {
		return "", fmt.Errorf("presign upload failed: %w", err)
	}

	slog.InfoContext(ctx, "presigned upload URL generated", "filehash", fileHash, "username", username)
	return url, nil
}

// ConfirmUpload 确认预签名上传完成
// 前端 PUT 文件到 MinIO 后调用此接口，后端验证文件存在并创建元数据
func (s *FileService) ConfirmUpload(ctx context.Context, fileHash, fileName, username string) error {
	if err := validateFileHash(fileHash); err != nil {
		return err
	}
	// 验证文件确实存在于存储层
	exists, err := s.store.Exists(ctx, fileHash)
	if err != nil {
		return fmt.Errorf("check file in storage failed: %w", err)
	}
	if !exists {
		return fmt.Errorf("file not found in storage")
	}

	// 获取文件在存储层的实际大小
	size, actualHash, err := s.inspectStoredFile(ctx, fileHash)
	if err != nil {
		return fmt.Errorf("presigned confirm: inspect file failed: %w", err)
	}
	if actualHash != fileHash {
		return fmt.Errorf("%w: expected %s, got %s", ErrFileFingerprintMismatch, fileHash, actualHash)
	}

	// 注册全局文件（幂等）
	if err := s.fileRepo.Create(ctx, model.File{FileSha1: fileHash, FileSize: size, FileAddr: fileHash}); err != nil {
		return fmt.Errorf("presigned confirm: save global file meta failed: %w", err)
	}

	// 建立用户拥有关系
	if err := s.fileRepo.CreateUserFile(ctx, model.UserFile{Username: username, FileSha1: fileHash, FileName: fileName, Status: model.UserFileStatusActive}); err != nil {
		return fmt.Errorf("save user file relation failed: %w", err)
	}

	// 记入 Redis 缓存
	s.cacheMark(ctx, fileHash)

	// 触发 AI 异步分析
	s.enqueue(ctx, fileHash, fileName, username)

	slog.InfoContext(ctx, "presigned upload confirmed", "filehash", fileHash, "filename", fileName, "username", username)
	return nil
}

// PresignDownload 生成预签名下载 URL
// 验证用户所有权后签发 5 分钟有效的下载 URL
func (s *FileService) PresignDownload(ctx context.Context, fileHash, username string) (string, error) {
	// 验证文件所有权
	_, err := s.fileRepo.GetByHash(ctx, fileHash, username)
	if err != nil {
		return "", fmt.Errorf("file not found or no permission")
	}

	// 签发预签名下载 URL（5 分钟有效）
	url, err := s.store.PresignGet(ctx, fileHash, 5*time.Minute)
	if err != nil {
		return "", fmt.Errorf("presign download failed: %w", err)
	}

	slog.InfoContext(ctx, "presigned download URL generated", "filehash", fileHash, "username", username)
	return url, nil
}

// FastUpload 秒传检测:全局存储命中时建立当前用户的所有权关联(幂等)
// 返回 exists=true 表示秒传成功;失败时返回 false(调用方继续走完整上传)
func (s *FileService) FastUpload(ctx context.Context, fileHash, username string) (bool, error) {
	exists, err := s.store.Exists(ctx, fileHash)
	if err != nil || !exists {
		return false, err
	}
	// 建立用户拥有关系(幂等 INSERT IGNORE);文件名取全局记录,取不到则留空
	fileName := ""
	if global, gErr := s.fileRepo.GetGlobalFile(ctx, fileHash); gErr == nil {
		fileName = global.FileName
	}
	if ufErr := s.fileRepo.CreateUserFile(ctx, model.UserFile{Username: username, FileSha1: fileHash, FileName: fileName, Status: model.UserFileStatusActive}); ufErr != nil {
		slog.WarnContext(ctx, "fast upload: create user file relation failed", "error", ufErr, "filehash", fileHash, "username", username)
	}
	s.cacheMark(ctx, fileHash)
	s.enqueue(ctx, fileHash, fileName, username)
	slog.InfoContext(ctx, "fast upload (dedup)", "filehash", fileHash, "username", username)
	return true, nil
}

// GetMeta 获取文件元信息
func (s *FileService) GetMeta(ctx context.Context, filehash, username string) (model.FileMeta, error) {
	return s.fileRepo.GetByHash(ctx, filehash, username)
}

// FileSize 获取文件大小（含所有权校验），供 416 响应等场景使用
func (s *FileService) FileSize(ctx context.Context, filehash, username string) (int64, error) {
	if _, err := s.fileRepo.GetByHash(ctx, filehash, username); err != nil {
		return 0, fmt.Errorf("file not found: %w", err)
	}
	size, err := s.store.FileSize(ctx, filehash)
	if err != nil {
		return 0, fmt.Errorf("get file size failed: %w", err)
	}
	return size, nil
}

// Download 获取文件读取流
func (s *FileService) Download(ctx context.Context, filehash, username string) (io.ReadCloser, model.FileMeta, error) {
	fMeta, err := s.fileRepo.GetByHash(ctx, filehash, username)
	if err != nil {
		return nil, model.FileMeta{}, fmt.Errorf("file not found: %w", err)
	}

	reader, err := s.store.Get(ctx, fMeta.FileSha1)
	if err != nil {
		return nil, model.FileMeta{}, fmt.Errorf("get file from storage failed: %w", err)
	}

	return reader, fMeta, nil
}

// DownloadRange 按字节区间下载文件（支持 HTTP Range）
// 返回裁剪后的实际 length（开放区间/越界请求按文件大小收敛），供 handler 构造响应头
func (s *FileService) DownloadRange(ctx context.Context, filehash, username string, offset, length int64) (io.ReadCloser, model.FileMeta, int64, int64, error) {
	fMeta, err := s.fileRepo.GetByHash(ctx, filehash, username)
	if err != nil {
		return nil, model.FileMeta{}, 0, 0, fmt.Errorf("file not found: %w", err)
	}

	// 获取文件总大小
	totalSize, err := s.store.FileSize(ctx, fMeta.FileSha1)
	if err != nil {
		return nil, model.FileMeta{}, 0, 0, fmt.Errorf("get file size failed: %w", err)
	}

	// 开放区间（bytes=a-）时 length 为 -1，按文件总大小补齐；越界时裁剪到文件末尾
	if offset < 0 || offset >= totalSize {
		return nil, model.FileMeta{}, totalSize, 0, ErrRangeOutOfBounds
	}
	if length < 0 || offset+length > totalSize {
		length = totalSize - offset
	}

	reader, err := s.store.GetRange(ctx, fMeta.FileSha1, offset, length)
	if err != nil {
		return nil, model.FileMeta{}, 0, 0, fmt.Errorf("get range failed: %w", err)
	}

	return reader, fMeta, totalSize, length, nil
}

// Rename 重命名文件（含所有权验证）
func (s *FileService) Rename(ctx context.Context, filehash, username, newName string) error {
	// 验证文件所有权
	_, err := s.fileRepo.GetByHash(ctx, filehash, username)
	if err != nil {
		return fmt.Errorf("file not found or no permission")
	}

	_, err = s.fileRepo.UpdateName(ctx, filehash, username, newName)
	return err
}

// Delete 软删除用户文件（含所有权验证 + 索引清理）
func (s *FileService) Delete(ctx context.Context, filehash, username string) error {
	// 验证文件所有权
	_, err := s.fileRepo.GetByHash(ctx, filehash, username)
	if err != nil {
		return fmt.Errorf("file not found or no permission")
	}

	_, err = s.fileRepo.Delete(ctx, filehash, username)
	if err != nil {
		return err
	}

	// 无活跃引用时清理 Typesense 索引
	if s.indexer != nil {
		refs, _ := s.fileRepo.CountRefs(ctx, filehash)
		if refs == 0 {
			if err := s.indexer.Delete(context.Background(), username, filehash); err != nil {
				slog.Warn("delete: clean typesense index failed", "error", err, "filehash", filehash)
			}
		}
	}
	return nil
}

// ListByUser 获取用户的所有文件
func (s *FileService) ListByUser(ctx context.Context, username string) ([]model.FileMeta, error) {
	return s.fileRepo.ListByUser(ctx, username)
}

// ListByUserPaged 分页查询用户文件
func (s *FileService) ListByUserPaged(ctx context.Context, username string, page, size int) ([]model.FileMeta, int64, error) {
	total, err := s.fileRepo.CountByUser(ctx, username)
	if err != nil {
		return nil, 0, err
	}
	files, err := s.fileRepo.ListByUserPaged(ctx, username, page, size)
	if err != nil {
		return nil, 0, err
	}
	return files, total, nil
}

// CountByUser 获取用户文件总数
func (s *FileService) CountByUser(ctx context.Context, username string) (int64, error) {
	return s.fileRepo.CountByUser(ctx, username)
}
