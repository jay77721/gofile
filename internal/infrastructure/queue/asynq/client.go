package task

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

// Client Asynq 生产者封装，投递 AI 分析任务到 Redis 持久化队列
type Client struct {
	c *asynq.Client
}

// NewClient 创建 Asynq 生产者客户端
func NewClient(redisAddr, redisPassword string, redisDB int) *Client {
	return &Client{
		c: asynq.NewClient(asynq.RedisClientOpt{
			Addr:     redisAddr,
			Password: redisPassword,
			DB:       redisDB,
		}),
	}
}

// Enqueue 投递 AI 分析任务（非阻塞，由 ai.Processor 在 Asynq 不可用时降级到内存 chan）
// 实现 port.TaskEnqueuer 接口
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
		// Retention: 任务完成后保留 24h，供 Asynq Inspector 查看
		asynq.Retention(24*time.Hour),
	)

	_, err = c.c.EnqueueContext(ctx, t,
		// 已在队列/处理中时不重复投递（幂等）
		asynq.TaskID(username+":"+filehash),
	)
	if err != nil && err != asynq.ErrTaskIDConflict {
		return fmt.Errorf("asynq enqueue: %w", err)
	}
	return nil
}

// Close 关闭客户端连接
func (c *Client) Close() error {
	return c.c.Close()
}
