package task

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
	"gofile/internal/infrastructure/ai"
)

// AITaskProcessor is an Asynq handler: unpack Payload -> call ai.Processor.ProcessOne
// task package depends on ai, ai depends on task.Client via TaskEnqueuer interface, no circular dependency
type AITaskProcessor struct {
	proc *ai.Processor
}

// NewAITaskProcessor creates an Asynq AI analysis task processor
func NewAITaskProcessor(proc *ai.Processor) *AITaskProcessor {
	return &AITaskProcessor{proc: proc}
}

// ProcessTask implements the asynq.Handler interface
// When an error is returned, Asynq automatically retries according to MaxRetry (exponential backoff)
func (p *AITaskProcessor) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var payload AIAnalyzePayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		// Parse failure is a non-retryable error (corrupted Payload), wrap with asynq.SkipRetry
		return fmt.Errorf("%w: unmarshal ai analyze payload: %v", asynq.SkipRetry, err)
	}

	return p.proc.ProcessOne(ctx, payload.Filehash, payload.Filename, payload.Username)
}
