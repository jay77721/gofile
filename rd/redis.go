package rd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	RDB *redis.Client
	Ctx = context.Background()
)

// InitRedis 初始化 Redis 连接池
func InitRedis(addr, password string, db int) error {
	RDB = redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		PoolSize:     20,
		MinIdleConns: 5,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	_, err := RDB.Ping(Ctx).Result()
	if err != nil {
		return fmt.Errorf("redis connect failed: %w", err)
	}

	slog.Info("Redis connected", "addr", addr)
	return nil
}

// SetFileHash 记录文件 hash 到 Redis（秒传缓存）
func SetFileHash(hash string, location string) {
	key := "file:" + hash
	RDB.Set(Ctx, key, location, 0)
}

// GetFileHash 查询文件 hash（秒传检测）
func GetFileHash(hash string) (string, error) {
	key := "file:" + hash
	return RDB.Get(Ctx, key).Result()
}

// HealthCheck 检查 Redis 连通性
func HealthCheck() error {
	return RDB.Ping(Ctx).Err()
}
