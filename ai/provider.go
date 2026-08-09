// Package ai 提供文件内容理解能力：文本提取、LLM 分析（摘要+标签）、向量化、
// 语义检索（Typesense）、异步任务编排。
//
// 架构不变量：MySQL 是真相源，Typesense 是派生索引；AI 功能永不阻塞/反噬上传下载主链路。
package ai

import "context"

// Analysis LLM 一次调用返回的结构化结果：摘要 + 标签
type Analysis struct {
	Summary string   `json:"summary"`
	Tags    []string `json:"tags"`
}

// Provider 文件内容 AI 能力抽象（厂商无关）
//
// 真实实现（anthropic.go / openai.go）由 AIProvider 配置切换；本 Phase 只提供 mock。
type Provider interface {
	// Analyze 基于文件名与内容片段，一次调用返回摘要与标签
	Analyze(ctx context.Context, fileName, content string) (*Analysis, error)
	// Embed 将文本转为定长向量（用于语义检索）
	Embed(ctx context.Context, text string) ([]float32, error)
	// Dimension 返回 Embed 输出的向量维度（创建检索索引时需要）
	Dimension() int
}
