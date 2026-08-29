package chunkutil

import (
	"os"
	"path/filepath"
	"strconv"
)

// GetUploadedChunks gets the list of uploaded chunks (isolated per user).
func GetUploadedChunks(chunkDir, username, filehash string) ([]string, error) {
	dir := filepath.Join(chunkDir, filepath.Base(username), filepath.Base(filehash))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var chunks []string
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		chunks = append(chunks, e.Name())
	}
	return chunks, nil
}

// ChunkExists checks whether a chunk already exists (isolated per user).
func ChunkExists(chunkDir, username, filehash string, index int) bool {
	chunkPath := filepath.Join(chunkDir, filepath.Base(username), filepath.Base(filehash), strconv.Itoa(index))
	_, err := os.Stat(chunkPath)
	return err == nil
}
