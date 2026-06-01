package handler

import (
	"encoding/json"
	"filestore-server/meta"
	"filestore-server/rd"
	"filestore-server/util"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

const (
	MaxUploadSize = 100 << 20 // 100MB
)

// HealthCheckHandler 健康检查端点
func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	if err := rd.HealthCheck(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, 1, "redis unavailable", nil)
		return
	}
	writeJSON(w, http.StatusOK, 0, "ok", nil)
}

// UploadHandler 处理文件上传
func UploadHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		http.ServeFile(w, r, "./static/view/index.html")
	case "POST":
		// 限制上传大小
		r.Body = http.MaxBytesReader(w, r.Body, MaxUploadSize)

		fileHash := r.FormValue("filehash")
		// 秒传检测
		if fileHash != "" {
			if TryFastUploadHandler(fileHash) {
				writeJSON(w, http.StatusOK, 0, "秒传成功", nil)
				return
			}
		}

		// 解析上传的文件
		file, header, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, 1, "文件获取失败", nil)
			return
		}
		defer file.Close()

		// 路径穿越防护：只取文件名
		filename := filepath.Base(header.Filename)
		dstPath := filepath.Join("./uploads", filename)

		loc, _ := time.LoadLocation("Asia/Shanghai")
		now := time.Now().In(loc)

		fileMeta := meta.FileMeta{
			FileName: filename,
			Location: dstPath,
			UploadAt: now,
		}

		dst, err := os.Create(fileMeta.Location)
		if err != nil {
			slog.Error("create file failed", "error", err, "filename", filename)
			writeJSON(w, http.StatusInternalServerError, 1, "文件创建失败", nil)
			return
		}
		defer dst.Close()

		fileMeta.FileSize, err = io.Copy(dst, file)
		if err != nil {
			slog.Error("copy file failed", "error", err, "filename", filename)
			writeJSON(w, http.StatusInternalServerError, 1, "文件上传失败", nil)
			return
		}

		dst.Seek(0, 0)
		fileMeta.FileSha1 = util.FileSha1(dst)

		if ok := meta.UpdateFileMetaDB(fileMeta); !ok {
			slog.Warn("save file meta failed", "filehash", fileMeta.FileSha1)
		}

		slog.Info("file uploaded", "filename", filename, "size", fileMeta.FileSize, "hash", fileMeta.FileSha1)
		writeJSON(w, http.StatusOK, 0, "上传成功", map[string]string{
			"filehash": fileMeta.FileSha1,
		})
	}
}

// UploadSucHandler 上传成功页面
func UploadSucHandler(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, "Upload finished")
}

// GetFileHandler 获取文件元信息
func GetFileHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	filehash := r.Form.Get("filehash")
	if filehash == "" {
		writeJSON(w, http.StatusBadRequest, 1, "缺少 filehash 参数", nil)
		return
	}

	fMeta, err := meta.GetFileMetaDB(filehash)
	if err != nil {
		writeJSON(w, http.StatusNotFound, 1, "文件不存在", nil)
		return
	}

	data, err := json.Marshal(fMeta)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, 1, "数据序列化失败", nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// DownloadHandler 下载文件
func DownloadHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	filehash := r.Form.Get("filehash")
	if filehash == "" {
		writeJSON(w, http.StatusBadRequest, 1, "缺少 filehash 参数", nil)
		return
	}

	fMeta, err := meta.GetFileMetaDB(filehash)
	if err != nil {
		writeJSON(w, http.StatusNotFound, 1, "文件不存在", nil)
		return
	}

	file, err := os.Open(fMeta.Location)
	if err != nil {
		slog.Error("open file failed", "error", err, "filehash", filehash)
		writeJSON(w, http.StatusInternalServerError, 1, "文件读取失败", nil)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		slog.Error("read file failed", "error", err, "filehash", filehash)
		writeJSON(w, http.StatusInternalServerError, 1, "文件读取失败", nil)
		return
	}

	w.Header().Set("Content-Disposition", "attachment; filename=\""+fMeta.FileName+"\"")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(data)
}

// FileMetaUpdateHandler 更新元信息接口（重命名）
func FileMetaUpdateHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	opType := r.Form.Get("op")
	fileSha1 := r.Form.Get("filehash")
	newFileName := r.Form.Get("filename")

	if opType != "0" {
		writeJSON(w, http.StatusForbidden, 1, "不支持的操作", nil)
		return
	}
	if r.Method == "GET" {
		writeJSON(w, http.StatusMethodNotAllowed, 1, "仅支持 POST", nil)
		return
	}
	if fileSha1 == "" || newFileName == "" {
		writeJSON(w, http.StatusBadRequest, 1, "缺少参数", nil)
		return
	}

	curFileMeta, err := meta.GetFileMetaDB(fileSha1)
	if err != nil {
		writeJSON(w, http.StatusNotFound, 1, "文件不存在", nil)
		return
	}

	curFileMeta.FileName = filepath.Base(newFileName)
	_ = meta.UpdateFileMetaDB(curFileMeta)

	data, _ := json.Marshal(curFileMeta)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// FileDeleteHandler 删除文件及元信息（软删除）
func FileDeleteHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	fileSha1 := r.Form.Get("filehash")
	if fileSha1 == "" {
		writeJSON(w, http.StatusBadRequest, 1, "缺少 filehash 参数", nil)
		return
	}

	// 软删除：更新数据库状态
	if ok := meta.DeleteFileMetaDB(fileSha1); !ok {
		writeJSON(w, http.StatusInternalServerError, 1, "删除失败", nil)
		return
	}

	slog.Info("file deleted", "filehash", fileSha1)
	writeJSON(w, http.StatusOK, 0, "删除成功", nil)
}

// FileQueryHandler 返回所有文件元信息列表
func FileQueryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeJSON(w, http.StatusMethodNotAllowed, 1, "仅支持 GET", nil)
		return
	}

	// 从 MySQL 查询
	fileMetas, err := meta.GetAllFileMetaDB()
	if err != nil {
		slog.Error("query all files failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, 1, "查询失败", nil)
		return
	}

	data, err := json.Marshal(fileMetas)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, 1, "数据序列化失败", nil)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// TryFastUploadHandler 秒传检测
func TryFastUploadHandler(fileHash string) bool {
	// 先查 Redis
	loc, err := rd.GetFileHash(fileHash)
	if err == nil && loc != "" {
		return true
	}

	// 再查 MySQL
	fileMeta, err := meta.GetFileMetaDB(fileHash)
	if err == nil && fileMeta.FileSha1 != "" {
		// 写入 Redis 缓存
		rd.SetFileHash(fileHash, fileMeta.Location)
		return true
	}

	return false
}

// UploadChunkHandler 分块上传
func UploadChunkHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, 1, "仅支持 POST", nil)
		return
	}

	// 限制分块大小
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadSize)

	fileHash := r.FormValue("filehash")
	index := r.FormValue("index")

	if fileHash == "" || index == "" {
		writeJSON(w, http.StatusBadRequest, 1, "缺少 filehash 或 index 参数", nil)
		return
	}

	chunkIndex, err := strconv.Atoi(index)
	if err != nil || chunkIndex < 0 {
		writeJSON(w, http.StatusBadRequest, 1, "无效的 chunk index", nil)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, 1, "分块文件获取失败", nil)
		return
	}
	defer file.Close()

	// 已上传过该分块则直接返回（幂等）
	if util.ChunkExists(fileHash, chunkIndex) {
		writeJSON(w, http.StatusOK, 0, "chunk already uploaded", nil)
		return
	}

	dir := filepath.Join("./chunks", filepath.Base(fileHash))
	os.MkdirAll(dir, 0755)

	chunkPath := filepath.Join(dir, index)

	dst, err := os.Create(chunkPath)
	if err != nil {
		slog.Error("create chunk failed", "error", err, "filehash", fileHash, "index", chunkIndex)
		writeJSON(w, http.StatusInternalServerError, 1, "分块创建失败", nil)
		return
	}
	defer dst.Close()

	io.Copy(dst, file)

	_ = util.AddChunk(fileHash, chunkIndex)

	slog.Info("chunk uploaded", "filehash", fileHash, "index", chunkIndex)
	writeJSON(w, http.StatusOK, 0, "chunk upload success", nil)
}

// UploadStatusHandler 断点续传状态查询
func UploadStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, 1, "仅支持 GET", nil)
		return
	}

	fileHash := r.FormValue("filehash")
	if fileHash == "" {
		writeJSON(w, http.StatusBadRequest, 1, "缺少 filehash 参数", nil)
		return
	}

	chunks, err := util.GetUploadedChunks(fileHash)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
		return
	}

	sort.Slice(chunks, func(i, j int) bool {
		ii, _ := strconv.Atoi(chunks[i])
		jj, _ := strconv.Atoi(chunks[j])
		return ii < jj
	})

	data, _ := json.Marshal(chunks)
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// MergeChunkHandler 分块合并
func MergeChunkHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, 1, "仅支持 POST", nil)
		return
	}

	fileHash := r.FormValue("filehash")
	fileName := r.FormValue("filename")
	totalStr := r.FormValue("chunks")

	if fileHash == "" || fileName == "" {
		writeJSON(w, http.StatusBadRequest, 1, "缺少 filehash 或 filename 参数", nil)
		return
	}

	// 路径穿越防护
	fileName = filepath.Base(fileName)

	chunkDir := filepath.Join("./chunks", filepath.Base(fileHash))

	files, err := os.ReadDir(chunkDir)
	if err != nil || len(files) == 0 {
		writeJSON(w, http.StatusInternalServerError, 1, "分块不存在", nil)
		return
	}

	// 按 chunk 序号排序
	sort.Slice(files, func(i, j int) bool {
		iIndex, _ := strconv.Atoi(files[i].Name())
		jIndex, _ := strconv.Atoi(files[j].Name())
		return iIndex < jIndex
	})

	// 校验分块数量
	if totalStr != "" {
		if total, err := strconv.Atoi(totalStr); err == nil && total > 0 {
			if len(files) != total {
				writeJSON(w, http.StatusBadRequest, 1, fmt.Sprintf("分块数量不匹配: 期望 %d, 实际 %d", total, len(files)), nil)
				return
			}
		}
	}

	dstPath := filepath.Join("./uploads", fileName)

	dst, err := os.Create(dstPath)
	if err != nil {
		slog.Error("create merged file failed", "error", err, "filename", fileName)
		writeJSON(w, http.StatusInternalServerError, 1, "合并文件创建失败", nil)
		return
	}
	defer dst.Close()

	// 合并 chunk
	for _, f := range files {
		chunkPath := filepath.Join(chunkDir, f.Name())
		chunkFile, err := os.Open(chunkPath)
		if err != nil {
			slog.Error("open chunk failed", "error", err, "chunk", chunkPath)
			writeJSON(w, http.StatusInternalServerError, 1, "分块读取失败", nil)
			return
		}

		_, err = io.Copy(dst, chunkFile)
		chunkFile.Close()

		if err != nil {
			slog.Error("merge chunk failed", "error", err, "chunk", chunkPath)
			writeJSON(w, http.StatusInternalServerError, 1, "分块合并失败", nil)
			return
		}
	}

	// 获取文件信息
	stat, err := os.Stat(dstPath)
	if err != nil {
		slog.Error("stat file failed", "error", err, "path", dstPath)
		writeJSON(w, http.StatusInternalServerError, 1, "获取文件信息失败", nil)
		return
	}

	loc, _ := time.LoadLocation("Asia/Shanghai")

	fileMeta := meta.FileMeta{
		FileName: fileName,
		Location: dstPath,
		UploadAt: time.Now().In(loc),
		FileSha1: fileHash,
		FileSize: stat.Size(),
	}

	// 写入数据库
	meta.UpdateFileMetaDB(fileMeta)

	// 写入 Redis 秒传缓存
	rd.SetFileHash(fileHash, dstPath)

	// 清理 chunk 数据
	util.ClearChunks(fileHash)
	os.RemoveAll(chunkDir)

	slog.Info("chunks merged", "filehash", fileHash, "filename", fileName, "size", stat.Size())
	writeJSON(w, http.StatusOK, 0, "merge success", nil)
}
