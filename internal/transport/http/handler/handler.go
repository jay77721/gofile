package handler

import (
	"errors"
	"gofile/internal/application/service"
	"gofile/internal/config"
	"gofile/internal/infrastructure/storage"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	MaxUploadSize = 100 << 20 // 100MB
)

// Dangerous extension blocklist (stored XSS / malicious file distribution / executables)
// Rejected directly on upload without entering storage layer
var dangerousExts = map[string]bool{
	// HTML / scripts (primary vectors for stored XSS)
	".html": true, ".htm": true, ".xhtml": true,
	".js": true, ".mjs": true, ".cjs": true,
	// Windows executables
	".exe": true, ".com": true, ".bat": true, ".cmd": true, ".ps1": true, ".msi": true, ".scr": true, ".pif": true,
	// Unix/Linux executables/scripts
	".sh": true, ".bash": true, ".zsh": true, ".csh": true, ".ksh": true,
	".bin": true, ".run": true, ".appimage": true,
	// Server-side scripts (prevent execution in supported environments)
	".php": true, ".jsp": true, ".asp": true, ".aspx": true, ".cgi": true,
	// Scripting languages
	".py": true, ".pl": true, ".rb": true, ".lua": true,
	// Other dangerous types
	".svg": true, // Can inline JavaScript; requires special handling during preview
	".jar": true, // Java executable
	".war": true, ".ear": true,
	".ps":  true, // PowerShell script
	".vbs": true, ".vbe": true, ".wsf": true, ".wsh": true,
	".reg": true, // Windows registry file
	".inf": true,
}

// isDangerousExtension check whether the extension is blocklisted
func isDangerousExtension(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return dangerousExts[ext]
}

// sha1HashPattern 40-char lowercase hex (safety check for SHA1 filename/directory)
var sha1HashPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

// isValidHash validate a 40-char SHA1 hex to prevent path traversal
func isValidHash(hash string) bool {
	return sha1HashPattern.MatchString(hash)
}

// FileHandler file HTTP handler, injected with FileService
type FileHandler struct {
	fileSvc *service.FileService
	cfg     *config.Config
}

// NewFileHandler create a file handler
func NewFileHandler(fileSvc *service.FileService, cfg *config.Config) *FileHandler {
	return &FileHandler{fileSvc: fileSvc, cfg: cfg}
}

// UploadHandler handle file upload
// @Summary Upload file
// @Description Supports instant upload deduplication (via filehash param); dangerous file types (.html/.js/.exe etc.) will be rejected
// @Tags File
// @Accept multipart/form-data
// @Produce json
// @Security ApiKeyAuth
// @Param file formData file true "File"
// @Param filehash formData string false "File SHA1 (instant upload check)"
// @Success 200 {object} map[string]any{code=int,msg=string,data=object{filehash=string}} "Upload succeeded"
// @Failure 400 {object} map[string]any{code=int,msg=string,data=nil} "Invalid params or file type not allowed"
// @Router /file/upload [post]
func (h *FileHandler) UploadHandler(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxUploadSize)

	fileHash := c.PostForm("filehash")
	// Instant upload check (on hit, also establishes ownership for the current user)
	if fileHash != "" {
		exists, err := h.fileSvc.FastUpload(c.Request.Context(), fileHash, c.GetString("username"))
		if err != nil {
			slog.WarnContext(c.Request.Context(), "upload: fast upload check failed", "error", err, "filehash", fileHash)
		} else if exists {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "秒传成功", "data": nil})
			return
		}
	}

	// Parse the uploaded file
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "文件获取失败")
		return
	}
	defer file.Close()

	// Path traversal protection
	filename := filepath.Base(header.Filename)

	// Dangerous file type blocklist (prevent stored XSS / malicious distribution)
	if isDangerousExtension(filename) {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "该文件类型不允许上传")
		return
	}

	fMeta, err := h.fileSvc.Upload(c.Request.Context(), file, filename, 0, c.GetString("username"))
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "upload failed", "error", err, "filename", filename)
		respondError(c, http.StatusInternalServerError, CodeUploadFailed, "文件上传失败")
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "上传成功", "data": gin.H{"filehash": fMeta.FileSha1}})
}

// FastUploadHandler handle the dedicated instant upload check endpoint
func (h *FileHandler) FastUploadHandler(c *gin.Context) {
	fileHash := c.PostForm("filehash")
	if fileHash == "" {
		fileHash = c.Query("filehash")
	}
	if fileHash == "" || !isValidHash(fileHash) {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少有效 filehash 参数")
		return
	}

	exists, err := h.fileSvc.FastUpload(c.Request.Context(), fileHash, c.GetString("username"))
	if err != nil {
		slog.WarnContext(c.Request.Context(), "fast upload check failed", "error", err, "filehash", fileHash)
		respondError(c, http.StatusInternalServerError, CodeInternalError, "秒传检测失败")
		return
	}
	if exists {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "秒传成功", "data": gin.H{"filehash": fileHash}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "文件不存在，需完整上传", "data": nil})
}

// PresignUploadHandler retrieve a presigned upload URL
func (h *FileHandler) PresignUploadHandler(c *gin.Context) {
	fileHash := c.PostForm("filehash")
	fileName := c.PostForm("filename")

	if fileHash == "" || fileName == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filehash 或 filename 参数")
		return
	}

	if !isValidHash(fileHash) {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "无效的 filehash 格式")
		return
	}

	uploadURL, err := h.fileSvc.PresignUpload(c.Request.Context(), fileHash, c.GetString("username"))
	if err != nil {
		if err.Error() == "file already exists" {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "秒传成功", "data": gin.H{"filehash": fileHash}})
			return
		}
		if errors.Is(err, storage.ErrPresignNotSupported) {
			respondError(c, http.StatusBadRequest, CodeStorageError, "预签名上传仅支持 MinIO 存储，当前为本地存储")
			return
		}
		slog.ErrorContext(c.Request.Context(), "presign upload failed", "error", err, "filehash", fileHash)
		respondError(c, http.StatusInternalServerError, CodeStorageError, "生成上传链接失败")
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": gin.H{
		"upload_url": uploadURL,
		"filehash":   fileHash,
	}})
}

// ConfirmUploadHandler confirm presigned upload completion
func (h *FileHandler) ConfirmUploadHandler(c *gin.Context) {
	fileHash := c.PostForm("filehash")
	fileName := c.PostForm("filename")

	if fileHash == "" || fileName == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filehash 或 filename 参数")
		return
	}

	if !isValidHash(fileHash) {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "无效的 filehash 格式")
		return
	}

	// Dangerous file type blocklist (prevent stored XSS / malicious distribution)
	if isDangerousExtension(fileName) {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "该文件类型不允许上传")
		return
	}

	if err := h.fileSvc.ConfirmUpload(c.Request.Context(), fileHash, fileName, c.GetString("username")); err != nil {
		slog.ErrorContext(c.Request.Context(), "confirm upload failed", "error", err, "filehash", fileHash)
		respondError(c, http.StatusInternalServerError, CodeUploadFailed, "确认上传失败")
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "上传成功", "data": gin.H{"filehash": fileHash}})
}

// PresignDownloadHandler retrieve a presigned download URL
func (h *FileHandler) PresignDownloadHandler(c *gin.Context) {
	fileHash := c.Query("filehash")
	if fileHash == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filehash 参数")
		return
	}

	if !isValidHash(fileHash) {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "无效的 filehash 格式")
		return
	}

	downloadURL, err := h.fileSvc.PresignDownload(c.Request.Context(), fileHash, c.GetString("username"))
	if err != nil {
		if errors.Is(err, storage.ErrPresignNotSupported) {
			respondError(c, http.StatusBadRequest, CodeStorageError, "预签名下载仅支持 MinIO 存储，当前为本地存储")
			return
		}
		slog.ErrorContext(c.Request.Context(), "presign download failed", "error", err, "filehash", fileHash)
		respondError(c, http.StatusNotFound, CodeNotFound, "文件不存在")
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": gin.H{
		"download_url": downloadURL,
	}})
}

// MetaHandler retrieve file metadata
func (h *FileHandler) MetaHandler(c *gin.Context) {
	filehash := c.Query("filehash")
	if filehash == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filehash 参数")
		return
	}

	fMeta, err := h.fileSvc.GetMeta(c.Request.Context(), filehash, c.GetString("username"))
	if err != nil {
		respondError(c, http.StatusNotFound, CodeNotFound, "文件不存在")
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": fMeta})
}

// GetFileHandler compatibility alias
func (h *FileHandler) GetFileHandler(c *gin.Context) {
	h.MetaHandler(c)
}

// QueryHandler return the user file list (supports pagination and directory tree query)
// @Summary List files
// @Description Supports pagination; returns all files when no pagination params are given
// @Tags File
// @Produce json
// @Security ApiKeyAuth
// @Param page query int false "Page number (starting from 1)"
// @Param size query int false "Page size (1-100, default 20)"
// @Success 200 {object} map[string]any{code=int,msg=string,data=object{list=array,total=int,page=int,size=int}} "File list"
// @Router /file/query [get]
func (h *FileHandler) QueryHandler(c *gin.Context) {
	username := c.GetString("username")

	parentIDStr := c.Query("parent_id")
	pageStr := c.Query("page")
	sizeStr := c.Query("size")

	// If parent_id is provided, query by directory hierarchy with breadcrumbs
	if parentIDStr != "" {
		parentID, _ := strconv.ParseUint(parentIDStr, 10, 64)
		page, _ := strconv.Atoi(pageStr)
		size, _ := strconv.Atoi(sizeStr)
		if page < 1 {
			page = 1
		}
		if size < 1 || size > 100 {
			size = 50
		}
		offset := (page - 1) * size
		files, total, crumbs, err := h.fileSvc.QueryDirectory(c.Request.Context(), username, parentID, offset, size)
		if err != nil {
			slog.ErrorContext(c.Request.Context(), "query directory failed", "error", err)
			respondError(c, http.StatusInternalServerError, CodeInternalError, "查询目录失败")
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": gin.H{
			"list":        files,
			"total":       total,
			"page":        page,
			"size":        size,
			"breadcrumbs": crumbs,
		}})
		return
	}

	if pageStr == "" && sizeStr == "" {
		// No pagination params: return all (compatible with legacy logic)
		fileMetas, err := h.fileSvc.ListByUser(c.Request.Context(), username)
		if err != nil {
			slog.ErrorContext(c.Request.Context(), "query all files failed", "error", err)
			respondError(c, http.StatusInternalServerError, CodeInternalError, "查询失败")
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": fileMetas})
		return
	}

	// Pagination params present: use paged logic
	page, _ := strconv.Atoi(pageStr)
	size, _ := strconv.Atoi(sizeStr)
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	files, total, err := h.fileSvc.ListByUserPaged(c.Request.Context(), username, page, size)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "paged query files failed", "error", err)
		respondError(c, http.StatusInternalServerError, CodeInternalError, "查询失败")
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": gin.H{
		"list":  files,
		"total": total,
		"page":  page,
		"size":  size,
	}})
}

// FileQueryHandler compatibility alias
func (h *FileHandler) FileQueryHandler(c *gin.Context) {
	h.QueryHandler(c)
}

// RenameHandler update metadata (rename)
func (h *FileHandler) RenameHandler(c *gin.Context) {
	opType := c.PostForm("op")
	fileSha1 := c.PostForm("filehash")
	username := c.GetString("username")

	if opType != "0" {
		respondError(c, http.StatusForbidden, CodeInvalidParams, "不支持的操作")
		return
	}
	if fileSha1 == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少参数")
		return
	}

	newFileName := c.PostForm("filename")
	if newFileName == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filename 参数")
		return
	}

	if err := h.fileSvc.Rename(c.Request.Context(), fileSha1, username, filepath.Base(newFileName)); err != nil {
		slog.ErrorContext(c.Request.Context(), "rename failed", "error", err, "filehash", fileSha1)
		respondError(c, http.StatusForbidden, CodeForbidden, "无权操作该文件")
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功", "data": nil})
}

// FileMetaUpdateHandler compatibility alias
func (h *FileHandler) FileMetaUpdateHandler(c *gin.Context) {
	h.RenameHandler(c)
}

// DeleteHandler delete file and metadata (soft delete)
func (h *FileHandler) DeleteHandler(c *gin.Context) {
	fileSha1 := c.PostForm("filehash")
	if fileSha1 == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filehash 参数")
		return
	}

	if err := h.fileSvc.Delete(c.Request.Context(), fileSha1, c.GetString("username")); err != nil {
		respondError(c, http.StatusForbidden, CodeForbidden, "无权操作该文件")
		return
	}

	slog.InfoContext(c.Request.Context(), "file deleted", "filehash", fileSha1, "username", c.GetString("username"))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功", "data": nil})
}

// FileDeleteHandler compatibility alias
func (h *FileHandler) FileDeleteHandler(c *gin.Context) {
	h.DeleteHandler(c)
}

// TrashHandler list files in trash (paged)
func (h *FileHandler) TrashHandler(c *gin.Context) {
	username := c.GetString("username")
	page, _ := strconv.Atoi(c.Query("page"))
	size, _ := strconv.Atoi(c.Query("size"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	files, total, err := h.fileSvc.ListTrash(c.Request.Context(), username, page, size)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "list trash failed", "error", err, "username", username)
		respondError(c, http.StatusInternalServerError, CodeInternalError, "查询回收站失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": gin.H{"list": files, "total": total, "page": page, "size": size}})
}

// RestoreHandler restore a file from trash
func (h *FileHandler) RestoreHandler(c *gin.Context) {
	filehash := c.PostForm("filehash")
	if filehash == "" || !isValidHash(filehash) {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filehash 参数")
		return
	}

	if err := h.fileSvc.Restore(c.Request.Context(), filehash, c.GetString("username")); err != nil {
		slog.ErrorContext(c.Request.Context(), "restore failed", "error", err, "filehash", filehash)
		respondError(c, http.StatusNotFound, CodeNotFound, "回收站中不存在该文件")
		return
	}

	slog.InfoContext(c.Request.Context(), "file restored", "filehash", filehash, "username", c.GetString("username"))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "恢复成功", "data": nil})
}

// PurgeHandler permanently delete a file from trash (irrecoverable)
func (h *FileHandler) PurgeHandler(c *gin.Context) {
	filehash := c.PostForm("filehash")
	if filehash == "" || !isValidHash(filehash) {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filehash 参数")
		return
	}

	if err := h.fileSvc.Purge(c.Request.Context(), filehash, c.GetString("username")); err != nil {
		slog.ErrorContext(c.Request.Context(), "purge failed", "error", err, "filehash", filehash)
		respondError(c, http.StatusForbidden, CodeForbidden, "无权操作该文件")
		return
	}

	slog.InfoContext(c.Request.Context(), "file purged", "filehash", filehash, "username", c.GetString("username"))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已彻底删除", "data": nil})
}

// parseInt helper converts string to positive integer
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

// HealthCheckHandler health check endpoint (pure function, no injection required)
func HealthCheckHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": nil})
}
