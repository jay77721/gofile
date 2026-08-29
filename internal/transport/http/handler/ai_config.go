package handler

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"gofile/internal/application/service"
)

// AIConfigHandler per-user AI Provider configuration endpoints
// Frontend can customize OpenAI-compatible baseURL + API key; async tasks and semantic search take effect per user after saving
type AIConfigHandler struct {
	svc *service.AIConfigService
}

// NewAIConfigHandler create the AI configuration handler
func NewAIConfigHandler(svc *service.AIConfigService) *AIConfigHandler {
	return &AIConfigHandler{svc: svc}
}

// aiConfigReq request body for saving/testing the connection
type aiConfigReq struct {
	BaseURL    string `json:"base_url"`    // OpenAI-compatible endpoint; empty means official default
	APIKey     string `json:"api_key"`     // empty means keep the previous value (only when saving)
	Model      string `json:"model"`       // chat model name
	EmbedModel string `json:"embed_model"` // embedding model name
}

// GetConfigHandler retrieve the current user config (API key is masked)
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

// SaveConfigHandler save the current user configuration
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
		// URL validation failure (including SSRF interception) is treated as invalid params
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

// DeleteConfigHandler clear the current user configuration (falls back to system default/mock)
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

// TestConfigHandler test the connection (without persistence)
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

// isURLValidationError check whether the error originates from URL/SSRF validation (mapped to invalid params)
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
