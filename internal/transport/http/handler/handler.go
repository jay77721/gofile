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

// 危险扩展名黑名单（存储型 XSS / 恶意文件分发 / 可执行文件）
// 上传时直接拒绝，不进入存储层
var dangerousExts = map[string]bool{
	// HTML / 脚本（存储型 XSS 主要载体）
	".html": true, ".htm": true, ".xhtml": true,
	".js": true, ".mjs": true, ".cjs": true,
	// Windows 可执行文件
	".exe": true, ".com": true, ".bat": true, ".cmd": true, ".ps1": true, ".msi": true, ".scr": true, ".pif": true,
	// Unix / Linux 可执行文件 / 脚本
	".sh": true, ".bash": true, ".zsh": true, ".csh": true, ".ksh": true,
	".bin": true, ".run": true, ".appimage": true,
	// 服务端脚本（防止在支持的环境中执行）
	".php": true, ".jsp": true, ".asp": true, ".aspx": true, ".cgi": true,
	// 脚本语言
	".py": true, ".pl": true, ".rb": true, ".lua": true,
	// 其他危险类型
	".svg": true, // 可内联 JavaScript，预览时需特殊处理
	".jar": true, // Java 可执行
	".war": true, ".ear": true,
	".ps":  true, // PowerShell 脚本
	".vbs": true, ".vbe": true, ".wsf": true, ".wsh": true,
	".reg": true, // Windows 注册表文件
	".inf": true,
}

// isDangerousExtension 检查文件扩展名是否在黑名单中
func isDangerousExtension(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return dangerousExts[ext]
}

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
// @Summary 上传文件
// @Description 支持秒传去重（通过 filehash 参数），危险文件类型（.html/.js/.exe 等）会被拒绝
// @Tags 文件
// @Accept multipart/form-data
// @Produce json
// @Security ApiKeyAuth
// @Param file formData file true "文件"
// @Param filehash formData string false "文件 SHA1（秒传检测）"
// @Success 200 {object} map[string]any{code=int,msg=string,data=object{filehash=string}} "上传成功"
// @Failure 400 {object} map[string]any{code=int,msg=string,data=nil} "参数错误或文件类型不允许"
// @Router /file/upload [post]
func (h *FileHandler) UploadHandler(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxUploadSize)

	fileHash := c.PostForm("filehash")
	// 秒传检测(命中时同时建立当前用户的所有权关联)
	if fileHash != "" {
		exists, err := h.fileSvc.FastUpload(c.Request.Context(), fileHash, c.GetString("username"))
		if err != nil {
			slog.WarnContext(c.Request.Context(), "upload: fast upload check failed", "error", err, "filehash", fileHash)
		} else if exists {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "秒传成功", "data": nil})
			return
		}
	}

	// 解析上传的文件
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "文件获取失败")
		return
	}
	defer file.Close()

	// 路径穿越防护
	filename := filepath.Base(header.Filename)

	// 危险文件类型黑名单（防存储型 XSS / 恶意文件分发）
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

// FastUploadHandler 处理独立秒传检测接口
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

// PresignUploadHandler 获取预签名上传 URL
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

// ConfirmUploadHandler 确认预签名上传完成
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

	// 危险文件类型黑名单（防存储型 XSS / 恶意文件分发）
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

// PresignDownloadHandler 获取预签名下载 URL
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

// MetaHandler 获取文件元信息
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

// GetFileHandler 兼容别名
func (h *FileHandler) GetFileHandler(c *gin.Context) {
	h.MetaHandler(c)
}

// QueryHandler 返回用户文件列表（支持分页与目录树查询）
// @Summary 查询文件列表
// @Description 支持分页，无分页参数时返回全部文件
// @Tags 文件
// @Produce json
// @Security ApiKeyAuth
// @Param page query int false "页码（从 1 开始）"
// @Param size query int false "每页数量（1-100，默认 20）"
// @Success 200 {object} map[string]any{code=int,msg=string,data=object{list=array,total=int,page=int,size=int}} "文件列表"
// @Router /file/query [get]
func (h *FileHandler) QueryHandler(c *gin.Context) {
	username := c.GetString("username")

	parentIDStr := c.Query("parent_id")
	pageStr := c.Query("page")
	sizeStr := c.Query("size")

	// 若传递了 parent_id，按目录层级与面包屑查询
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
		// 无分页参数：返回全部（兼容旧逻辑）
		fileMetas, err := h.fileSvc.ListByUser(c.Request.Context(), username)
		if err != nil {
			slog.ErrorContext(c.Request.Context(), "query all files failed", "error", err)
			respondError(c, http.StatusInternalServerError, CodeInternalError, "查询失败")
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": fileMetas})
		return
	}

	// 有分页参数：走分页逻辑
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

// FileQueryHandler 兼容别名
func (h *FileHandler) FileQueryHandler(c *gin.Context) {
	h.QueryHandler(c)
}

// RenameHandler 更新元信息接口（重命名）
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

// FileMetaUpdateHandler 兼容别名
func (h *FileHandler) FileMetaUpdateHandler(c *gin.Context) {
	h.RenameHandler(c)
}

// DeleteHandler 删除文件及元信息（软删除）
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

// FileDeleteHandler 兼容别名
func (h *FileHandler) FileDeleteHandler(c *gin.Context) {
	h.DeleteHandler(c)
}

// TrashHandler 回收站文件列表（分页）
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

// RestoreHandler 恢复回收站文件
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

// PurgeHandler 彻底删除回收站文件（不可恢复）
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

// parseInt 字符串转正整数辅助函数
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
