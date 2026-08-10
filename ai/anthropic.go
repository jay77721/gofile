package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gofile/metrics"
)

// AnthropicProvider Anthropic API 实现的 Provider
type AnthropicProvider struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
	dim        int
}

// NewAnthropicProvider 创建 Anthropic Provider
func NewAnthropicProvider(apiKey, model string, dim int) Provider {
	if model == "" {
		model = "claude-3-haiku-20240307"
	}
	return &AnthropicProvider{
		apiKey:  apiKey,
		baseURL: "https://api.anthropic.com/v1",
		model:   model,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		dim: dim,
	}
}

func (p *AnthropicProvider) Dimension() int { return p.dim }

func (p *AnthropicProvider) Analyze(ctx context.Context, fileName, content string) (*Analysis, error) {
	start := time.Now()
	defer func() {
		metrics.ObserveLLMDuration("analyze", time.Since(start).Seconds())
	}()

	prompt := fmt.Sprintf(`基于以下文件名和内容片段，生成一个简洁的中文摘要（不超过100字）和3-5个中文标签。
只返回 JSON 格式：{"summary": "...", "tags": ["...", "..."]}

文件名：%s
内容：%s`, fileName, truncate(content, 2000))

	body := map[string]any{
		"model":      p.model,
		"max_tokens": 512,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	resp, err := p.doRequest(ctx, "/messages", body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(resp, &result); err != nil || len(result.Content) == 0 {
		return nil, fmt.Errorf("anthropic: parse response failed: %w", err)
	}

	var analysis Analysis
	content_str := strings.TrimSpace(result.Content[0].Text)
	content_str = strings.TrimPrefix(content_str, "```json")
	content_str = strings.TrimPrefix(content_str, "```")
	content_str = strings.TrimSuffix(content_str, "```")
	content_str = strings.TrimSpace(content_str)
	if err := json.Unmarshal([]byte(content_str), &analysis); err != nil {
		return nil, fmt.Errorf("anthropic: parse analysis JSON failed: %w", err)
	}
	return &analysis, nil
}

func (p *AnthropicProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	// Anthropic 不直接提供 embedding API，回退到 hash 向量
	// 建议搭配 Voyage AI 或 OpenAI embedding
	return deterministicVector(text, p.dim), nil
}

func (p *AnthropicProvider) doRequest(ctx context.Context, path string, body any) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+path, strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf("anthropic: create request failed: %w", err)
	}
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: read response failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: API error (status %d): %s", resp.StatusCode, string(respBody))
	}
	return respBody, nil
}
