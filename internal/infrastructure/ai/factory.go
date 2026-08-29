package ai

import (
	"gofile/internal/config"
	"gofile/internal/port"
)

// NewProvider creates the corresponding AI Provider based on configuration (env/system default)
func NewProvider(cfg *config.Config) port.Provider {
	switch cfg.AIProvider {
	case "openai":
		return NewOpenAIProvider(cfg.AIAPIKey, "", cfg.AIModel, "", cfg.AIEmbedDim)
	case "anthropic":
		return NewAnthropicProvider(cfg.AIAPIKey, cfg.AIModel, cfg.AIEmbedDim)
	default:
		return NewMockProvider(cfg.AIEmbedDim)
	}
}
