package task

import "time"

// TypeFileAIAnalyze AI 文件分析任务类型常量
const TypeFileAIAnalyze = "file:ai:analyze"

// AIAnalyzePayload AI 分析任务 Payload
type AIAnalyzePayload struct {
	Filehash string `json:"filehash"`
	Filename string `json:"filename"`
	Username string `json:"username"`
}

// defaultTimeout 单任务最长执行时间（LLM 调用可能较慢，给 5 分钟）
const defaultTimeout = 5 * time.Minute

// maxRetry 最大重试次数（与 ai/processor.go 中 maxRetry 常量对齐）
const maxRetry = 3
