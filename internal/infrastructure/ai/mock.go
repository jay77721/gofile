package ai

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	"gofile/internal/port"
)

// MockProvider 基于规则的 mock 实现（无 LLM 时可跑通全流程，行为确定性、可测试）
//
// Analyze：summary = 文件名 + ": " + 前 200 字符；tags = 扩展名规则 + 文件名关键词。
// Embed：从文本 hash 生成确定性定长向量（同一文本 → 同一向量），不调用任何外部服务。
type MockProvider struct {
	dim int
}

// NewMockProvider 创建 mock provider，dim 为向量维度
func NewMockProvider(dim int) port.Provider {
	if dim <= 0 {
		dim = 128
	}
	return &MockProvider{dim: dim}
}

func (m *MockProvider) Dimension() int { return m.dim }

func (m *MockProvider) Analyze(_ context.Context, fileName, content string) (*port.Analysis, error) {
	summary := buildSummary(fileName, content)
	tags := buildTags(fileName, content)
	return &port.Analysis{Summary: summary, Tags: tags}, nil
}

func (m *MockProvider) Embed(_ context.Context, text string) ([]float32, error) {
	return deterministicVector(text, m.dim), nil
}

// buildSummary 生成 mock 摘要
func buildSummary(fileName, content string) string {
	const maxLen = 200
	preview := content
	if len(preview) > maxLen {
		preview = preview[:maxLen]
	}
	preview = strings.TrimSpace(preview)
	if preview == "" {
		return fileName
	}
	return fileName + ": " + preview
}

// buildTags 基于扩展名 + 文件名关键词生成标签
func buildTags(fileName, content string) []string {
	ext := strings.ToLower(getExt(fileName))
	var tags []string

	switch ext {
	case ".pdf":
		tags = append(tags, "文档")
	case ".doc", ".docx":
		tags = append(tags, "文档")
	case ".xls", ".xlsx", ".csv":
		tags = append(tags, "表格")
	case ".ppt", ".pptx":
		tags = append(tags, "演示")
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg", ".bmp":
		tags = append(tags, "图片")
	case ".mp4", ".webm", ".mov", ".avi":
		tags = append(tags, "视频")
	case ".mp3", ".wav", ".ogg", ".flac":
		tags = append(tags, "音频")
	case ".zip", ".tar", ".gz", ".rar", ".7z":
		tags = append(tags, "压缩包")
	case ".go", ".py", ".js", ".ts", ".java", ".rs", ".c", ".cpp", ".swift", ".kt", ".php", ".rb":
		tags = append(tags, "代码")
	case ".txt", ".md", ".log", ".json", ".xml", ".yaml", ".yml", ".html", ".htm", ".css", ".sql", ".conf", ".ini", ".toml", ".env":
		tags = append(tags, "文本")
	default:
		// 按 MIME 推断
		mt := mimetype.Lookup(ext)
		if mt != nil {
			switch {
			case strings.HasPrefix(mt.String(), "image/"):
				tags = append(tags, "图片")
			case strings.HasPrefix(mt.String(), "video/"):
				tags = append(tags, "视频")
			case strings.HasPrefix(mt.String(), "audio/"):
				tags = append(tags, "音频")
			case strings.HasPrefix(mt.String(), "text/"):
				tags = append(tags, "文本")
			default:
				tags = append(tags, "其他")
			}
		} else {
			tags = append(tags, "其他")
		}
	}

	// 文件名关键词补充标签
	lower := strings.ToLower(fileName)
	for kw, tag := range keywordTags {
		if strings.Contains(lower, kw) {
			tags = append(tags, tag)
		}
	}

	return uniqueTags(tags)
}

// keywordTags 文件名关键词 → 标签映射
var keywordTags = map[string]string{
	"报告":       "报告",
	"report":   "报告",
	"合同":       "合同",
	"contract": "合同",
	"发票":       "发票",
	"invoice":  "发票",
	"简历":       "简历",
	"resume":   "简历",
	"预算":       "预算",
	"budget":   "预算",
	"工资":       "工资",
	"salary":   "工资",
	"会议":       "会议",
	"meeting":  "会议",
	"计划":       "计划",
	"plan":     "计划",
	"备份":       "备份",
	"backup":   "备份",
}

func uniqueTags(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, t := range in {
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

// deterministicVector 从文本 hash 生成确定性定长向量（不依赖 math/rand，测试可重复）
func deterministicVector(text string, dim int) []float32 {
	v := make([]float32, dim)
	h := fnv.New64a()
	fmt.Fprint(h, text)
	for i := 0; i < dim; i++ {
		// 每个维度用独立 hash 种子，映射到 [-1, 1)
		h.Reset()
		fmt.Fprintf(h, "%s:%d", text, i)
		v[i] = float32(h.Sum64()%2000)/1000.0 - 1.0
	}
	return v
}

func getExt(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			return name[i:]
		}
	}
	return ""
}
