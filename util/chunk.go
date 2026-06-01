package util

import (
	"filestore-server/rd"
)

// AddChunk 记录 chunk 上传成功
func AddChunk(filehash string, index int) error {
	key := "chunk:" + filehash
	return rd.RDB.SAdd(rd.Ctx, key, index).Err()
}

// GetUploadedChunks 获取已上传的 chunk 列表
func GetUploadedChunks(filehash string) ([]string, error) {
	key := "chunk:" + filehash
	return rd.RDB.SMembers(rd.Ctx, key).Result()
}

// ChunkExists 判断 chunk 是否已存在
func ChunkExists(filehash string, index int) bool {
	key := "chunk:" + filehash
	res, _ := rd.RDB.SIsMember(rd.Ctx, key, index).Result()
	return res
}

// ClearChunks 删除 chunk 记录
func ClearChunks(filehash string) {
	key := "chunk:" + filehash
	rd.RDB.Del(rd.Ctx, key)
}
