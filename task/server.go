package task

import (
	"context"
	"log/slog"

	"github.com/hibiken/asynq"
)

// NewServer 创建 Asynq 消费者服务（AI 专用队列，concurrency = AIWorkers）
//
// 队列优先级：ai(6) > default(3) > low(1)，加权严格优先
func NewServer(redisAddr, redisPassword string, redisDB, concurrency int) *asynq.Server {
	if concurrency <= 0 {
		concurrency = 4
	}
	return asynq.NewServer(
		asynq.RedisClientOpt{
			Addr:     redisAddr,
			Password: redisPassword,
			DB:       redisDB,
		},
		asynq.Config{
			// 多队列加权优先：ai 队列享受最高权重
			Queues: map[string]int{
				"ai":      6,
				"default": 3,
				"low":     1,
			},
			Concurrency: concurrency,
			// 错误处理：打日志（重试由 Asynq 内置，这里只记录最终失败）
			ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, t *asynq.Task, err error) {
				slog.Error("asynq task permanently failed",
					"type", t.Type(),
					"error", err,
					"payload", string(t.Payload()),
				)
			}),
			// 日志级别：仅 Error（减少 Info 日志噪声）
			Logger: newAsynqLogger(),
		},
	)
}

// asynqLogger 适配 Asynq 的 Logger 接口，桥接到 slog
type asynqLogger struct{}

func newAsynqLogger() *asynqLogger { return &asynqLogger{} }

func (l *asynqLogger) Debug(args ...any) {}
func (l *asynqLogger) Info(args ...any)  {}
func (l *asynqLogger) Warn(args ...any) {
	slog.Warn("asynq", "msg", args)
}
func (l *asynqLogger) Error(args ...any) {
	slog.Error("asynq", "msg", args)
}
func (l *asynqLogger) Fatal(args ...any) {
	slog.Error("asynq fatal", "msg", args)
}
