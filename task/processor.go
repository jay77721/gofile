package task

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
	"gofile/ai"
)

// AITaskProcessor Asynq handler：解包 Payload → 调用 ai.Processor.ProcessOne
// task 包依赖 ai 包，ai 包通过 TaskEnqueuer 接口依赖 task.Client，无循环引用
type AITaskProcessor struct {
	proc *ai.Processor
}

// NewAITaskProcessor 创建 Asynq AI 分析任务处理器
func NewAITaskProcessor(proc *ai.Processor) *AITaskProcessor {
	return &AITaskProcessor{proc: proc}
}

// ProcessTask 实现 asynq.Handler 接口
// 返回 error 时 Asynq 按 MaxRetry 自动重试（指数退避）
func (p *AITaskProcessor) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var payload AIAnalyzePayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		// 解析失败是不可重试的错误（Payload 损坏），用 asynq.SkipRetry 包装
		return fmt.Errorf("%w: unmarshal ai analyze payload: %v", asynq.SkipRetry, err)
	}

	return p.proc.ProcessOne(ctx, payload.Filehash, payload.Filename, payload.Username)
}
