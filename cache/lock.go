package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// AcquireLock 尝试获取分布式锁（SETNX + TTL）
// key: 锁的唯一标识，如 "gofile:lock:merge:<hash>"
// token: 随机字符串，用于释放时验证身份
// ttl: 锁的自动过期时间
// 返回 true 表示获取成功
func (c *Client) AcquireLock(ctx context.Context, key, token string, ttl time.Duration) (bool, error) {
	ok, err := c.rdb.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

// ReleaseLock 释放分布式锁（Lua CAS：仅当 token 匹配时才删除，防止误删他人的锁）
func (c *Client) ReleaseLock(ctx context.Context, key, token string) error {
	script := redis.NewScript(`
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		end
		return 0
	`)
	return script.Run(ctx, c.rdb, []string{key}, token).Err()
}