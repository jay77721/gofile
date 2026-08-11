package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"gofile/ai"
	"gofile/repository"
)

// SearchResult AI 检索返回的单条结果
type SearchResult struct {
	Filehash string  `json:"filehash"`
	Filename string  `json:"filename"`
	Summary  string  `json:"summary"`
	Tags     string  `json:"tags"`
	Size     int64   `json:"size"`
	Score    float64 `json:"score"`
}

// AIService AI 语义检索编排（搜索 / 相似推荐 / 重复检测）
type AIService struct {
	indexer  ai.Indexer
	provider ai.Provider
	fileRepo repository.FileRepository

	// resolve 按用户名解析生效 Provider(用户自定义配置优先),nil 时使用默认 provider
	resolve func(ctx context.Context, username string) ai.Provider
}

// NewAIService 创建 AI 检索服务
func NewAIService(indexer ai.Indexer, provider ai.Provider, fileRepo repository.FileRepository) *AIService {
	return &AIService{indexer: indexer, provider: provider, fileRepo: fileRepo}
}

// WithResolver 注入按用户解析 Provider 的函数(用户级 AI 配置)
func (s *AIService) WithResolver(fn func(ctx context.Context, username string) ai.Provider) *AIService {
	s.resolve = fn
	return s
}

// providerFor 解析当前用户生效的 Provider,解析不到时回退默认
func (s *AIService) providerFor(ctx context.Context, username string) ai.Provider {
	if s.resolve != nil {
		if prov := s.resolve(ctx, username); prov != nil {
			return prov
		}
	}
	return s.provider
}

// Search 对话式语义检索（自然语言 → 结构化过滤 + 全文+向量混合检索）
// indexer 不可用时降级为 MySQL LIKE filename 模糊搜索。
func (s *AIService) Search(ctx context.Context, username, q string, page, size int) ([]SearchResult, error) {
	if q == "" {
		return []SearchResult{}, nil
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	// 1. 解析查询
	filter := ai.ParseQuery(q)

	// 2. 构建向量（用过滤后的语义词）
	vector, err := s.providerFor(ctx, username).Embed(ctx, filter.SemanticQuery)
	if err != nil {
		slog.WarnContext(ctx, "ai search: embed failed, fallback LIKE", "error", err)
		return s.fallbackLike(ctx, username, q, page, size), nil
	}

	// 3. 混合检索（indexer 为 nil 时降级 LIKE）
	if s.indexer == nil {
		return s.fallbackLike(ctx, username, q, page, size), nil
	}
	docs, err := s.indexer.SearchHybrid(ctx, filter.SemanticQuery, username, vector, filter.TypeFilter, page, size)
	if err != nil {
		slog.WarnContext(ctx, "ai search: hybrid search failed, fallback LIKE", "error", err)
		return s.fallbackLike(ctx, username, q, page, size), nil
	}

	out := make([]SearchResult, 0, len(docs))
	for _, d := range docs {
		out = append(out, SearchResult{
			Filehash: d.Filehash,
			Filename: d.Filename,
			Summary:  d.Summary,
			Tags:     strings.Join(d.Tags, ","),
			Size:     d.Size,
			Score:    d.Score,
		})
	}
	return out, nil
}

// fallbackLike 降级：MySQL LIKE filename 模糊搜索
func (s *AIService) fallbackLike(ctx context.Context, username, q string, page, size int) []SearchResult {
	files, err := s.fileRepo.ListByUser(username)
	if err != nil {
		return []SearchResult{}
	}
	lower := strings.ToLower(q)
	var matched []SearchResult
	for _, f := range files {
		if strings.Contains(strings.ToLower(f.FileName), lower) ||
			strings.Contains(strings.ToLower(f.Summary), lower) {
			matched = append(matched, SearchResult{
				Filehash: f.FileSha1,
				Filename: f.FileName,
				Summary:  f.Summary,
				Tags:     f.Tags,
				Size:     f.FileSize,
			})
		}
	}
	start := (page - 1) * size
	if start >= len(matched) {
		return []SearchResult{}
	}
	end := start + size
	if end > len(matched) {
		end = len(matched)
	}
	return matched[start:end]
}

// Similar 相似文件推荐（向量 KNN，排除自身）
func (s *AIService) Similar(ctx context.Context, username, filehash string, limit int) ([]SearchResult, error) {
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	// 验证所有权
	fMeta, err := s.fileRepo.GetByHash(filehash, username)
	if err != nil {
		return nil, fmt.Errorf("file not found or no permission")
	}
	vector, err := s.providerFor(ctx, username).Embed(ctx, fMeta.FileName+" "+fMeta.Summary)
	if err != nil {
		return nil, fmt.Errorf("embed failed: %w", err)
	}
	docs, err := s.indexer.Similar(ctx, username, vector, filehash, limit)
	if err != nil {
		return nil, fmt.Errorf("similar search failed: %w", err)
	}
	out := make([]SearchResult, 0, len(docs))
	for _, d := range docs {
		out = append(out, SearchResult{
			Filehash: d.Filehash,
			Filename: d.Filename,
			Summary:  d.Summary,
			Tags:     strings.Join(d.Tags, ","),
			Size:     d.Size,
			Score:    d.Score,
		})
	}
	return out, nil
}

// Duplicates 近似重复检测（相似度 > 阈值的文件）
func (s *AIService) Duplicates(ctx context.Context, username, filehash string, threshold float64) ([]SearchResult, error) {
	if threshold <= 0 {
		threshold = 0.9
	}
	// 候选：取较多，再按阈值过滤
	candidates, err := s.Similar(ctx, username, filehash, 20)
	if err != nil {
		return nil, err
	}
	var dupes []SearchResult
	for _, c := range candidates {
		if c.Score >= threshold {
			dupes = append(dupes, c)
		}
	}
	return dupes, nil
}
