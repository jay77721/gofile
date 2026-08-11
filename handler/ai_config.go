package handler

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"gofile/service"
)

// AIConfigHandler 用户级 AI Provider 配置端点
// 前端可自定义 OpenAI 协议 baseURL + API key,保存后异步任务与语义搜索按用户生效
type AIConfigHandler struct {
	svc *service.AIConfigService
}

// NewAIConfigHandler 创建 AI 配置 handler
func NewAIConfigHandler(svc *service.AIConfigService) *AIConfigHandler {
	return &AIConfigHandler{svc: svc}
}

// aiConfigReq 保存/测试连接的请求体
type aiConfigReq struct {
	BaseURL    string `json:"base_url"`    // OpenAI 协议端点,空 = 官方默认
	APIKey     string `json:"api_key"`     // 空 = 保留旧值(仅保存时)
	Model      string `json:"model"`       // 对话模型名
	EmbedModel string `json:"embed_model"` // embedding 模型名
}

// GetConfigHandler 获取当前用户配置(API key 仅掩码)
// GET /ai/config
func (h *AIConfigHandler) GetConfigHandler(c *gin.Context) {
	username := c.GetString("username")
	view, err := h.svc.GetView(c.Request.Context(), username)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "ai config: get failed", "error", err, "username", username)
		respondError(c, http.StatusInternalServerError, CodeStorageError, "读取 AI 配置失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": view})
}

// SaveConfigHandler 保存当前用户配置
// POST /ai/config
func (h *AIConfigHandler) SaveConfigHandler(c *gin.Context) {
	var req aiConfigReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "参数格式错误")
		return
	}
	req.BaseURL = strings.TrimSpace(req.BaseURL)
	req.Model = strings.TrimSpace(req.Model)
	req.EmbedModel = strings.TrimSpace(req.EmbedModel)
	req.APIKey = strings.TrimSpace(req.APIKey)

	username := c.GetString("username")
	if err := h.svc.Save(c.Request.Context(), username, req.BaseURL, req.APIKey, req.Model, req.EmbedModel); err != nil {
		msg := "保存 AI 配置失败"
		code := CodeStorageError
		status := http.StatusInternalServerError
		// URL 校验失败(含 SSRF 拦截)属参数问题
		if isURLValidationError(err) {
			msg = err.Error()
			code = CodeInvalidParams
			status = http.StatusBadRequest
		}
		slog.WarnContext(c.Request.Context(), "ai config: save failed", "error", err, "username", username)
		respondError(c, status, code, msg)
		return
	}
	slog.InfoContext(c.Request.Context(), "ai config saved", "username", username)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已保存,配置即刻生效", "data": nil})
}

// DeleteConfigHandler 清除当前用户配置(回退系统默认/mock)
// DELETE /ai/config
func (h *AIConfigHandler) DeleteConfigHandler(c *gin.Context) {
	username := c.GetString("username")
	if err := h.svc.Delete(c.Request.Context(), username); err != nil {
		slog.WarnContext(c.Request.Context(), "ai config: delete failed", "error", err, "username", username)
		respondError(c, http.StatusInternalServerError, CodeStorageError, "清除配置失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已清除,回退系统默认配置", "data": nil})
}

// TestConfigHandler 测试连接(不持久化)
// POST /ai/config/test
func (h *AIConfigHandler) TestConfigHandler(c *gin.Context) {
	var req aiConfigReq
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, CodeInvalidParams, "参数格式错误")
		return
	}
	result := h.svc.TestConnection(c.Request.Context(), strings.TrimSpace(req.BaseURL), strings.TrimSpace(req.APIKey), strings.TrimSpace(req.Model), strings.TrimSpace(req.EmbedModel))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": result})
}

// isURLValidationError 判断错误是否来自 URL/SSRF 校验(映射为参数错误)
func isURLValidationError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "invalid url") ||
		strings.Contains(msg, "scheme must be") ||
		strings.Contains(msg, "host is required") ||
		strings.Contains(msg, "private network address not allowed") ||
		strings.Contains(msg, "resolve host failed")
}
