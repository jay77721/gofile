package cache

import "context"

// fileHashKey Redis Set 的键名，存储已上传文件的 SHA1 集合
const fileHashKey = "gofile:filehashes"

// HashExists 检查 hash 是否在已知文件集合中（O(1) 快速判断）
func (c *Client) HashExists(ctx context.Context, hash string) (bool, error) {
	return c.rdb.SIsMember(ctx, fileHashKey, hash).Result()
}

// MarkHash 将 hash 加入已知文件集合（幂等，重复添加无副作用）
func (c *Client) MarkHash(ctx context.Context, hash string) error {
	return c.rdb.SAdd(ctx, fileHashKey, hash).Err()
}
