package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	cryptoutil "gofile/internal/common/crypto"
	"gofile/internal/common/urlcheck"
	"gofile/internal/config"
	"gofile/internal/domain"
	"gofile/internal/infrastructure/ai"
	"gofile/internal/port"
)

// providerCacheTTL is the in-memory cache TTL for per-user Provider resolution.
const providerCacheTTL = 5 * time.Minute

// errAPIKeyRequired is returned when saving config with neither new nor existing key.
var errAPIKeyRequired = fmt.Errorf("api key is required")

// AIConfigService manages per-user AI Provider configuration.
//
// Effective priority: user config (DB) -> env system default -> mock fallback.
// API keys are encrypted with AES-GCM before storage; all endpoints return only masked values.
type AIConfigService struct {
	repo     port.AIConfigRepository
	cfg      *config.Config
	defaultP port.Provider // env system default provider (fallback target of ResolveProvider is handled by caller)

	mu    sync.Mutex
	cache map[string]cachedProvider
}

type cachedProvider struct {
	prov port.Provider
	at   time.Time
}

// NewAIConfigService creates the user AI configuration service.
func NewAIConfigService(repo port.AIConfigRepository, cfg *config.Config, defaultP port.Provider) *AIConfigService {
	return &AIConfigService{
		repo:     repo,
		cfg:      cfg,
		defaultP: defaultP,
		cache:    make(map[string]cachedProvider),
	}
}

// ResolveProvider resolves the effective Provider for a username (user config takes precedence); returns nil if no config or resolution fails.
// For injection into Processor / AIService; includes 5min in-memory cache.
func (s *AIConfigService) ResolveProvider(ctx context.Context, username string) port.Provider {
	if s == nil || username == "" {
		return nil
	}

	s.mu.Lock()
	if c, ok := s.cache[username]; ok && time.Since(c.at) < providerCacheTTL {
		s.mu.Unlock()
		return c.prov
	}
	s.mu.Unlock()

	cfg, err := s.repo.Get(username)
	if err != nil {
		return nil // no user config -> caller falls back to default
	}
	if cfg.APIKeyEnc == "" {
		return nil
	}
	key, err := cryptoutil.DecryptSecret(s.cfg.AIConfigSecretKey(), cfg.APIKeyEnc)
	if err != nil || key == "" {
		slog.Warn("ai config: decrypt api key failed", "username", username, "error", err)
		return nil
	}
	prov := ai.NewOpenAIProvider(key, cfg.BaseURL, cfg.Model, cfg.EmbedModel, s.cfg.AIEmbedDim)

	s.mu.Lock()
	s.cache[username] = cachedProvider{prov: prov, at: time.Now()}
	s.mu.Unlock()
	return prov
}

// GetView returns the frontend display view (API key masked only).
func (s *AIConfigService) GetView(ctx context.Context, username string) (*model.AIConfigView, error) {
	view := &model.AIConfigView{Mode: "mock"}
	cfg, err := s.repo.Get(username)
	if err != nil {
		// no config: determine system-default effective mode
		if s.defaultP != nil && s.cfg.AIProvider == "openai" {
			view.Mode = "openai"
		}
		return view, nil
	}
	view.Configured = true
	view.BaseURL = cfg.BaseURL
	view.Model = cfg.Model
	view.EmbedModel = cfg.EmbedModel
	view.HasKey = cfg.APIKeyEnc != ""
	if cfg.APIKeyEnc != "" {
		if key, err := cryptoutil.DecryptSecret(s.cfg.AIConfigSecretKey(), cfg.APIKeyEnc); err == nil {
			view.APIKeyMask = cryptoutil.MaskSecret(key)
		}
	}
	view.Mode = "openai"
	return view, nil
}

// Save persists user config; when apiKey is empty the old value is kept.
// Parameter errors (e.g. SSRF blocked) are mapped to error codes by the caller.
func (s *AIConfigService) Save(ctx context.Context, username, baseURL, apiKey, chatModel, embedModel string) error {
	if baseURL != "" {
		if err := urlcheck.ValidatePublicURL(baseURL, s.cfg.AllowPrivateAIURL); err != nil {
			return err
		}
	}

	cfg := &model.AIConfig{
		Username:   username,
		BaseURL:    baseURL,
		Model:      chatModel,
		EmbedModel: embedModel,
	}
	if apiKey != "" {
		enc, err := cryptoutil.EncryptSecret(s.cfg.AIConfigSecretKey(), apiKey)
		if err != nil {
			return err
		}
		cfg.APIKeyEnc = enc
	} else {
		// keep existing key
		existing, err := s.repo.Get(username)
		if err == nil {
			cfg.APIKeyEnc = existing.APIKeyEnc
		}
	}
	if cfg.APIKeyEnc == "" {
		return errAPIKeyRequired
	}
	if err := s.repo.Upsert(cfg); err != nil {
		return err
	}
	s.invalidate(username)
	return nil
}

// Delete clears user config (falls back to env/mock).
func (s *AIConfigService) Delete(ctx context.Context, username string) error {
	if err := s.repo.Delete(username); err != nil {
		return err
	}
	s.invalidate(username)
	return nil
}

// TestConnection tests connectivity with the submitted parameters (without persisting):
// it calls the chat API first, then probes the embedding API to detect actual dimensions.
func (s *AIConfigService) TestConnection(ctx context.Context, baseURL, apiKey, model, embedModel string) TestResult {
	res := TestResult{}
	if apiKey == "" {
		res.Error = "API key 不能为空"
		return res
	}
	if baseURL != "" {
		if err := urlcheck.ValidatePublicURL(baseURL, s.cfg.AllowPrivateAIURL); err != nil {
			res.Error = err.Error()
			return res
		}
	}
	prov := ai.NewOpenAIProviderWithTimeout(apiKey, baseURL, model, embedModel, s.cfg.AIEmbedDim, 10*time.Second)

	if _, err := prov.Analyze(ctx, "connection_test.txt", "ping"); err != nil {
		res.Error = "对话接口调用失败: " + err.Error()
		return res
	}
	res.ChatOK = true

	vec, err := prov.Embed(ctx, "ping")
	if err != nil {
		res.Error = "对话接口正常,但 embedding 接口失败: " + err.Error()
		res.OK = true // summary available, semantic search unavailable
		return res
	}
	res.EmbedOK = true
	res.Dim = len(vec)
	if res.Dim != s.cfg.AIEmbedDim {
		res.DimMismatch = true
	}
	res.OK = true
	return res
}

// invalidate clears the cached provider for a user.
func (s *AIConfigService) invalidate(username string) {
	s.mu.Lock()
	delete(s.cache, username)
	s.mu.Unlock()
}

// TestResult is the connectivity test result.
type TestResult struct {
	OK          bool   `json:"ok"`           // chat + embedding both available
	ChatOK      bool   `json:"chat_ok"`      // chat API available
	EmbedOK     bool   `json:"embed_ok"`     // embedding API available
	Dim         int    `json:"dim"`          // actual embedding dimension returned
	DimMismatch bool   `json:"dim_mismatch"` // actual dimension != index engine dimension, semantic search will be degraded
	Error       string `json:"error"`
}
