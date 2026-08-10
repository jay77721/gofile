package handler

import (
	"errors"
	"fmt"
	"gofile/model"
	"gofile/service"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// ShareHandler 文件分享 HTTP 处理器
type ShareHandler struct {
	shareSvc *service.ShareService
	fileSvc  *service.FileService
}

// NewShareHandler 创建分享处理器
func NewShareHandler(shareSvc *service.ShareService, fileSvc *service.FileService) *ShareHandler {
	return &ShareHandler{shareSvc: shareSvc, fileSvc: fileSvc}
}

// CreateShareHandler 创建分享
// POST /file/share: filehash、days(1-30,默认7)、password(可选)
func (h *ShareHandler) CreateShareHandler(c *gin.Context) {
	filehash := c.PostForm("filehash")
	if filehash == "" || !isValidHash(filehash) {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filehash 参数")
		return
	}
	days, _ := strconv.Atoi(c.PostForm("days"))
	password := c.PostForm("password")

	share, err := h.shareSvc.Create(c.Request.Context(), c.GetString("username"), filehash, days, password)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "create share failed", "error", err, "filehash", filehash)
		respondError(c, http.StatusForbidden, CodeForbidden, "无权操作该文件")
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "分享成功", "data": gin.H{
		"share_token": share.ShareToken,
		"expire_at":   share.ExpireAt.Format("2006-01-02 15:04:05"),
		"url":         "/share/" + share.ShareToken,
	}})
}

// ShareListHandler 我的分享列表
// GET /file/share/list
func (h *ShareHandler) ShareListHandler(c *gin.Context) {
	shares, err := h.shareSvc.List(c.GetString("username"))
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "list shares failed", "error", err)
		respondError(c, http.StatusInternalServerError, CodeInternalError, "查询分享列表失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": shares})
}

// RevokeShareHandler 撤销分享
// POST /file/share/revoke: share_token
func (h *ShareHandler) RevokeShareHandler(c *gin.Context) {
	token := strings.TrimSpace(c.PostForm("share_token"))
	if token == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 share_token 参数")
		return
	}

	if err := h.shareSvc.Revoke(c.Request.Context(), token, c.GetString("username")); err != nil {
		if errors.Is(err, service.ErrShareNotFound) {
			respondError(c, http.StatusNotFound, CodeNotFound, "分享不存在")
			return
		}
		slog.ErrorContext(c.Request.Context(), "revoke share failed", "error", err)
		respondError(c, http.StatusInternalServerError, CodeInternalError, "撤销分享失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已撤销分享", "data": nil})
}

// ShareDownloadHandler 免登录分享下载(公开路由,支持 Range 断点)
// GET /share/:token?pwd=提取码(可选)
func (h *ShareHandler) ShareDownloadHandler(c *gin.Context) {
	token := c.Param("token")
	fMeta, err := h.shareSvc.Resolve(c.Request.Context(), token, c.Query("pwd"))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrShareWrongPwd):
			respondError(c, http.StatusForbidden, CodeForbidden, "提取码错误")
		case errors.Is(err, service.ErrShareFileGone):
			respondError(c, http.StatusNotFound, CodeNotFound, "分享的文件已不可用")
		default:
			respondError(c, http.StatusNotFound, CodeNotFound, "分享不存在或已过期")
		}
		return
	}

	h.streamSharedFile(c, fMeta)
}

// streamSharedFile 输出分享文件内容(支持 Range 206)
func (h *ShareHandler) streamSharedFile(c *gin.Context, fMeta model.FileMeta) {
	safeName := sanitizeFilename(fMeta.FileName)

	if rangeHeader := c.GetHeader("Range"); rangeHeader != "" {
		offset, length, ok := parseRangeHeader(rangeHeader, 0)
		if !ok {
			if size, err := h.fileSvc.FileSize(c.Request.Context(), fMeta.FileSha1, fMeta.Username); err == nil {
				c.Header("Content-Range", fmt.Sprintf("bytes */%d", size))
			}
			respondError(c, http.StatusRequestedRangeNotSatisfiable, CodeInvalidParams, "无效的 Range 范围")
			return
		}

		reader, _, totalSize, actualLen, err := h.fileSvc.DownloadRange(c.Request.Context(), fMeta.FileSha1, fMeta.Username, offset, length)
		if err != nil {
			if errors.Is(err, service.ErrRangeOutOfBounds) {
				c.Header("Content-Range", fmt.Sprintf("bytes */%d", totalSize))
				respondError(c, http.StatusRequestedRangeNotSatisfiable, CodeInvalidParams, "Range 越界")
				return
			}
			slog.ErrorContext(c.Request.Context(), "share range failed", "error", err, "filehash", fMeta.FileSha1)
			respondError(c, http.StatusNotFound, CodeNotFound, "文件不存在")
			return
		}
		defer reader.Close()

		c.Header("Content-Disposition", buildContentDisposition(safeName))
		c.Header("Content-Type", "application/octet-stream")
		c.Header("Accept-Ranges", "bytes")
		c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, offset+actualLen-1, totalSize))
		c.Header("Content-Length", fmt.Sprintf("%d", actualLen))
		c.Status(http.StatusPartialContent) // 206
		io.CopyBuffer(c.Writer, reader, make([]byte, 32*1024))
		return
	}

	reader, _, err := h.fileSvc.Download(c.Request.Context(), fMeta.FileSha1, fMeta.Username)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "share download failed", "error", err, "filehash", fMeta.FileSha1)
		respondError(c, http.StatusNotFound, CodeNotFound, "文件不存在")
		return
	}
	defer reader.Close()

	c.Header("Content-Disposition", buildContentDisposition(safeName))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Accept-Ranges", "bytes")
	io.CopyBuffer(c.Writer, reader, make([]byte, 32*1024))
}
