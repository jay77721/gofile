package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client 封装 go-redis 客户端
type Client struct {
	rdb *redis.Client
}

// New 创建 Redis 客户端，连接失败返回 err
func New(addr, password string, db int) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return &Client{rdb: rdb}, nil
}

// Ping 健康检查
func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

// Close 关闭连接池
func (c *Client) Close() error {
	return c.rdb.Close()
}

// Rdb 返回底层 redis.Client，供 ratelimit 等模块直接使用 Lua 脚本
func (c *Client) Rdb() *redis.Client {
	return c.rdb
}