package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"gofile/internal/infrastructure/ai"
	"gofile/internal/port"
)

// SearchResult is a single AI search result.
type SearchResult struct {
	Filehash string  `json:"filehash"`
	Filename string  `json:"filename"`
	Summary  string  `json:"summary"`
	Tags     string  `json:"tags"`
	Size     int64   `json:"size"`
	Score    float64 `json:"score"`
}

// AIService orchestrates AI semantic search (search / similar recommendations / duplicate detection).
type AIService struct {
	indexer  port.Indexer
	provider port.Provider
	fileRepo port.FileRepository

	// resolve resolves the effective Provider by username (user custom config takes precedence); nil means use default provider
	resolve func(ctx context.Context, username string) port.Provider
}

// NewAIService creates the AI search service.
func NewAIService(indexer port.Indexer, provider port.Provider, fileRepo port.FileRepository) *AIService {
	return &AIService{indexer: indexer, provider: provider, fileRepo: fileRepo}
}

// WithResolver injects the function that resolves Provider per user (user-level AI config).
func (s *AIService) WithResolver(fn func(ctx context.Context, username string) port.Provider) *AIService {
	s.resolve = fn
	return s
}

// providerFor resolves the effective Provider for the current user, falling back to default if not found.
func (s *AIService) providerFor(ctx context.Context, username string) port.Provider {
	if s.resolve != nil {
		if prov := s.resolve(ctx, username); prov != nil {
			return prov
		}
	}
	return s.provider
}

// Search performs conversational semantic search (natural language -> structured filters + hybrid full-text+vector search).
// Falls back to MySQL LIKE on filename when indexer is unavailable.
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

	// 1. parse query
	filter := ai.ParseQuery(q)

	// 2. build vector (using filtered semantic query)
	vector, err := s.providerFor(ctx, username).Embed(ctx, filter.SemanticQuery)
	if err != nil {
		slog.WarnContext(ctx, "ai search: embed failed, fallback LIKE", "error", err)
		return s.fallbackLike(ctx, username, q, page, size), nil
	}

	// 3. hybrid search (fallback to LIKE when indexer is nil)
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

// fallbackLike is the fallback: MySQL LIKE fuzzy search on filename.
func (s *AIService) fallbackLike(ctx context.Context, username, q string, page, size int) []SearchResult {
	files, err := s.fileRepo.ListByUser(ctx, username)
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

// Similar recommends similar files (vector KNN, excluding itself).
func (s *AIService) Similar(ctx context.Context, username, filehash string, limit int) ([]SearchResult, error) {
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	// verify ownership
	fMeta, err := s.fileRepo.GetByHash(ctx, filehash, username)
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

// Duplicates detects near-duplicates (files with similarity > threshold).
func (s *AIService) Duplicates(ctx context.Context, username, filehash string, threshold float64) ([]SearchResult, error) {
	if threshold <= 0 {
		threshold = 0.9
	}
	// candidates: fetch more, then filter by threshold
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
