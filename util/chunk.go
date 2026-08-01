package util

import (
	"os"
	"path/filepath"
	"strconv"
)

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

