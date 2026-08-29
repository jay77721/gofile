package handler

import (
	"gofile/internal/domain"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// CreateFolderReq DTO for creating a folder
type CreateFolderReq = model.FolderCreateReq

// RenameFolderReq DTO for renaming a folder or file
type RenameFolderReq = model.FolderRenameReq

// MoveFolderReq DTO for moving a file or folder
type MoveFolderReq = model.FolderMoveReq

// CreateFolderHandler Create folder
func (h *FileHandler) CreateFolderHandler(c *gin.Context) {
	var req CreateFolderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "参数错误: "+err.Error())
		return
	}

	username := c.GetString("username")
	uf, err := h.fileSvc.CreateFolder(c.Request.Context(), username, req)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "create folder failed", "error", err, "username", username)
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "创建文件夹失败: "+err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "创建成功", "data": uf})
}

// RenameFolderHandler rename a file or folder
func (h *FileHandler) RenameFolderHandler(c *gin.Context) {
	var req RenameFolderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "参数错误: "+err.Error())
		return
	}

	username := c.GetString("username")
	if err := h.fileSvc.RenameFolderOrFile(c.Request.Context(), username, req); err != nil {
		slog.ErrorContext(c.Request.Context(), "rename folder/file failed", "error", err, "username", username)
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "重命名失败: "+err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "重命名成功", "data": nil})
}

// MoveFolderHandler move a file or folder
func (h *FileHandler) MoveFolderHandler(c *gin.Context) {
	var req MoveFolderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "参数错误: "+err.Error())
		return
	}

	username := c.GetString("username")
	if err := h.fileSvc.MoveFolderOrFile(c.Request.Context(), username, req); err != nil {
		slog.ErrorContext(c.Request.Context(), "move folder/file failed", "error", err, "username", username)
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "移动失败: "+err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "移动成功", "data": nil})
}
