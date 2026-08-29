package handler

import (
	"bytes"
	"errors"
	"fmt"
	"gofile/internal/application/service"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// sanitizeFilename 清理文件名中的危险字符
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, `"`, "")
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	return name
}

// buildContentDisposition 构建安全的 Content-Disposition 响应头
func buildContentDisposition(name string) string {
	return `attachment; filename="` + name + `"; filename*=UTF-8''` + url.PathEscape(name)
}

// parseRangeHeader 解析 Range 头，返回 offset 和 length
// totalSize=0 表示还不知道总大小，会先查 storage 获取
func parseRangeHeader(rangeHeader string, totalSize int64) (offset, length int64, ok bool) {
	// 格式：bytes=start-end 或 bytes=start-
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return 0, 0, false
	}
	rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
	parts := strings.SplitN(rangeSpec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}

	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 {
		return 0, 0, false
	}

	if parts[1] == "" {
		// bytes=100-  → 从 100 到末尾；length 未知时返回 -1，由 service 按文件大小补齐
		offset = start
		length = -1
		if totalSize > 0 {
			length = totalSize - start
		}
		if length == 0 {
			return 0, 0, false
		}
	} else {
		end, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end < start {
			return 0, 0, false
		}
		offset = start
		length = end - start + 1
		// 空区间或溢出（end-start 达到 MaxInt64 时 +1 变负）均非法
		if length <= 0 {
			return 0, 0, false
		}
	}

	// length == 0 表示空区间（非法）；-1 表示开放区间未知大小，放行由 service 补齐
	if length == 0 {
		return 0, 0, false
	}
	return offset, length, true
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

// DownloadHandler 下载文件（支持 HTTP Range 断点续传）
// @Summary 下载文件
// @Description 支持 Range 请求头实现断点续传和拖动播放
// @Tags 文件
// @Produce application/octet-stream
// @Security ApiKeyAuth
// @Param filehash query string true "文件 SHA1"
// @Param range header string false "Range 请求头（如 bytes=0-1023）"
// @Success 200 "完整文件内容"
// @Success 206 "区间响应"
// @Failure 404 "文件不存在"
// @Router /file/download [get]
func (h *FileHandler) DownloadHandler(c *gin.Context) {
	filehash := c.Query("filehash")
	if filehash == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filehash 参数")
		return
	}

	username := c.GetString("username")

	// 获取文件元信息（用于文件名和 Content-Type）
	fMeta, err := h.fileSvc.GetMeta(c.Request.Context(), filehash, username)
	if err != nil {
		respondError(c, http.StatusNotFound, CodeNotFound, "文件不存在")
		return
	}

	// 解析 Range 请求头
	rangeHeader := c.GetHeader("Range")
	if rangeHeader == "" {
		// 无 Range 头：返回完整文件
		reader, _, err := h.fileSvc.Download(c.Request.Context(), filehash, username)
		if err != nil {
			slog.ErrorContext(c.Request.Context(), "download failed", "error", err, "filehash", filehash)
			respondError(c, http.StatusNotFound, CodeNotFound, "文件不存在")
			return
		}
		defer reader.Close()

		safeName := sanitizeFilename(fMeta.FileName)
		c.Header("Content-Disposition", buildContentDisposition(safeName))
		c.Header("Content-Type", "application/octet-stream")
		c.Header("Accept-Ranges", "bytes")

		buf := make([]byte, 32*1024)
		io.CopyBuffer(c.Writer, reader, buf)
		return
	}

	// 有 Range 头：解析范围（开放区间 bytes=a- 由 service 按文件大小补齐）
	offset, length, ok := parseRangeHeader(rangeHeader, 0)
	if !ok {
		if size, err := h.fileSvc.FileSize(c.Request.Context(), filehash, username); err == nil {
			c.Header("Content-Range", fmt.Sprintf("bytes */%d", size))
		}
		respondError(c, http.StatusRequestedRangeNotSatisfiable, CodeInvalidParams, "无效的 Range 范围")
		return
	}

	reader, _, totalSize, actualLen, err := h.fileSvc.DownloadRange(c.Request.Context(), filehash, username, offset, length)
	if err != nil {
		if errors.Is(err, service.ErrRangeOutOfBounds) {
			c.Header("Content-Range", fmt.Sprintf("bytes */%d", totalSize))
			respondError(c, http.StatusRequestedRangeNotSatisfiable, CodeInvalidParams, "Range 越界")
			return
		}
		slog.ErrorContext(c.Request.Context(), "download range failed", "error", err, "filehash", filehash)
		respondError(c, http.StatusNotFound, CodeNotFound, "文件不存在")
		return
	}
	defer reader.Close()

	safeName := sanitizeFilename(fMeta.FileName)
	c.Header("Content-Disposition", buildContentDisposition(safeName))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, offset+actualLen-1, totalSize))
	c.Header("Content-Length", fmt.Sprintf("%d", actualLen))

	c.Status(http.StatusPartialContent) // 206

	buf := make([]byte, 32*1024)
	io.CopyBuffer(c.Writer, reader, buf)
}

// PreviewHandler 在线预览文件（图片/PDF/文本/视频等，支持 Range 拖动）
func (h *FileHandler) PreviewHandler(c *gin.Context) {
	filehash := c.Query("filehash")
	if filehash == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filehash 参数")
		return
	}

	username := c.GetString("username")

	// 获取文件元信息（用于文件名和 Content-Type）
	fMeta, err := h.fileSvc.GetMeta(c.Request.Context(), filehash, username)
	if err != nil {
		respondError(c, http.StatusNotFound, CodeNotFound, "文件不存在")
		return
	}

	// 根据扩展名检测 Content-Type
	ext := strings.ToLower(filepath.Ext(fMeta.FileName))
	contentType := detectMimeType(ext)
	safeName := sanitizeFilename(fMeta.FileName)

	// Range 请求：206 区间响应（视频/音频拖动播放依赖）
	if rangeHeader := c.GetHeader("Range"); rangeHeader != "" {
		offset, length, ok := parseRangeHeader(rangeHeader, 0)
		if !ok {
			if size, err := h.fileSvc.FileSize(c.Request.Context(), filehash, username); err == nil {
				c.Header("Content-Range", fmt.Sprintf("bytes */%d", size))
			}
			respondError(c, http.StatusRequestedRangeNotSatisfiable, CodeInvalidParams, "无效的 Range 范围")
			return
		}

		reader, _, totalSize, actualLen, err := h.fileSvc.DownloadRange(c.Request.Context(), filehash, username, offset, length)
		if err != nil {
			if errors.Is(err, service.ErrRangeOutOfBounds) {
				c.Header("Content-Range", fmt.Sprintf("bytes */%d", totalSize))
				respondError(c, http.StatusRequestedRangeNotSatisfiable, CodeInvalidParams, "Range 越界")
				return
			}
			slog.ErrorContext(c.Request.Context(), "preview range failed", "error", err, "filehash", filehash)
			respondError(c, http.StatusNotFound, CodeNotFound, "文件不存在")
			return
		}
		defer reader.Close()

		c.Header("Content-Disposition", "inline; filename=\""+safeName+"\"")
		c.Header("Content-Type", contentType)
		c.Header("Accept-Ranges", "bytes")
		c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, offset+actualLen-1, totalSize))
		c.Header("Content-Length", fmt.Sprintf("%d", actualLen))
		c.Header("X-Content-Type-Options", "nosniff")
		c.Status(http.StatusPartialContent) // 206

		io.CopyBuffer(c.Writer, reader, make([]byte, 32*1024))
		return
	}

	// 无 Range 头：返回完整文件
	reader, _, err := h.fileSvc.Download(c.Request.Context(), filehash, username)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "preview failed", "error", err, "filehash", filehash)
		respondError(c, http.StatusNotFound, CodeNotFound, "文件不存在")
		return
	}
	defer reader.Close()

	c.Header("Content-Disposition", "inline; filename=\""+safeName+"\"")
	c.Header("Content-Type", contentType)
	c.Header("Accept-Ranges", "bytes")
	c.Header("X-Content-Type-Options", "nosniff")

	// 扩展名无法识别时，读取文件头探测真实类型
	if contentType == "application/octet-stream" {
		buf := make([]byte, 512)
		n, _ := reader.Read(buf)
		if n > 0 {
			contentType = http.DetectContentType(buf[:n])
			c.Header("Content-Type", contentType)
		}
		// 重新组合读取器
		combinedReader := io.MultiReader(bytes.NewReader(buf[:n]), reader)
		io.CopyBuffer(c.Writer, combinedReader, make([]byte, 32*1024))
		return
	}

	io.CopyBuffer(c.Writer, reader, make([]byte, 32*1024))
}
