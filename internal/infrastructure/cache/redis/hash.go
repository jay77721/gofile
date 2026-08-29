package cache

import "context"

// fileHashKey is the Redis Set key that stores the SHA1 set of uploaded files
const fileHashKey = "gofile:filehashes"

// HashExists checks whether a hash is in the known file set (O(1) fast check)
func (c *Client) HashExists(ctx context.Context, hash string) (bool, error) {
	return c.rdb.SIsMember(ctx, fileHashKey, hash).Result()
}

// MarkHash adds a hash to the known file set (idempotent, duplicate adds have no side effects)
func (c *Client) MarkHash(ctx context.Context, hash string) error {
	return c.rdb.SAdd(ctx, fileHashKey, hash).Err()
}
