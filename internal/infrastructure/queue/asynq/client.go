package task

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

// Client is an Asynq producer wrapper that dispatches AI analysis tasks to a Redis-persisted queue
type Client struct {
	c *asynq.Client
}

// NewClient creates an Asynq producer client
func NewClient(redisAddr, redisPassword string, redisDB int) *Client {
	return &Client{
		c: asynq.NewClient(asynq.RedisClientOpt{
			Addr:     redisAddr,
			Password: redisPassword,
			DB:       redisDB,
		}),
	}
}

// Enqueue dispatches an AI analysis task (non-blocking, falls back to in-memory chan via ai.Processor when Asynq is unavailable)
// Implements the port.TaskEnqueuer interface
func (c *Client) Enqueue(ctx context.Context, filehash, filename, username string) error {
	payload, err := json.Marshal(AIAnalyzePayload{
		Filehash: filehash,
		Filename: filename,
		Username: username,
	})
	if err != nil {
		return fmt.Errorf("marshal ai analyze payload: %w", err)
	}

	t := asynq.NewTask(TypeFileAIAnalyze, payload,
		asynq.MaxRetry(maxRetry),
		asynq.Timeout(defaultTimeout),
		asynq.Queue("ai"),
		// Retention: keep task for 24h after completion for Asynq Inspector
		asynq.Retention(24*time.Hour),
	)

	_, err = c.c.EnqueueContext(ctx, t,
		// Do not re-enqueue if already queued/processing (idempotent)
		asynq.TaskID(username+":"+filehash),
	)
	if err != nil && err != asynq.ErrTaskIDConflict {
		return fmt.Errorf("asynq enqueue: %w", err)
	}
	return nil
}

// Close closes the client connection
func (c *Client) Close() error {
	return c.c.Close()
}
