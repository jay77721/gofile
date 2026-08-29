package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// AcquireLock tries to acquire a distributed lock (SETNX + TTL)
// key: unique lock identifier, e.g., "gofile:lock:merge:<hash>"
// token: random string for verifying identity on release
// ttl: automatic expiration time for the lock
// Returns true if acquisition succeeds
func (c *Client) AcquireLock(ctx context.Context, key, token string, ttl time.Duration) (bool, error) {
	ok, err := c.rdb.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

// ReleaseLock releases the distributed lock (Lua CAS: only deletes when token matches, preventing accidental deletion of another holder's lock)
func (c *Client) ReleaseLock(ctx context.Context, key, token string) error {
	script := redis.NewScript(`
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		end
		return 0
	`)
	return script.Run(ctx, c.rdb, []string{key}, token).Err()
}
