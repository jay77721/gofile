package handler

import (
	"errors"
	"gofile/internal/domain"
	"gofile/internal/infrastructure/storage"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

// MultipartCompleteReq DTO for merging chunks
type MultipartCompleteReq = model.MultipartCompleteReq

// MultipartAbortReq DTO for aborting multipart upload
type MultipartAbortReq = model.MultipartAbortReq

// ---- S3 Multipart direct upload handlers ----

// MultipartInitHandler initialize S3 multipart direct upload
func (h *FileHandler) MultipartInitHandler(c *gin.Context) {
	var req model.MultipartInitReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "参数错误: "+err.Error())
		return
	}

	if !isValidHash(req.FileSha1) {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "无效的 filehash 格式")
		return
	}
	if isDangerousExtension(req.FileName) {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "该文件类型不允许上传")
		return
	}

	username := c.GetString("username")
	resp, err := h.fileSvc.InitMultipartUpload(c.Request.Context(), username, req)
	if err != nil {
		if errors.Is(err, storage.ErrPresignNotSupported) {
			respondError(c, http.StatusBadRequest, CodeStorageError, "分片直传仅支持 MinIO/S3 存储，当前为本地存储")
			return
		}
		slog.ErrorContext(c.Request.Context(), "init multipart failed", "error", err, "username", username)
		respondError(c, http.StatusInternalServerError, CodeUploadFailed, "初始化分片直传失败: "+err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": resp})
}

// InitMultipartHandler compatibility alias
func (h *FileHandler) InitMultipartHandler(c *gin.Context) {
	h.MultipartInitHandler(c)
}

// MultipartCompleteHandler complete multipart upload and merge via storage layer
func (h *FileHandler) MultipartCompleteHandler(c *gin.Context) {
	var req MultipartCompleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "参数错误: "+err.Error())
		return
	}

	username := c.GetString("username")
	meta, err := h.fileSvc.CompleteMultipartUpload(c.Request.Context(), username, req)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "complete multipart failed", "error", err, "upload_id", req.UploadID, "username", username)
		respondError(c, http.StatusInternalServerError, CodeMergeFailed, "分片合并失败: "+err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "上传并合并成功", "data": meta})
}

// CompleteMultipartHandler compatibility alias
func (h *FileHandler) CompleteMultipartHandler(c *gin.Context) {
	h.MultipartCompleteHandler(c)
}

// MultipartAbortHandler abort the multipart upload session
func (h *FileHandler) MultipartAbortHandler(c *gin.Context) {
	var req MultipartAbortReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "参数错误: "+err.Error())
		return
	}

	username := c.GetString("username")
	if err := h.fileSvc.AbortMultipartUpload(c.Request.Context(), username, req.UploadID); err != nil {
		slog.WarnContext(c.Request.Context(), "abort multipart failed", "error", err, "upload_id", req.UploadID)
		respondError(c, http.StatusInternalServerError, CodeStorageError, "取消分片失败: "+err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已取消", "data": nil})
}

// AbortMultipartHandler compatibility alias
func (h *FileHandler) AbortMultipartHandler(c *gin.Context) {
	h.MultipartAbortHandler(c)
}

// ---- Legacy chunked upload handlers ----

// UploadChunkHandler chunked upload (per-user isolated)
func (h *FileHandler) UploadChunkHandler(c *gin.Context) {
	r := c.Request
	r.Body = http.MaxBytesReader(c.Writer, r.Body, MaxUploadSize)

	fileHash := c.PostForm("filehash")
	index := c.PostForm("index")

	if fileHash == "" || index == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filehash 或 index 参数")
		return
	}

	// Validate hash format to prevent path traversal
	if !isValidHash(fileHash) {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "无效的 filehash 格式")
		return
	}

	chunkIndex, err := parseInt(index)
	if err != nil || chunkIndex < 0 {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "无效的 chunk index")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "分块文件获取失败")
		return
	}
	defer file.Close()

	username := c.GetString("username")
	if err := h.fileSvc.UploadChunk(c.Request.Context(), fileHash, chunkIndex, file, username); err != nil {
		slog.ErrorContext(c.Request.Context(), "upload chunk failed", "error", err, "filehash", fileHash, "index", chunkIndex, "username", username)
		// Already uploaded is not considered an error
		respondError(c, http.StatusInternalServerError, CodeUploadFailed, "chunk upload failed")
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "chunk upload success", "data": nil})
}

// ChunkStatusHandler query resumable upload status (per-user isolated)
func (h *FileHandler) ChunkStatusHandler(c *gin.Context) {
	fileHash := c.Query("filehash")
	if fileHash == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filehash 参数")
		return
	}

	// Validate hash format to prevent path traversal
	if !isValidHash(fileHash) {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "无效的 filehash 格式")
		return
	}

	chunks, err := h.fileSvc.GetChunkStatus(fileHash, c.GetString("username"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": []string{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": chunks})
}

// UploadStatusHandler compatibility alias
func (h *FileHandler) UploadStatusHandler(c *gin.Context) {
	h.ChunkStatusHandler(c)
}

// MergeHandler merge chunks
func (h *FileHandler) MergeHandler(c *gin.Context) {
	fileHash := c.PostForm("filehash")
	fileName := c.PostForm("filename")
	totalStr := c.PostForm("chunks")

	if fileHash == "" || fileName == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filehash 或 filename 参数")
		return
	}
	if totalStr == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 chunks 参数")
		return
	}
	if total, err := parseInt(totalStr); err != nil || total <= 0 {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "无效 chunks 参数")
		return
	}

	// Validate hash format to prevent path traversal
	if !isValidHash(fileHash) {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "无效的 filehash 格式")
		return
	}

	// Path traversal protection
	fileName = filepath.Base(fileName)

	// Dangerous file type blocklist (prevent stored XSS / malicious distribution)
	if isDangerousExtension(fileName) {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "该文件类型不允许上传")
		return
	}

	fMeta, err := h.fileSvc.MergeChunks(c.Request.Context(), fileHash, fileName, c.GetString("username"), totalStr)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "merge chunks failed", "error", err, "filehash", fileHash)
		respondError(c, http.StatusInternalServerError, CodeMergeFailed, "文件合并失败")
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "merge success", "data": gin.H{"filehash": fMeta.FileSha1}})
}

// MergeChunkHandler compatibility alias
func (h *FileHandler) MergeChunkHandler(c *gin.Context) {
	h.MergeHandler(c)
}
