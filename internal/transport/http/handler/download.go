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

// sanitizeFilename remove dangerous characters from the filename
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, `"`, "")
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	return name
}

// buildContentDisposition build a safe Content-Disposition header
func buildContentDisposition(name string) string {
	return `attachment; filename="` + name + `"; filename*=UTF-8''` + url.PathEscape(name)
}

// parseRangeHeader parse the Range header and return offset and length
// totalSize=0 means total size is unknown; storage will be queried first
func parseRangeHeader(rangeHeader string, totalSize int64) (offset, length int64, ok bool) {
	// Format: bytes=start-end or bytes=start-
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
		// bytes=100-  → from 100 to end; returns -1 when length is unknown and service fills it by file size
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
		// Empty interval or overflow (end-start reaches MaxInt64 and +1 becomes negative) is invalid
		if length <= 0 {
			return 0, 0, false
		}
	}

	// length == 0 denotes empty interval (invalid); -1 denotes open interval with unknown size, handled by service
	if length == 0 {
		return 0, 0, false
	}
	return offset, length, true
}

// detectMimeType return MIME type based on file extension
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

// DownloadHandler download a file (supports HTTP Range resumable download)
// @Summary Download file
// @Description Supports Range header for resumable download and seek playback
// @Tags File
// @Produce application/octet-stream
// @Security ApiKeyAuth
// @Param filehash query string true "File SHA1"
// @Param range header string false "Range header (e.g. bytes=0-1023)"
// @Success 200 "Full file content"
// @Success 206 "Partial content"
// @Failure 404 "File not found"
// @Router /file/download [get]
func (h *FileHandler) DownloadHandler(c *gin.Context) {
	filehash := c.Query("filehash")
	if filehash == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filehash 参数")
		return
	}

	username := c.GetString("username")

	// Retrieve file metadata (for filename and Content-Type)
	fMeta, err := h.fileSvc.GetMeta(c.Request.Context(), filehash, username)
	if err != nil {
		respondError(c, http.StatusNotFound, CodeNotFound, "文件不存在")
		return
	}

	// Parse Range header
	rangeHeader := c.GetHeader("Range")
	if rangeHeader == "" {
		// No Range header: return the whole file
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

	// Range header present: parse range (open interval bytes=a- is filled by service with file size)
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

// PreviewHandler preview a file inline (images/PDF/text/video etc., supports Range seeking)
func (h *FileHandler) PreviewHandler(c *gin.Context) {
	filehash := c.Query("filehash")
	if filehash == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filehash 参数")
		return
	}

	username := c.GetString("username")

	// Retrieve file metadata (for filename and Content-Type)
	fMeta, err := h.fileSvc.GetMeta(c.Request.Context(), filehash, username)
	if err != nil {
		respondError(c, http.StatusNotFound, CodeNotFound, "文件不存在")
		return
	}

	// Detect Content-Type by extension
	ext := strings.ToLower(filepath.Ext(fMeta.FileName))
	contentType := detectMimeType(ext)
	safeName := sanitizeFilename(fMeta.FileName)

	// Range request: 206 partial response (required for video/audio seeking)
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

	// No Range header: return the whole file
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

	// When extension is unrecognized, sniff file header to detect real type
	if contentType == "application/octet-stream" {
		buf := make([]byte, 512)
		n, _ := reader.Read(buf)
		if n > 0 {
			contentType = http.DetectContentType(buf[:n])
			c.Header("Content-Type", contentType)
		}
		// Reassemble the reader
		combinedReader := io.MultiReader(bytes.NewReader(buf[:n]), reader)
		io.CopyBuffer(c.Writer, combinedReader, make([]byte, 32*1024))
		return
	}

	io.CopyBuffer(c.Writer, reader, make([]byte, 32*1024))
}
