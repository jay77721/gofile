package util

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	// ChunkTTL chunk 目录在磁盘上的保留时间
	ChunkTTL = 24 * time.Hour
)

// AddChunk 记录 chunk 上传成功（磁盘基础，无需额外操作）
func AddChunk(filehash string, index int) error {
	return nil
}

// GetUploadedChunks 获取已上传的 chunk 列表
func GetUploadedChunks(chunkDir, filehash string) ([]string, error) {
	dir := filepath.Join(chunkDir, filehash)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var chunks []string
	for _, e := range entries {
		chunks = append(chunks, e.Name())
	}
	return chunks, nil
}

// ChunkExists 判断 chunk 是否已存在
func ChunkExists(chunkDir, filehash string, index int) bool {
	chunkPath := filepath.Join(chunkDir, filehash, strconv.Itoa(index))
	_, err := os.Stat(chunkPath)
	return err == nil
}

// ClearChunks 删除 chunk 记录
func ClearChunks(filehash string) {
	// 空操作：磁盘目录在 MergeChunkHandler 中已通过 os.RemoveAll 清理
}