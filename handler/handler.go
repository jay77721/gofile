package handler

import (
	"bytes"
	"context"
	"gofile/config"
	"gofile/service"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	MaxUploadSize = 100 << 20 // 100MB
)

// sha1HashPattern 40 位小写 hex（SHA1 文件名/目录安全校验）
var sha1HashPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

// isValidHash 校验是否为合法的 40 位 SHA1 hex，防止路径穿越
func isValidHash(hash string) bool {
	return sha1HashPattern.MatchString(hash)
}

// FileHandler 文件 HTTP 处理器，依赖注入 FileService
type FileHandler struct {
	fileSvc *service.FileService
	cfg     *config.Config
}

// NewFileHandler 创建文件处理器
func NewFileHandler(fileSvc *service.FileService, cfg *config.Config) *FileHandler {
	return &FileHandler{fileSvc: fileSvc, cfg: cfg}
}

// UploadHandler 处理文件上传
func (h *FileHandler) UploadHandler(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxUploadSize)

	fileHash := c.PostForm("filehash")
	// 秒传检测
	if fileHash != "" {
		exists, err := h.fileSvc.FastUpload(context.Background(), fileHash)
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

	// 路径穿越防护
	filename := filepath.Base(header.Filename)

	fMeta, err := h.fileSvc.Upload(context.Background(), file, filename, 0, c.GetString("username"))
	if err != nil {
		slog.Error("upload failed", "error", err, "filename", filename)
		// 秒传成功的情况
		if fMeta.FileSha1 != "" && strings.Contains(err.Error(), "save file meta") {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "秒传成功", "data": gin.H{"filehash": fMeta.FileSha1}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "文件上传失败", "data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "上传成功", "data": gin.H{"filehash": fMeta.FileSha1}})
}

// GetFileHandler 获取文件元信息
func (h *FileHandler) GetFileHandler(c *gin.Context) {
	filehash := c.Query("filehash")
	if filehash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 filehash 参数", "data": nil})
		return
	}

	fMeta, err := h.fileSvc.GetMeta(filehash, c.GetString("username"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "msg": "文件不存在", "data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": fMeta})
}

// sanitizeFilename 清理文件名中的危险字符
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, `"`, "")
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	return name
}

// DownloadHandler 下载文件
func (h *FileHandler) DownloadHandler(c *gin.Context) {
	filehash := c.Query("filehash")
	if filehash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 filehash 参数", "data": nil})
		return
	}

	reader, fMeta, err := h.fileSvc.Download(context.Background(), filehash, c.GetString("username"))
	if err != nil {
		slog.Error("download failed", "error", err, "filehash", filehash)
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "msg": "文件不存在", "data": nil})
		return
	}
	defer reader.Close()

	// 使用 RFC 5987 编码，避免 Content-Disposition 头注入
	safeName := sanitizeFilename(fMeta.FileName)
	c.Header("Content-Disposition", buildContentDisposition(safeName))
	c.Header("Content-Type", "application/octet-stream")

	buf := make([]byte, 32*1024)
	io.CopyBuffer(c.Writer, reader, buf)
}

func buildContentDisposition(name string) string {
	return `attachment; filename="` + name + `"; filename*=UTF-8''` + url.PathEscape(name)
}

// PreviewHandler 在线预览文件（图片/PDF/文本/视频等）
func (h *FileHandler) PreviewHandler(c *gin.Context) {
	filehash := c.Query("filehash")
	if filehash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 filehash 参数", "data": nil})
		return
	}

	reader, fMeta, err := h.fileSvc.Download(context.Background(), filehash, c.GetString("username"))
	if err != nil {
		slog.Error("preview failed", "error", err, "filehash", filehash)
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "msg": "文件不存在", "data": nil})
		return
	}
	defer reader.Close()

	// 根据扩展名检测 Content-Type
	ext := strings.ToLower(filepath.Ext(fMeta.FileName))
	contentType := detectMimeType(ext)
	if contentType == "application/octet-stream" {
		// 尝试读取文件头检测
		buf := make([]byte, 512)
		n, _ := reader.Read(buf)
		if n > 0 {
			contentType = http.DetectContentType(buf[:n])
		}
		// 重新组合读取器
		combinedReader := io.MultiReader(bytes.NewReader(buf[:n]), reader)
		// 写文件内容到响应
		io.CopyBuffer(c.Writer, combinedReader, make([]byte, 32*1024))
		return
	}

	safeName := sanitizeFilename(fMeta.FileName)
	c.Header("Content-Disposition", "inline; filename=\""+safeName+"\"")
	c.Header("Content-Type", contentType)
	c.Header("X-Content-Type-Options", "nosniff")

	buf := make([]byte, 32*1024)
	io.CopyBuffer(c.Writer, reader, buf)
}

// detectMimeType 根据文件扩展名返回 MIME 类型
func detectMimeType(ext string) string {
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".bmp":
		return "image/bmp"
	case ".ico":
		return "image/x-icon"
	case ".pdf":
		return "application/pdf"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mov", ".qt":
		return "video/quicktime"
	case ".avi":
		return "video/x-msvideo"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".ogg", ".oga":
		return "audio/ogg"
	case ".flac":
		return "audio/flac"
	case ".txt", ".md", ".log":
		return "text/plain; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".xml":
		return "application/xml; charset=utf-8"
	case ".yaml", ".yml":
		return "text/plain; charset=utf-8"
	case ".go", ".js", ".ts", ".jsx", ".tsx", ".py", ".css", ".html", ".htm":
		return "text/plain; charset=utf-8"
	case ".sh", ".bash", ".zsh":
		return "text/plain; charset=utf-8"
	case ".bat", ".cmd":
		return "text/plain; charset=utf-8"
	case ".env", ".conf", ".ini", ".toml":
		return "text/plain; charset=utf-8"
	case ".sql":
		return "text/plain; charset=utf-8"
	case ".java", ".rb", ".php", ".rs", ".swift", ".kt", ".scala":
		return "text/plain; charset=utf-8"
	case ".csv":
		return "text/csv; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

// FileMetaUpdateHandler 更新元信息接口（重命名）
func (h *FileHandler) FileMetaUpdateHandler(c *gin.Context) {
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

	if err := h.fileSvc.Rename(fileSha1, username, filepath.Base(newFileName)); err != nil {
		slog.Error("rename failed", "error", err, "filehash", fileSha1)
		c.JSON(http.StatusForbidden, gin.H{"code": 1, "msg": "无权操作该文件", "data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功", "data": nil})
}

// FileDeleteHandler 删除文件及元信息（软删除）
func (h *FileHandler) FileDeleteHandler(c *gin.Context) {
	fileSha1 := c.PostForm("filehash")
	if fileSha1 == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 filehash 参数", "data": nil})
		return
	}

	if err := h.fileSvc.Delete(fileSha1, c.GetString("username")); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"code": 1, "msg": "无权操作该文件", "data": nil})
		return
	}

	slog.Info("file deleted", "filehash", fileSha1, "username", c.GetString("username"))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功", "data": nil})
}

// FileQueryHandler 返回所有文件元信息列表
func (h *FileHandler) FileQueryHandler(c *gin.Context) {
	fileMetas, err := h.fileSvc.ListByUser(c.GetString("username"))
	if err != nil {
		slog.Error("query all files failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "查询失败", "data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": fileMetas})
}

// UploadChunkHandler 分块上传（用户隔离）
func (h *FileHandler) UploadChunkHandler(c *gin.Context) {
	r := c.Request
	r.Body = http.MaxBytesReader(c.Writer, r.Body, MaxUploadSize)

	fileHash := c.PostForm("filehash")
	index := c.PostForm("index")

	if fileHash == "" || index == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 filehash 或 index 参数", "data": nil})
		return
	}

	// 校验 hash 格式，防止路径穿越
	if !isValidHash(fileHash) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "无效的 filehash 格式", "data": nil})
		return
	}

	chunkIndex, err := parseInt(index)
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

	username := c.GetString("username")
	if err := h.fileSvc.UploadChunk(fileHash, chunkIndex, file, username); err != nil {
		slog.Error("upload chunk failed", "error", err, "filehash", fileHash, "index", chunkIndex, "username", username)
		// 已上传的情况不算错误
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "chunk already uploaded", "data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "chunk upload success", "data": nil})
}

// UploadStatusHandler 断点续传状态查询（用户隔离）
func (h *FileHandler) UploadStatusHandler(c *gin.Context) {
	fileHash := c.Query("filehash")
	if fileHash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 filehash 参数", "data": nil})
		return
	}

	// 校验 hash 格式，防止路径穿越
	if !isValidHash(fileHash) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "无效的 filehash 格式", "data": nil})
		return
	}

	chunks, err := h.fileSvc.GetChunkStatus(fileHash, c.GetString("username"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": []string{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": chunks})
}

// MergeChunkHandler 分块合并
func (h *FileHandler) MergeChunkHandler(c *gin.Context) {
	fileHash := c.PostForm("filehash")
	fileName := c.PostForm("filename")
	totalStr := c.PostForm("chunks")

	if fileHash == "" || fileName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 filehash 或 filename 参数", "data": nil})
		return
	}

	// 校验 hash 格式，防止路径穿越
	if !isValidHash(fileHash) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "无效的 filehash 格式", "data": nil})
		return
	}

	// 路径穿越防护
	fileName = filepath.Base(fileName)

	fMeta, err := h.fileSvc.MergeChunks(context.Background(), fileHash, fileName, c.GetString("username"), totalStr)
	if err != nil {
		slog.Error("merge chunks failed", "error", err, "filehash", fileHash)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "文件合并失败", "data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "merge success", "data": gin.H{"filehash": fMeta.FileSha1}})
}

// 辅助函数
func parseInt(s string) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, strconv.ErrSyntax
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// HealthCheckHandler 健康检查端点（纯函数，不需要注入）
func HealthCheckHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": nil})
}

// PresignUploadHandler 获取预签名上传 URL
func (h *FileHandler) PresignUploadHandler(c *gin.Context) {
	fileHash := c.PostForm("filehash")
	fileName := c.PostForm("filename")

	if fileHash == "" || fileName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 filehash 或 filename 参数", "data": nil})
		return
	}

	if !isValidHash(fileHash) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "无效的 filehash 格式", "data": nil})
		return
	}

	uploadURL, err := h.fileSvc.PresignUpload(context.Background(), fileHash, c.GetString("username"))
	if err != nil {
		if err.Error() == "file already exists" {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "秒传成功", "data": gin.H{"filehash": fileHash}})
			return
		}
		slog.Error("presign upload failed", "error", err, "filehash", fileHash)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "生成上传链接失败", "data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": gin.H{
		"upload_url": uploadURL,
		"filehash":   fileHash,
	}})
}

// ConfirmUploadHandler 确认预签名上传完成
func (h *FileHandler) ConfirmUploadHandler(c *gin.Context) {
	fileHash := c.PostForm("filehash")
	fileName := c.PostForm("filename")

	if fileHash == "" || fileName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 filehash 或 filename 参数", "data": nil})
		return
	}

	if !isValidHash(fileHash) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "无效的 filehash 格式", "data": nil})
		return
	}

	if err := h.fileSvc.ConfirmUpload(context.Background(), fileHash, fileName, c.GetString("username")); err != nil {
		slog.Error("confirm upload failed", "error", err, "filehash", fileHash)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "确认上传失败", "data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "上传成功", "data": gin.H{"filehash": fileHash}})
}

// PresignDownloadHandler 获取预签名下载 URL
func (h *FileHandler) PresignDownloadHandler(c *gin.Context) {
	fileHash := c.Query("filehash")
	if fileHash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 filehash 参数", "data": nil})
		return
	}

	if !isValidHash(fileHash) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "无效的 filehash 格式", "data": nil})
		return
	}

	downloadURL, err := h.fileSvc.PresignDownload(context.Background(), fileHash, c.GetString("username"))
	if err != nil {
		slog.Error("presign download failed", "error", err, "filehash", fileHash)
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "msg": "文件不存在", "data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": gin.H{
		"download_url": downloadURL,
	}})
}