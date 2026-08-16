package handler

import (
	"errors"
	"gofile/model"
	"gofile/storage"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

// MultipartCompleteReq 合并分片请求 DTO
type MultipartCompleteReq = model.MultipartCompleteReq

// MultipartAbortReq 取消分片请求 DTO
type MultipartAbortReq = model.MultipartAbortReq

// ---- S3 Multipart 分片直传 Handlers ----

// MultipartInitHandler 初始化 S3 分片直传
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

// InitMultipartHandler 兼容别名
func (h *FileHandler) InitMultipartHandler(c *gin.Context) {
	h.MultipartInitHandler(c)
}

// MultipartCompleteHandler 完成分片上传并由存储层合并
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

// CompleteMultipartHandler 兼容别名
func (h *FileHandler) CompleteMultipartHandler(c *gin.Context) {
	h.MultipartCompleteHandler(c)
}

// MultipartAbortHandler 取消分片上传会话
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

// AbortMultipartHandler 兼容别名
func (h *FileHandler) AbortMultipartHandler(c *gin.Context) {
	h.MultipartAbortHandler(c)
}

// ---- 传统分片上传 Handlers ----

// UploadChunkHandler 分块上传（用户隔离）
func (h *FileHandler) UploadChunkHandler(c *gin.Context) {
	r := c.Request
	r.Body = http.MaxBytesReader(c.Writer, r.Body, MaxUploadSize)

	fileHash := c.PostForm("filehash")
	index := c.PostForm("index")

	if fileHash == "" || index == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filehash 或 index 参数")
		return
	}

	// 校验 hash 格式，防止路径穿越
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
		// 已上传的情况不算错误
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "chunk already uploaded", "data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "chunk upload success", "data": nil})
}

// ChunkStatusHandler 断点续传状态查询（用户隔离）
func (h *FileHandler) ChunkStatusHandler(c *gin.Context) {
	fileHash := c.Query("filehash")
	if fileHash == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filehash 参数")
		return
	}

	// 校验 hash 格式，防止路径穿越
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

// UploadStatusHandler 兼容别名
func (h *FileHandler) UploadStatusHandler(c *gin.Context) {
	h.ChunkStatusHandler(c)
}

// MergeHandler 分块合并
func (h *FileHandler) MergeHandler(c *gin.Context) {
	fileHash := c.PostForm("filehash")
	fileName := c.PostForm("filename")
	totalStr := c.PostForm("chunks")

	if fileHash == "" || fileName == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filehash 或 filename 参数")
		return
	}

	// 校验 hash 格式，防止路径穿越
	if !isValidHash(fileHash) {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "无效的 filehash 格式")
		return
	}

	// 路径穿越防护
	fileName = filepath.Base(fileName)

	// 危险文件类型黑名单（防存储型 XSS / 恶意文件分发）
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

// MergeChunkHandler 兼容别名
func (h *FileHandler) MergeChunkHandler(c *gin.Context) {
	h.MergeHandler(c)
}
