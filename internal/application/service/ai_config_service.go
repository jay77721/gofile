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

// providerCacheTTL 用户 Provider 解析结果的内存缓存时长
const providerCacheTTL = 5 * time.Minute

// errAPIKeyRequired 保存配置时既无新 key 也无旧 key
var errAPIKeyRequired = fmt.Errorf("api key is required")

// AIConfigService 用户级 AI Provider 配置管理
//
// 生效优先级:用户配置(DB)→ env 系统默认 → mock 降级。
// API key 使用 AES-GCM 加密落库,任何接口只回传掩码。
type AIConfigService struct {
	repo     port.AIConfigRepository
	cfg      *config.Config
	defaultP port.Provider // env 系统默认 provider(ResolveProvider 的回退目标由调用方处理)

	mu    sync.Mutex
	cache map[string]cachedProvider
}

type cachedProvider struct {
	prov port.Provider
	at   time.Time
}

// NewAIConfigService 创建用户 AI 配置服务
func NewAIConfigService(repo port.AIConfigRepository, cfg *config.Config, defaultP port.Provider) *AIConfigService {
	return &AIConfigService{
		repo:     repo,
		cfg:      cfg,
		defaultP: defaultP,
		cache:    make(map[string]cachedProvider),
	}
}

// ResolveProvider 解析用户名生效的 Provider(用户配置优先),无配置或解析失败返回 nil
// 供 Processor / AIService 注入;带 5min 内存缓存
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
		return nil // 无用户配置 → 调用方回退默认
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

// GetView 返回前端展示视图(API key 仅掩码)
func (s *AIConfigService) GetView(ctx context.Context, username string) (*model.AIConfigView, error) {
	view := &model.AIConfigView{Mode: "mock"}
	cfg, err := s.repo.Get(username)
	if err != nil {
		// 无配置:判定系统默认生效模式
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

// Save 保存用户配置;apiKey 为空时保留旧值
// 返回参数错误(如 SSRF 拦截)由调用方映射错误码
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
		// 保留旧 key
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

// Delete 清除用户配置(回退 env/mock)
func (s *AIConfigService) Delete(ctx context.Context, username string) error {
	if err := s.repo.Delete(username); err != nil {
		return err
	}
	s.invalidate(username)
	return nil
}

// TestConnection 用提交的参数(不持久化)做连通性测试:
// 先调对话接口,再调 embedding 接口探测实际维度
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
		res.OK = true // 摘要可用,语义搜索不可用
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

// invalidate 清除用户的 provider 缓存
func (s *AIConfigService) invalidate(username string) {
	s.mu.Lock()
	delete(s.cache, username)
	s.mu.Unlock()
}

// TestResult 连通性测试结果
type TestResult struct {
	OK          bool   `json:"ok"`           // 对话 + embedding 均可用
	ChatOK      bool   `json:"chat_ok"`      // 对话接口可用
	EmbedOK     bool   `json:"embed_ok"`     // embedding 接口可用
	Dim         int    `json:"dim"`          // embedding 实际返回维度
	DimMismatch bool   `json:"dim_mismatch"` // 实际维度 ≠ 检索引擎维度,语义搜索将降级
	Error       string `json:"error"`
}
