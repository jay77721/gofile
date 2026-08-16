package handler

import (
	"bytes"
	"context"
	"gofile/config"
	"gofile/model"
	"gofile/repository"
	"gofile/service"
	"gofile/storage"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupDownloadTestHandler(t *testing.T) (*FileHandler, string, string, string) {
	t.Helper()
	dir := t.TempDir()
	store := storage.NewLocal(dir)
	cfg := &config.Config{UploadDir: dir, ChunkDir: dir}
	fileRepo := repository.NewMockFileRepository()
	fileSvc := service.NewFileService(fileRepo, store, cfg)
	fh := NewFileHandler(fileSvc, cfg)

	ctx := context.Background()

	// 1. 创建普通文本文件 (10 bytes)
	const textHash = "abcdef0123456789abcdef0123456789abcdef01"
	textContent := []byte("0123456789")
	_ = fileRepo.Create(ctx, model.File{FileSha1: textHash, FileName: "test.txt", FileSize: int64(len(textContent))})
	_ = fileRepo.CreateUserFile(ctx, model.UserFile{Username: "alice", FileSha1: textHash, FileName: "test.txt", Status: model.UserFileStatusActive})
	_ = store.Put(ctx, textHash, bytes.NewReader(textContent), int64(len(textContent)))

	// 2. 创建 PNG 图片文件
	const imgHash = "1111222233334444555566667777888899990000"
	imgContent := []byte("\x89PNG\r\n\x1a\nimage-data")
	_ = fileRepo.Create(ctx, model.File{FileSha1: imgHash, FileName: "avatar.png", FileSize: int64(len(imgContent))})
	_ = fileRepo.CreateUserFile(ctx, model.UserFile{Username: "alice", FileSha1: imgHash, FileName: "avatar.png", Status: model.UserFileStatusActive})
	_ = store.Put(ctx, imgHash, bytes.NewReader(imgContent), int64(len(imgContent)))

	// 3. 创建未知格式二进制文件
	const binHash = "2222333344445555666677778888999900001111"
	binContent := []byte("plain text inside unknown ext")
	_ = fileRepo.Create(ctx, model.File{FileSha1: binHash, FileName: "data.bin", FileSize: int64(len(binContent))})
	_ = fileRepo.CreateUserFile(ctx, model.UserFile{Username: "alice", FileSha1: binHash, FileName: "data.bin", Status: model.UserFileStatusActive})
	_ = store.Put(ctx, binHash, bytes.NewReader(binContent), int64(len(binContent)))

	return fh, textHash, imgHash, binHash
}

func setupDownloadRouter(fh *FileHandler, username string) *gin.Engine {
	r := setupRouter()
	r.Use(func(c *gin.Context) {
		c.Set("username", username)
		c.Next()
	})
	r.GET("/file/download", fh.DownloadHandler)
	r.GET("/file/preview", fh.PreviewHandler)
	r.GET("/file/download/url", fh.PresignDownloadHandler)
	return r
}

func TestParseRangeHeader_Unit(t *testing.T) {
	cases := []struct {
		name      string
		header    string
		totalSize int64
		wantOK    bool
		wantOff   int64
		wantLen   int64
	}{
		{"closed range", "bytes=0-1023", 0, true, 0, 1024},
		{"closed range mid", "bytes=100-199", 0, true, 100, 100},
		{"open range with known size", "bytes=100-", 1000, true, 100, 900},
		{"open range unknown size -> -1", "bytes=100-", 0, true, 100, -1},
		{"open range beyond size", "bytes=900-", 900, false, 0, 0},
		{"reversed range", "bytes=200-100", 0, false, 0, 0},
		{"non-bytes unit", "items=0-10", 0, false, 0, 0},
		{"empty spec", "bytes=", 0, false, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			off, length, ok := parseRangeHeader(tc.header, tc.totalSize)
			if ok != tc.wantOK || off != tc.wantOff || length != tc.wantLen {
				t.Errorf("parseRangeHeader(%q, %d) = (%d, %d, %v), want (%d, %d, %v)",
					tc.header, tc.totalSize, off, length, ok, tc.wantOff, tc.wantLen, tc.wantOK)
			}
		})
	}
}

func TestDownloadHandler_FullAndRange(t *testing.T) {
	fh, textHash, _, _ := setupDownloadTestHandler(t)
	r := setupDownloadRouter(fh, "alice")

	t.Run("full file download without Range", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/file/download?filehash="+textHash, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if got := w.Body.String(); got != "0123456789" {
			t.Errorf("body = %q, want '0123456789'", got)
		}
		if w.Header().Get("Accept-Ranges") != "bytes" {
			t.Errorf("Accept-Ranges = %q, want bytes", w.Header().Get("Accept-Ranges"))
		}
	})

	t.Run("range partial downloads", func(t *testing.T) {
		cases := []struct {
			name        string
			rangeHeader string
			wantStatus  int
			wantCR      string
			wantBody    string
		}{
			{"open range", "bytes=2-", http.StatusPartialContent, "bytes 2-9/10", "23456789"},
			{"closed range", "bytes=1-3", http.StatusPartialContent, "bytes 1-3/10", "123"},
			{"clamped closed range", "bytes=5-99", http.StatusPartialContent, "bytes 5-9/10", "56789"},
			{"out of bounds", "bytes=10-", http.StatusRequestedRangeNotSatisfiable, "bytes */10", ""},
			{"invalid range", "bytes=9-2", http.StatusRequestedRangeNotSatisfiable, "bytes */10", ""},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest("GET", "/file/download?filehash="+textHash, nil)
				req.Header.Set("Range", tc.rangeHeader)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)

				if w.Code != tc.wantStatus {
					t.Fatalf("status = %d, want %d", w.Code, tc.wantStatus)
				}
				if tc.wantCR != "" {
					if got := w.Header().Get("Content-Range"); got != tc.wantCR {
						t.Errorf("Content-Range = %q, want %q", got, tc.wantCR)
					}
				}
				if tc.wantBody != "" {
					if got := w.Body.String(); got != tc.wantBody {
						t.Errorf("body = %q, want %q", got, tc.wantBody)
					}
				}
			})
		}
	})

	t.Run("download error cases", func(t *testing.T) {
		// 缺少 filehash
		req := httptest.NewRequest("GET", "/file/download", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("missing filehash status = %d, want 400", w.Code)
		}

		// 文件不存在
		req = httptest.NewRequest("GET", "/file/download?filehash=0000000000000000000000000000000000000000", nil)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("not found status = %d, want 404", w.Code)
		}
	})
}

func TestPreviewHandler_FullAndRange(t *testing.T) {
	fh, textHash, imgHash, binHash := setupDownloadTestHandler(t)
	r := setupDownloadRouter(fh, "alice")

	t.Run("preview text file", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/file/preview?filehash="+textHash, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if got := w.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
			t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", got)
		}
		if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
		}
	})

	t.Run("preview image file", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/file/preview?filehash="+imgHash, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if got := w.Header().Get("Content-Type"); got != "image/png" {
			t.Errorf("Content-Type = %q, want image/png", got)
		}
	})

	t.Run("preview with range (seeking)", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/file/preview?filehash="+textHash, nil)
		req.Header.Set("Range", "bytes=0-3")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusPartialContent {
			t.Fatalf("status = %d, want 206", w.Code)
		}
		if got := w.Body.String(); got != "0123" {
			t.Errorf("body = %q, want 0123", got)
		}
	})

	t.Run("preview unknown ext with auto detection", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/file/preview?filehash="+binHash, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
	})

	t.Run("preview missing or non-existent", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/file/preview", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}

		req = httptest.NewRequest("GET", "/file/preview?filehash=0000000000000000000000000000000000000000", nil)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})
}

func TestPresignDownloadHandler(t *testing.T) {
	fh, textHash, _, _ := setupDownloadTestHandler(t)
	r := setupDownloadRouter(fh, "alice")

	t.Run("missing filehash", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/file/download/url", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("invalid filehash", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/file/download/url?filehash=badhash", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("local storage returns 400 CodeStorageError", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/file/download/url?filehash="+textHash, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})
}
