package service

import (
	"context"
	"fmt"
	"gofile/config"
	"gofile/model"
	"gofile/repository"
	"gofile/storage"
	"gofile/util"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// FileService 文件业务逻辑
type FileService struct {
	fileRepo repository.FileRepository
	store    storage.Storage
	cfg      *config.Config
}

// NewFileService 创建文件服务
func NewFileService(fileRepo repository.FileRepository, store storage.Storage, cfg *config.Config) *FileService {
	return &FileService{fileRepo: fileRepo, store: store, cfg: cfg}
}

// Upload 处理文件上传（含秒传检测）
func (s *FileService) Upload(ctx context.Context, file io.Reader, filename string, fileSize int64, username string) (model.FileMeta, error) {
	// 计算文件 hash
	sha1Stream := &util.Sha1Stream{}
	buf := make([]byte, 32*1024)
	var totalSize int64
	for {
		n, readErr := file.Read(buf)
		if n > 0 {
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
	fileSha1 := sha1Stream.Sum()

	// 秒传检测：文件已存在于存储层
	exists, err := s.store.Exists(ctx, fileSha1)
	if err != nil {
		slog.Warn("dedup check failed", "error", err, "filehash", fileSha1)
	} else if exists {
		// 创建用户拥有关系（幂等）
		uf := model.UserFile{
			Username: username,
			FileSha1: fileSha1,
			FileName: filename,
			Status:   1,
		}
		// 即使 CreateUserFile 失败，也视为秒传成功（已存在则忽略）
		if ufErr := s.fileRepo.CreateUserFile(uf); ufErr != nil {
			slog.Warn("create user file during dedup failed", "error", ufErr, "filehash", fileSha1)
		}
		slog.Info("fast upload (dedup)", "filehash", fileSha1, "username", username)
		return model.FileMeta{FileSha1: fileSha1, FileName: filename, FileSize: totalSize, Username: username}, nil
	}

	// 重新定位文件指针
	if seeker, ok := file.(io.Seeker); ok {
		if _, err := seeker.Seek(0, 0); err != nil {
			return model.FileMeta{}, fmt.Errorf("seek file failed: %w", err)
		}
	}

	// 上传到存储层
	if err := s.store.Put(ctx, fileSha1, file, totalSize); err != nil {
		return model.FileMeta{}, fmt.Errorf("store file failed: %w", err)
	}

	// 注册全局文件（INSERT IGNORE 幂等）
	f := model.File{
		FileSha1: fileSha1,
		FileSize: totalSize,
		FileAddr: fileSha1,
	}
	if err := s.fileRepo.Create(f); err != nil {
		slog.Warn("save global file meta failed, rolling back storage", "filehash", fileSha1)
		if delErr := s.store.Delete(ctx, fileSha1); delErr != nil {
			slog.Error("rollback storage failed", "error", delErr, "filehash", fileSha1)
		}
		return model.FileMeta{}, fmt.Errorf("save file meta failed: %w", err)
	}

	// 建立用户拥有关系
	uf := model.UserFile{
		Username: username,
		FileSha1: fileSha1,
		FileName: filename,
		Status:   1,
	}
	if err := s.fileRepo.CreateUserFile(uf); err != nil {
		slog.Warn("save user file relation failed", "error", err, "filehash", fileSha1)
		// 用户关系失败不影响全局文件，但需要告知用户
		return model.FileMeta{}, fmt.Errorf("save user file relation failed: %w", err)
	}

	slog.Info("file uploaded", "filename", filename, "size", totalSize, "hash", fileSha1, "username", username)
	return model.FileMeta{FileSha1: fileSha1, FileName: filename, FileSize: totalSize, Username: username}, nil
}

// FastUpload 秒传检测
func (s *FileService) FastUpload(ctx context.Context, fileHash string) (bool, error) {
	return s.store.Exists(ctx, fileHash)
}

// GetMeta 获取文件元信息
func (s *FileService) GetMeta(filehash, username string) (model.FileMeta, error) {
	return s.fileRepo.GetByHash(filehash, username)
}

// Download 获取文件读取流
func (s *FileService) Download(ctx context.Context, filehash, username string) (io.ReadCloser, model.FileMeta, error) {
	fMeta, err := s.fileRepo.GetByHash(filehash, username)
	if err != nil {
		return nil, model.FileMeta{}, fmt.Errorf("file not found: %w", err)
	}

	reader, err := s.store.Get(ctx, fMeta.FileSha1)
	if err != nil {
		return nil, model.FileMeta{}, fmt.Errorf("get file from storage failed: %w", err)
	}

	return reader, fMeta, nil
}

// Rename 重命名文件（含所有权验证）
func (s *FileService) Rename(filehash, username, newName string) error {
	// 验证文件所有权
	_, err := s.fileRepo.GetByHash(filehash, username)
	if err != nil {
		return fmt.Errorf("file not found or no permission")
	}

	_, err = s.fileRepo.UpdateName(filehash, username, newName)
	return err
}

// Delete 软删除用户文件（含所有权验证）
func (s *FileService) Delete(filehash, username string) error {
	// 验证文件所有权
	_, err := s.fileRepo.GetByHash(filehash, username)
	if err != nil {
		return fmt.Errorf("file not found or no permission")
	}

	_, err = s.fileRepo.Delete(filehash, username)
	return err
}

// ListByUser 获取用户的所有文件
func (s *FileService) ListByUser(username string) ([]model.FileMeta, error) {
	return s.fileRepo.ListByUser(username)
}

// UploadChunk 上传分片（用户隔离）
func (s *FileService) UploadChunk(fileHash string, index int, file io.Reader, username string) error {
	// 已上传过的分片直接返回
	if util.ChunkExists(s.cfg.ChunkDir, username, fileHash, index) {
		return nil
	}

	dir := filepath.Join(s.cfg.ChunkDir, filepath.Base(username), filepath.Base(fileHash))
	os.MkdirAll(dir, 0755)

	chunkPath := filepath.Join(dir, strconv.Itoa(index))

	dst, err := os.Create(chunkPath)
	if err != nil {
		return fmt.Errorf("create chunk failed: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		return fmt.Errorf("write chunk failed: %w", err)
	}

	slog.Info("chunk uploaded", "filehash", fileHash, "index", index, "username", username)
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
	chunkDir := filepath.Join(s.cfg.ChunkDir, filepath.Base(username), filepath.Base(fileHash))

	files, err := readChunkDir(chunkDir, totalStr)
	if err != nil {
		return model.FileMeta{}, err
	}

	// 使用 UUID 防止并发 merge 临时文件冲突
	uuidSuffix := strings.ReplaceAll(uuid.New().String(), "-", "")
	tmpPath := filepath.Join(s.cfg.ChunkDir, fileHash+"."+uuidSuffix+".tmp")
	totalSize, err := mergeChunksToTemp(chunkDir, files, tmpPath)
	if err != nil {
		return model.FileMeta{}, err
	}

	// 上传到存储层
	if err := saveMergedFile(ctx, s.store, fileHash, tmpPath, totalSize); err != nil {
		slog.Error("store merged file failed", "error", err)
		return model.FileMeta{}, fmt.Errorf("store merged file failed: %w", err)
	}

	// 注册全局文件（幂等）
	f := model.File{
		FileSha1: fileHash,
		FileSize: totalSize,
		FileAddr: fileHash,
	}
	if err := s.fileRepo.Create(f); err != nil {
		slog.Warn("save global file meta failed, rolling back", "filehash", fileHash)
		if delErr := s.store.Delete(ctx, fileHash); delErr != nil {
			slog.Error("rollback merged storage failed", "error", delErr, "filehash", fileHash)
		}
		os.RemoveAll(chunkDir)
		return model.FileMeta{}, fmt.Errorf("save file meta failed: %w", err)
	}

	// 建立用户拥有关系
	uf := model.UserFile{
		Username: username,
		FileSha1: fileHash,
		FileName: fileName,
		Status:   1,
	}
	if err := s.fileRepo.CreateUserFile(uf); err != nil {
		slog.Warn("save user file relation failed", "error", err, "filehash", fileHash)
		return model.FileMeta{}, fmt.Errorf("save user file relation failed: %w", err)
	}

	slog.Info("chunks merged", "filehash", fileHash, "filename", fileName, "size", totalSize, "username", username)
	return model.FileMeta{FileSha1: fileHash, FileName: fileName, FileSize: totalSize, Username: username}, nil
}

// readChunkDir 读取并排序 chunk 文件，校验分块数量
func readChunkDir(chunkDir, totalStr string) ([]os.DirEntry, error) {
	files, err := os.ReadDir(chunkDir)
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("chunks not found")
	}

	sort.Slice(files, func(i, j int) bool {
		iIndex, _ := strconv.Atoi(files[i].Name())
		jIndex, _ := strconv.Atoi(files[j].Name())
		return iIndex < jIndex
	})

	if totalStr != "" {
		if total, err := strconv.Atoi(totalStr); err == nil && total > 0 {
			if len(files) != total {
				return nil, fmt.Errorf("chunk count mismatch: expected %d, got %d", total, len(files))
			}
		}
	}

	return files, nil
}

// mergeChunksToTemp 将分片合并到临时文件
func mergeChunksToTemp(chunkDir string, files []os.DirEntry, tmpPath string) (int64, error) {
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return 0, fmt.Errorf("create temp file failed")
	}
	defer tmpFile.Close()

	var totalSize int64
	for _, f := range files {
		chunkPath := filepath.Join(chunkDir, f.Name())
		chunkFile, err := os.Open(chunkPath)
		if err != nil {
			os.Remove(tmpPath)
			return 0, fmt.Errorf("open chunk failed: %w", err)
		}

		written, err := io.Copy(tmpFile, chunkFile)
		chunkFile.Close()
		if err != nil {
			os.Remove(tmpPath)
			return 0, fmt.Errorf("merge chunk failed: %w", err)
		}
		totalSize += written
	}

	return totalSize, nil
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