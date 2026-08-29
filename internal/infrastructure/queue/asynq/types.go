package task

import "time"

// TypeFileAIAnalyze is the AI file analysis task type constant
const TypeFileAIAnalyze = "file:ai:analyze"

// AIAnalyzePayload is the AI analysis task payload
type AIAnalyzePayload struct {
	Filehash string `json:"filehash"`
	Filename string `json:"filename"`
	Username string `json:"username"`
}

// defaultTimeout is the maximum execution time per task (LLM calls may be slow, 5 minutes)
const defaultTimeout = 5 * time.Minute

// maxRetry is the maximum number of retries (aligned with maxRetry constant in ai/processor.go)
const maxRetry = 3
