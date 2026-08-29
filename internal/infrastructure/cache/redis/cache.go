package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client wraps the go-redis client
type Client struct {
	rdb *redis.Client
}

// New creates a Redis client, returns err on connection failure
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

// Ping performs a health check
func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

// Close closes the connection pool
func (c *Client) Close() error {
	return c.rdb.Close()
}

// Rdb returns the underlying redis.Client for direct Lua script usage by modules such as rate limiting
func (c *Client) Rdb() *redis.Client {
	return c.rdb
}
