package ai

import "gofile/internal/config"

// NewProvider 根据配置创建对应的 AI Provider(env 系统默认)
func NewProvider(cfg *config.Config) Provider {
	switch cfg.AIProvider {
	case "openai":
		return NewOpenAIProvider(cfg.AIAPIKey, "", cfg.AIModel, "", cfg.AIEmbedDim)
	case "anthropic":
		return NewAnthropicProvider(cfg.AIAPIKey, cfg.AIModel, cfg.AIEmbedDim)
	default:
		return NewMockProvider(cfg.AIEmbedDim)
	}
}
