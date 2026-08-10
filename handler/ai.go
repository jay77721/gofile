package handler

import (
	"gofile/service"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// AIHandler AI 语义检索 HTTP 处理器
type AIHandler struct {
	aiSvc *service.AIService
}

// NewAIHandler 创建 AI 处理器
func NewAIHandler(aiSvc *service.AIService) *AIHandler {
	return &AIHandler{aiSvc: aiSvc}
}

// SearchHandler 对话式语义检索
// GET /file/ai/search?q=&page=&size=
func (h *AIHandler) SearchHandler(c *gin.Context) {
	q := c.Query("q")
	if q == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 q 参数")
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	size, _ := strconv.Atoi(c.Query("size"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	username := c.GetString("username")

	results, err := h.aiSvc.Search(c.Request.Context(), username, q, page, size)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "ai search failed", "error", err, "q", q)
		respondError(c, http.StatusInternalServerError, CodeSearchFailed, "搜索失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": gin.H{
		"list": results,
		"page": page,
		"size": size,
	}})
}

// SimilarHandler 相似文件推荐
// GET /file/ai/similar?filehash=&limit=
func (h *AIHandler) SimilarHandler(c *gin.Context) {
	filehash := c.Query("filehash")
	if filehash == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filehash 参数")
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	username := c.GetString("username")

	results, err := h.aiSvc.Similar(c.Request.Context(), username, filehash, limit)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "ai similar failed", "error", err, "filehash", filehash)
		if err.Error() == "file not found or no permission" {
			respondError(c, http.StatusForbidden, CodeForbidden, "无权操作该文件")
			return
		}
		respondError(c, http.StatusInternalServerError, CodeSearchFailed, "相似推荐失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": results})
}

// DuplicatesHandler 近似重复检测
// GET /file/ai/duplicates?filehash=&threshold=
func (h *AIHandler) DuplicatesHandler(c *gin.Context) {
	filehash := c.Query("filehash")
	if filehash == "" {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "缺少 filehash 参数")
		return
	}
	threshold, _ := strconv.ParseFloat(c.Query("threshold"), 64)
	username := c.GetString("username")

	results, err := h.aiSvc.Duplicates(c.Request.Context(), username, filehash, threshold)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "ai duplicates failed", "error", err, "filehash", filehash)
		if err.Error() == "file not found or no permission" {
			respondError(c, http.StatusForbidden, CodeForbidden, "无权操作该文件")
			return
		}
		respondError(c, http.StatusInternalServerError, CodeSearchFailed, "重复检测失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": results})
}
