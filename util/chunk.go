package util

import (
	"os"
	"path/filepath"
	"strconv"
)

// GetUploadedChunks 获取已上传的 chunk 列表（按用户隔离）
func GetUploadedChunks(chunkDir, username, filehash string) ([]string, error) {
	dir := filepath.Join(chunkDir, filepath.Base(username), filepath.Base(filehash))
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

// ChunkExists 判断 chunk 是否已存在（按用户隔离）
func ChunkExists(chunkDir, username, filehash string, index int) bool {
	chunkPath := filepath.Join(chunkDir, filepath.Base(username), filepath.Base(filehash), strconv.Itoa(index))
	_, err := os.Stat(chunkPath)
	return err == nil
}