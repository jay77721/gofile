package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gofile/internal/observability/metrics"
	"gofile/internal/port"
)

// OpenAIProvider is a Provider implementation using the OpenAI-compatible API
// Supports custom baseURL and can connect to any OpenAI-compatible endpoint (OpenAI / DeepSeek / Ollama / vLLM / OneAPI, etc.)
type OpenAIProvider struct {
	apiKey     string
	baseURL    string
	model      string
	embedModel string
	httpClient *http.Client
	dim        int
}

// NewOpenAIProvider creates an OpenAI-compatible Provider
// Uses the official endpoint when baseURL is empty; uses text-embedding-3-small when embedModel is empty
func NewOpenAIProvider(apiKey, baseURL, model, embedModel string, dim int) port.Provider {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	if embedModel == "" {
		embedModel = "text-embedding-3-small"
	}
	return &OpenAIProvider{
		apiKey:     apiKey,
		baseURL:    baseURL,
		model:      model,
		embedModel: embedModel,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		dim: dim,
	}
}

// NewOpenAIProviderWithTimeout creates an OpenAI-compatible Provider with configurable HTTP timeout (use short timeout for testing connections)
func NewOpenAIProviderWithTimeout(apiKey, baseURL, model, embedModel string, dim int, timeout time.Duration) port.Provider {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	if embedModel == "" {
		embedModel = "text-embedding-3-small"
	}
	return &OpenAIProvider{
		apiKey:     apiKey,
		baseURL:    baseURL,
		model:      model,
		embedModel: embedModel,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		dim: dim,
	}
}

func (p *OpenAIProvider) Dimension() int { return p.dim }

func (p *OpenAIProvider) Analyze(ctx context.Context, fileName, content string) (*port.Analysis, error) {
	start := time.Now()
	defer func() {
		metrics.ObserveLLMDuration("analyze", time.Since(start).Seconds())
	}()

	prompt := fmt.Sprintf(`基于以下文件名和内容片段，生成一个简洁的中文摘要（不超过100字）和3-5个中文标签。
只返回 JSON 格式：{"summary": "...", "tags": ["...", "..."]}

文件名：%s
内容：%s`, fileName, truncate(content, 2000))

	body := map[string]any{
		"model": p.model,
		"messages": []map[string]string{
			{"role": "system", "content": "你是一个文件分析助手，只返回 JSON 格式。"},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.3,
	}
	resp, err := p.doRequest(ctx, "/chat/completions", body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(resp, &result); err != nil || len(result.Choices) == 0 {
		return nil, fmt.Errorf("openai: parse response failed: %w", err)
	}

	var analysis port.Analysis
	content_str := strings.TrimSpace(result.Choices[0].Message.Content)
	// Strip possible markdown code blocks
	content_str = strings.TrimPrefix(content_str, "```json")
	content_str = strings.TrimPrefix(content_str, "```")
	content_str = strings.TrimSuffix(content_str, "```")
	content_str = strings.TrimSpace(content_str)
	if err := json.Unmarshal([]byte(content_str), &analysis); err != nil {
		return nil, fmt.Errorf("openai: parse analysis JSON failed: %w", err)
	}
	return &analysis, nil
}

func (p *OpenAIProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	start := time.Now()
	defer func() {
		metrics.ObserveLLMDuration("embed", time.Since(start).Seconds())
	}()

	body := map[string]any{
		"model": p.embedModel,
		"input": text,
	}
	// OpenAI text-embedding-3 series supports dimensions truncation, keeping it consistent with the search engine dimension
	// If a third-party compatible endpoint does not support this parameter, it will be exposed during connection testing/search and a fallback hint will be shown
	if p.dim > 0 {
		body["dimensions"] = p.dim
	}
	resp, err := p.doRequest(ctx, "/embeddings", body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &result); err != nil || len(result.Data) == 0 {
		return nil, fmt.Errorf("openai: parse embedding failed: %w", err)
	}

	vec := make([]float32, len(result.Data[0].Embedding))
	for i, v := range result.Data[0].Embedding {
		vec[i] = float32(v)
	}
	return vec, nil
}

func (p *OpenAIProvider) doRequest(ctx context.Context, path string, body any) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+path, strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf("openai: create request failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai: read response failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai: API error (status %d): %s", resp.StatusCode, string(respBody))
	}
	return respBody, nil
}

func truncate(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}
