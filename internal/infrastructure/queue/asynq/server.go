package task

import (
	"context"
	"log/slog"

	"github.com/hibiken/asynq"
)

// NewServer creates an Asynq consumer service (AI-dedicated queue, concurrency = AIWorkers)
//
// Queue priority: ai(6) > default(3) > low(1), strictly weighted priority
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
			// Multi-queue weighted priority: ai queue has the highest weight
			Queues: map[string]int{
				"ai":      6,
				"default": 3,
				"low":     1,
			},
			Concurrency: concurrency,
			// Error handling: log errors (retries are built into Asynq, only final failures are logged here)
			ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, t *asynq.Task, err error) {
				slog.Error("asynq task permanently failed",
					"type", t.Type(),
					"error", err,
					"payload", string(t.Payload()),
				)
			}),
			// Log level: Error only (reduce Info log noise)
			Logger: newAsynqLogger(),
		},
	)
}

// asynqLogger adapts the Asynq Logger interface to slog
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
