package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	hashutil "gofile/internal/common/hash"
	"gofile/internal/config"
	"gofile/internal/domain"
	"gofile/internal/observability/metrics"
	"gofile/internal/port"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

// ErrRangeOutOfBounds indicates a Range request is out of bounds (offset >= file size); handler returns 416 for this.
var ErrRangeOutOfBounds = errors.New("range out of bounds")

var (
	ErrInvalidFileHash         = errors.New("invalid file hash")
	ErrFileSizeMismatch        = errors.New("file size mismatch")
	ErrFileFingerprintMismatch = errors.New("file fingerprint mismatch")
)

// FileService handles file business logic.
type FileService struct {
	fileRepo      port.FileRepository
	multipartRepo port.MultipartRepository // optional, metadata repository for direct multipart upload
	store         port.Storage
	cfg           *config.Config
	cache         port.Cache        // optional, nil when Redis is unavailable
	taskEnqueuer  port.TaskEnqueuer // optional, nil when AI is disabled
	indexer       port.Indexer      // optional, search engine (used to clean index on delete)
}

// NewFileService creates the file service (falls back to original logic when cache is nil).
func NewFileService(fileRepo port.FileRepository, store port.Storage, cfg *config.Config, c ...port.Cache) *FileService {
	var cc port.Cache
	if len(c) > 0 {
		cc = c[0]
	}
	return &FileService{fileRepo: fileRepo, store: store, cfg: cfg, cache: cc}
}

// WithMultipart injects the direct multipart repository.
func (s *FileService) WithMultipart(r port.MultipartRepository) *FileService {
	s.multipartRepo = r
	return s
}

// WithAI injects the AI async orchestrator (called during main wiring, optional).
func (s *FileService) WithAI(p port.TaskEnqueuer) *FileService {
	s.taskEnqueuer = p
	return s
}

// WithIndexer injects the search engine (for cleaning index on delete, optional).
func (s *FileService) WithIndexer(idx port.Indexer) *FileService {
	s.indexer = idx
	return s
}

// enqueue triggers async AI analysis (nil-safe, does not block the main upload path).
func (s *FileService) enqueue(ctx context.Context, filehash, filename, username string) {
	if s.taskEnqueuer != nil {
		if err := s.taskEnqueuer.Enqueue(context.WithoutCancel(ctx), filehash, filename, username); err != nil {
			slog.WarnContext(ctx, "enqueue ai task failed", "filehash", filehash, "username", username, "error", err)
		}
	}
}

// cacheMark silently marks the hash in Redis cache (failure does not block business logic).
func (s *FileService) cacheMark(ctx context.Context, hash string) {
	if s.cache != nil {
		if err := s.cache.MarkHash(ctx, hash); err != nil {
			slog.WarnContext(ctx, "cache mark hash failed", "hash", hash, "error", err)
		}
	}
}

// Upload handles file upload (including fast-dedup check).
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
	// compute file hash
	sha1Stream := &hashutil.Sha1Stream{}
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

	// fast-dedup check: look up Redis cache first (O(1)), then confirm with storage layer on hit
	if s.cache != nil {
		seen, cacheErr := s.cache.HashExists(ctx, fileSha1)
		if cacheErr != nil {
			slog.WarnContext(ctx, "cache hash lookup failed", "hash", fileSha1, "error", cacheErr)
		}
		if seen {
			// Redis reports seen, re-confirm with storage layer (prevent false positives)
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

	// traditional fast-dedup check: query storage directly
	exists, err := s.store.Exists(ctx, fileSha1)
	if err != nil {
		return model.FileMeta{}, fmt.Errorf("dedup check failed: %w", err)
	} else if exists {
		if err := s.validateStoredSize(ctx, fileSha1, totalSize); err != nil {
			return model.FileMeta{}, err
		}
		// fast dedup succeeded, mark in Redis cache for fast future checks
		s.cacheMark(ctx, fileSha1)
		if err := s.fileRepo.CreateUserFile(ctx, model.UserFile{Username: username, FileSha1: fileSha1, FileName: filename, Status: model.UserFileStatusActive}); err != nil {
			return model.FileMeta{}, fmt.Errorf("save user file relation failed: %w", err)
		}
		s.enqueue(ctx, fileSha1, filename, username)
		slog.InfoContext(ctx, "fast upload (dedup)", "filehash", fileSha1, "username", username)
		return model.FileMeta{FileSha1: fileSha1, FileName: filename, FileSize: totalSize, Username: username}, nil
	}

	// reposition file pointer
	// upload to storage layer
	if err := s.store.Put(ctx, fileSha1, tmp, totalSize); err != nil {
		return model.FileMeta{}, fmt.Errorf("store file failed: %w", err)
	}

	// register global file (INSERT IGNORE idempotent)
	if err := s.fileRepo.Create(ctx, model.File{FileSha1: fileSha1, FileName: filename, FileSize: totalSize, FileAddr: fileSha1}); err != nil {
		slog.WarnContext(ctx, "save global file meta failed, rolling back storage", "filehash", fileSha1)
		return model.FileMeta{}, errors.Join(fmt.Errorf("save file meta failed: %w", err), s.deleteStoredFile(ctx, fileSha1, false))
	}

	// create user ownership relation
	if err := s.fileRepo.CreateUserFile(ctx, model.UserFile{Username: username, FileSha1: fileSha1, FileName: filename, Status: model.UserFileStatusActive}); err != nil {
		slog.WarnContext(ctx, "save user file relation failed", "error", err, "filehash", fileSha1)
		return model.FileMeta{}, errors.Join(fmt.Errorf("save user file relation failed: %w", err), s.deleteStoredFile(ctx, fileSha1, true))
	}

	// mark in Redis cache, future fast dedup will hit directly
	s.cacheMark(ctx, fileSha1)

	// trigger async AI analysis (fast-dedup hits return early and will not reach here)
	s.enqueue(ctx, fileSha1, filename, username)

	// business metric: accumulate actual uploaded bytes (fast-dedup hits return early and will not reach here)
	metrics.AddUploadBytes(totalSize)
	slog.InfoContext(ctx, "file uploaded", "filename", filename, "size", totalSize, "hash", fileSha1, "username", username)
	return model.FileMeta{FileSha1: fileSha1, FileName: filename, FileSize: totalSize, Username: username}, nil
}

// PresignUpload generates a presigned upload URL.
// Frontend pre-computes file SHA1 and passes filehash; backend issues URL + fast-dedup check.
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
	// fast-dedup check: Redis cache hit -> re-confirm with storage layer
	if s.cache != nil {
		if seen, _ := s.cache.HashExists(ctx, fileHash); seen {
			if exists, _ := s.store.Exists(ctx, fileHash); exists {
				s.fileRepo.CreateUserFile(ctx, model.UserFile{Username: username, FileSha1: fileHash, FileName: "", Status: model.UserFileStatusActive})
				return "", fmt.Errorf("file already exists")
			}
		}
	}
	// traditional fast-dedup check
	if exists, _ := s.store.Exists(ctx, fileHash); exists {
		s.fileRepo.CreateUserFile(ctx, model.UserFile{Username: username, FileSha1: fileHash, FileName: "", Status: model.UserFileStatusActive})
		s.cacheMark(ctx, fileHash)
		return "", fmt.Errorf("file already exists")
	}

	// issue presigned upload URL (valid for 15 minutes)
	url, err := s.store.PresignPut(ctx, fileHash, 15*time.Minute)
	if err != nil {
		return "", fmt.Errorf("presign upload failed: %w", err)
	}

	slog.InfoContext(ctx, "presigned upload URL generated", "filehash", fileHash, "username", username)
	return url, nil
}

// ConfirmUpload confirms the presigned upload is complete.
// Frontend calls this after PUTing the file to MinIO; backend verifies file existence and creates metadata.
func (s *FileService) ConfirmUpload(ctx context.Context, fileHash, fileName, username string) error {
	if err := validateFileHash(fileHash); err != nil {
		return err
	}
	// verify the file actually exists in storage
	exists, err := s.store.Exists(ctx, fileHash)
	if err != nil {
		return fmt.Errorf("check file in storage failed: %w", err)
	}
	if !exists {
		return fmt.Errorf("file not found in storage")
	}

	// get actual size of the file in storage
	size, actualHash, err := s.inspectStoredFile(ctx, fileHash)
	if err != nil {
		return fmt.Errorf("presigned confirm: inspect file failed: %w", err)
	}
	if actualHash != fileHash {
		return fmt.Errorf("%w: expected %s, got %s", ErrFileFingerprintMismatch, fileHash, actualHash)
	}

	// register global file (idempotent)
	if err := s.fileRepo.Create(ctx, model.File{FileSha1: fileHash, FileSize: size, FileAddr: fileHash}); err != nil {
		return fmt.Errorf("presigned confirm: save global file meta failed: %w", err)
	}

	// create user ownership relation
	if err := s.fileRepo.CreateUserFile(ctx, model.UserFile{Username: username, FileSha1: fileHash, FileName: fileName, Status: model.UserFileStatusActive}); err != nil {
		return fmt.Errorf("save user file relation failed: %w", err)
	}

	// mark in Redis cache
	s.cacheMark(ctx, fileHash)

	// trigger async AI analysis
	s.enqueue(ctx, fileHash, fileName, username)

	slog.InfoContext(ctx, "presigned upload confirmed", "filehash", fileHash, "filename", fileName, "username", username)
	return nil
}

// PresignDownload generates a presigned download URL.
// Issues a download URL valid for 5 minutes after verifying user ownership.
func (s *FileService) PresignDownload(ctx context.Context, fileHash, username string) (string, error) {
	// verify file ownership
	_, err := s.fileRepo.GetByHash(ctx, fileHash, username)
	if err != nil {
		return "", fmt.Errorf("file not found or no permission")
	}

	// issue presigned download URL (valid for 5 minutes)
	url, err := s.store.PresignGet(ctx, fileHash, 5*time.Minute)
	if err != nil {
		return "", fmt.Errorf("presign download failed: %w", err)
	}

	slog.InfoContext(ctx, "presigned download URL generated", "filehash", fileHash, "username", username)
	return url, nil
}

// FastUpload fast-dedup check: creates ownership relation for current user when global storage hits (idempotent).
// Returns exists=true on fast-dedup success; false means caller should continue normal upload.
func (s *FileService) FastUpload(ctx context.Context, fileHash, username string) (bool, error) {
	exists, err := s.store.Exists(ctx, fileHash)
	if err != nil || !exists {
		return false, err
	}
	// create user ownership relation (idempotent INSERT IGNORE); filename reuses global record, empty if not found
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

// GetMeta gets file metadata.
func (s *FileService) GetMeta(ctx context.Context, filehash, username string) (model.FileMeta, error) {
	return s.fileRepo.GetByHash(ctx, filehash, username)
}

// FileSize gets file size (with ownership check), used for 416 responses and similar cases.
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

// Download gets the file read stream.
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

// DownloadRange downloads a byte range (supports HTTP Range).
// Returns the clipped actual length (open-ended/out-of-bounds requests are clamped to file size) for handler to build response headers.
func (s *FileService) DownloadRange(ctx context.Context, filehash, username string, offset, length int64) (io.ReadCloser, model.FileMeta, int64, int64, error) {
	fMeta, err := s.fileRepo.GetByHash(ctx, filehash, username)
	if err != nil {
		return nil, model.FileMeta{}, 0, 0, fmt.Errorf("file not found: %w", err)
	}

	// get total file size
	totalSize, err := s.store.FileSize(ctx, fMeta.FileSha1)
	if err != nil {
		return nil, model.FileMeta{}, 0, 0, fmt.Errorf("get file size failed: %w", err)
	}

	// for open-ended ranges (bytes=a-), length is -1 and is filled by file size; clamp to EOF if out of bounds
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

// Rename renames a file (with ownership verification).
func (s *FileService) Rename(ctx context.Context, filehash, username, newName string) error {
	// verify file ownership
	_, err := s.fileRepo.GetByHash(ctx, filehash, username)
	if err != nil {
		return fmt.Errorf("file not found or no permission")
	}

	_, err = s.fileRepo.UpdateName(ctx, filehash, username, newName)
	return err
}

// Delete soft-deletes a user file (with ownership verification + index cleanup).
func (s *FileService) Delete(ctx context.Context, filehash, username string) error {
	// verify file ownership
	_, err := s.fileRepo.GetByHash(ctx, filehash, username)
	if err != nil {
		return fmt.Errorf("file not found or no permission")
	}

	_, err = s.fileRepo.Delete(ctx, filehash, username)
	if err != nil {
		return err
	}

	// clean Typesense index when no active references remain
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

// ListByUser lists all files for a user.
func (s *FileService) ListByUser(ctx context.Context, username string) ([]model.FileMeta, error) {
	return s.fileRepo.ListByUser(ctx, username)
}

// ListByUserPaged lists user files with pagination.
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

// CountByUser counts total files for a user.
func (s *FileService) CountByUser(ctx context.Context, username string) (int64, error) {
	return s.fileRepo.CountByUser(ctx, username)
}
