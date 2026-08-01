package handler

import (
	"context"
	"filestore-server/config"
	"filestore-server/meta"
	"filestore-server/storage"
	"filestore-server/util"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	MaxUploadSize = 100 << 20 // 100MB
)

var (
	globalStore storage.Storage
	globalCfg   *config.Config
)

// InitStore 初始化存储层（由 main.go 调用，兼容旧模式）
func InitStore(s storage.Storage, c *config.Config) {
	globalStore = s
	globalCfg = c
}

// HealthCheckHandler 健康检查端点
func HealthCheckHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": nil})
}

// UploadHandler 处理文件上传
func UploadHandler(c *gin.Context) {
	// 限制上传大小
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxUploadSize)

	fileHash := c.PostForm("filehash")
	// 秒传检测
	if fileHash != "" {
		exists, err := globalStore.Exists(context.Background(), fileHash)
		if err != nil {
			slog.Warn("upload: fast upload check failed", "error", err, "filehash", fileHash)
		} else if exists {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "秒传成功", "data": nil})
			return
		}
	}

	// 解析上传的文件
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "文件获取失败", "data": nil})
		return
	}
	defer file.Close()

	// 路径穿越防护：只取文件名
	filename := filepath.Base(header.Filename)

	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Now().In(loc)

	// 计算文件 hash
	sha1Stream := &util.Sha1Stream{}
	buf := make([]byte, 32*1024)
	var fileSize int64
	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			sha1Stream.Update(buf[:n])
			fileSize += int64(n)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			slog.Error("read file failed", "error", readErr)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "文件读取失败", "data": nil})
			return
		}
	}
	fileSha1 := sha1Stream.Sum()

	// 再次秒传检测（基于实际 hash）
	exists, err := globalStore.Exists(context.Background(), fileSha1)
	if err != nil {
		slog.Warn("upload: second fast upload check failed", "error", err, "filehash", fileSha1)
	} else if exists {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "秒传成功", "data": gin.H{"filehash": fileSha1}})
		return
	}

	// 重新定位文件指针到开头
	if seeker, ok := file.(io.Seeker); ok {
		if _, err := seeker.Seek(0, 0); err != nil {
			slog.Error("seek file failed", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "文件处理失败", "data": nil})
			return
		}
	}

	// 上传到存储层
	if err := globalStore.Put(context.Background(), fileSha1, file, fileSize); err != nil {
		slog.Error("store file failed", "error", err, "filename", filename)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "文件上传失败", "data": nil})
		return
	}

	fileMeta := meta.FileMeta{
		FileName: filename,
		Location: fileSha1,
		FileSize: fileSize,
		FileSha1: fileSha1,
		UploadAt: now,
		Username: c.GetString("username"),
	}

	if ok := meta.UpdateFileMetaDB(fileMeta); !ok {
		slog.Warn("save file meta failed, rolling back storage", "filehash", fileMeta.FileSha1)
		if err := globalStore.Delete(context.Background(), fileSha1); err != nil {
			slog.Error("rollback storage failed", "error", err, "filehash", fileSha1)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "文件上传失败", "data": nil})
		return
	}


	slog.Info("file uploaded", "filename", filename, "size", fileSize, "hash", fileSha1)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "上传成功", "data": gin.H{"filehash": fileSha1}})
}

// GetFileHandler 获取文件元信息
func GetFileHandler(c *gin.Context) {
	filehash := c.Query("filehash")
	if filehash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 filehash 参数", "data": nil})
		return
	}

	fMeta, err := meta.GetFileMetaDBByUser(filehash, c.GetString("username"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "msg": "文件不存在", "data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": fMeta})
}

// sanitizeFilename 清理文件名中的危险字符，防止 Content-Disposition 头注入
func sanitizeFilename(name string) string {
	// 移除双引号、回车、换行等可能破坏 HTTP 头的字符
	name = strings.ReplaceAll(name, `"`, "")
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	return name
}

// DownloadHandler 下载文件
func DownloadHandler(c *gin.Context) {
	filehash := c.Query("filehash")
	if filehash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 filehash 参数", "data": nil})
		return
	}

	fMeta, err := meta.GetFileMetaDBByUser(filehash, c.GetString("username"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "msg": "文件不存在", "data": nil})
		return
	}

	// 从存储层读取
	reader, err := globalStore.Get(context.Background(), fMeta.FileSha1)
	if err != nil {
		slog.Error("get file from storage failed", "error", err, "filehash", filehash)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "文件读取失败", "data": nil})
		return
	}
	defer reader.Close()

	// 使用 RFC 5987 编码，避免 Content-Disposition 头注入
	safeName := sanitizeFilename(fMeta.FileName)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, safeName, url.PathEscape(safeName)))
	c.Header("Content-Type", "application/octet-stream")

	buf := make([]byte, 32*1024)
	io.CopyBuffer(c.Writer, reader, buf)
}

// FileMetaUpdateHandler 更新元信息接口（重命名）
func FileMetaUpdateHandler(c *gin.Context) {
	opType := c.PostForm("op")
	fileSha1 := c.PostForm("filehash")
	username := c.GetString("username")

	if opType != "0" {
		c.JSON(http.StatusForbidden, gin.H{"code": 1, "msg": "不支持的操作", "data": nil})
		return
	}
	if fileSha1 == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少参数", "data": nil})
		return
	}

	newFileName := c.PostForm("filename")
	if newFileName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 filename 参数", "data": nil})
		return
	}

	// 验证文件所有权
	_, err := meta.GetFileMetaDBByUser(fileSha1, username)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"code": 1, "msg": "无权操作该文件", "data": nil})
		return
	}

	if ok := meta.UpdateFileMetaDBName(fileSha1, filepath.Base(newFileName)); !ok {
		slog.Error("update file meta failed", "filehash", fileSha1)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "更新失败", "data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功", "data": nil})
}

// FileDeleteHandler 删除文件及元信息（软删除）
func FileDeleteHandler(c *gin.Context) {
	fileSha1 := c.PostForm("filehash")
	if fileSha1 == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 filehash 参数", "data": nil})
		return
	}

	// 验证文件所有权
	_, err := meta.GetFileMetaDBByUser(fileSha1, c.GetString("username"))
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"code": 1, "msg": "无权操作该文件", "data": nil})
		return
	}

	// 软删除：更新数据库状态
	if ok := meta.DeleteFileMetaDB(fileSha1); !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "删除失败", "data": nil})
		return
	}

	slog.Info("file deleted", "filehash", fileSha1, "username", c.GetString("username"))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功", "data": nil})
}

// FileQueryHandler 返回所有文件元信息列表
func FileQueryHandler(c *gin.Context) {
	fileMetas, err := meta.GetAllFileMetaDBByUser(c.GetString("username"))
	if err != nil {
		slog.Error("query all files failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "查询失败", "data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": fileMetas})
}

// UploadChunkHandler 分块上传
func UploadChunkHandler(c *gin.Context) {
	r := c.Request
	r.Body = http.MaxBytesReader(c.Writer, r.Body, MaxUploadSize)

	fileHash := c.PostForm("filehash")
	index := c.PostForm("index")

	if fileHash == "" || index == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 filehash 或 index 参数", "data": nil})
		return
	}

	chunkIndex, err := strconv.Atoi(index)
	if err != nil || chunkIndex < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "无效的 chunk index", "data": nil})
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "分块文件获取失败", "data": nil})
		return
	}
	defer file.Close()

	// 已上传过该分块则直接返回（幂等）
	if util.ChunkExists(globalCfg.ChunkDir, fileHash, chunkIndex) {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "chunk already uploaded", "data": nil})
		return
	}

	dir := filepath.Join(globalCfg.ChunkDir, filepath.Base(fileHash))
	os.MkdirAll(dir, 0755)

	chunkPath := filepath.Join(dir, index)

	dst, err := os.Create(chunkPath)
	if err != nil {
		slog.Error("create chunk failed", "error", err, "filehash", fileHash, "index", chunkIndex)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "分块创建失败", "data": nil})
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
			slog.Error("write chunk failed", "error", err, "filehash", fileHash, "index", chunkIndex)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "分块写入失败", "data": nil})
			return
		}

	slog.Info("chunk uploaded", "filehash", fileHash, "index", chunkIndex)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "chunk upload success", "data": nil})
}

// UploadStatusHandler 断点续传状态查询
func UploadStatusHandler(c *gin.Context) {
	fileHash := c.Query("filehash")
	if fileHash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 filehash 参数", "data": nil})
		return
	}

	chunks, err := util.GetUploadedChunks(globalCfg.ChunkDir, fileHash)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": []string{}})
		return
	}

	sort.Slice(chunks, func(i, j int) bool {
		ii, _ := strconv.Atoi(chunks[i])
		jj, _ := strconv.Atoi(chunks[j])
		return ii < jj
	})

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": chunks})
}

// MergeChunkHandler 分块合并
func MergeChunkHandler(c *gin.Context) {
	fileHash, fileName, totalStr := c.PostForm("filehash"), c.PostForm("filename"), c.PostForm("chunks")

	if fileHash == "" || fileName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 filehash 或 filename 参数", "data": nil})
		return
	}

	// 路径穿越防护
	fileName = filepath.Base(fileName)

	chunkDir := filepath.Join(globalCfg.ChunkDir, filepath.Base(fileHash))

	files, err := readChunkDir(chunkDir, totalStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error(), "data": nil})
		return
	}

	// 合并分片到临时文件
	tmpPath := filepath.Join(globalCfg.ChunkDir, fileHash+".tmp")
	totalSize, err := mergeChunksToTemp(chunkDir, files, tmpPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": err.Error(), "data": nil})
		return
	}

	// 上传到存储层
	if err := saveMergedFile(fileHash, tmpPath, totalSize); err != nil {
		slog.Error("store merged file failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": err.Error(), "data": nil})
		return
	}

	loc, _ := time.LoadLocation("Asia/Shanghai")

	fileMeta := meta.FileMeta{
		FileName: fileName,
		Location: fileHash,
		Username: c.GetString("username"),
		UploadAt: time.Now().In(loc),
		FileSha1: fileHash,
		FileSize: totalSize,
	}

	if ok := meta.UpdateFileMetaDB(fileMeta); !ok {
		slog.Warn("save merged file meta failed, rolling back storage", "filehash", fileHash)
		if err := globalStore.Delete(context.Background(), fileHash); err != nil {
			slog.Error("rollback merged storage failed", "error", err, "filehash", fileHash)
		}
		os.RemoveAll(chunkDir)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "文件合并失败", "data": nil})
		return
	}

	slog.Info("chunks merged", "filehash", fileHash, "filename", fileName, "size", totalSize)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "merge success", "data": nil})
}

// readChunkDir 读取并排序 chunk 文件，校验分块数量
func readChunkDir(chunkDir, totalStr string) ([]os.DirEntry, error) {
	files, err := os.ReadDir(chunkDir)
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("分块不存在")
	}

	// 按 chunk 序号排序
	sort.Slice(files, func(i, j int) bool {
		iIndex, _ := strconv.Atoi(files[i].Name())
		jIndex, _ := strconv.Atoi(files[j].Name())
		return iIndex < jIndex
	})

	// 校验分块数量
	if totalStr != "" {
		if total, err := strconv.Atoi(totalStr); err == nil && total > 0 {
			if len(files) != total {
				return nil, fmt.Errorf("分块数量不匹配: 期望 %d, 实际 %d", total, len(files))
			}
		}
	}

	return files, nil
}

// mergeChunksToTemp 将分片合并到临时文件，返回总大小
func mergeChunksToTemp(chunkDir string, files []os.DirEntry, tmpPath string) (int64, error) {
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return 0, fmt.Errorf("合并文件创建失败")
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
func saveMergedFile(fileHash, tmpPath string, totalSize int64) error {
	tmpReader, err := os.Open(tmpPath)
	if err != nil {
		slog.Error("open merged tmp file failed", "error", err)
		os.Remove(tmpPath)
		return fmt.Errorf("合并文件读取失败")
	}
	defer tmpReader.Close()
	defer os.Remove(tmpPath)

	return globalStore.Put(context.Background(), fileHash, tmpReader, totalSize)
}