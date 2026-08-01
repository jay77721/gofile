package util

import (
	"os"
	"testing"
)

func TestSha1(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", "da39a3ee5e6b4b0d3255bfef95601890afd80709"},
		{"hello", "hello", "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d"},
		{"hello world", "hello world", "2aae6c35c94fcfb415dbe95f408b9ce91ee846ed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Sha1([]byte(tt.input))
			if got != tt.want {
				t.Errorf("Sha1(%q) = %s, want %s", tt.input, got, tt.want)
			}
		})
	}
}

func TestMD5(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", "d41d8cd98f00b204e9800998ecf8427e"},
		{"hello", "hello", "5d41402abc4b2a76b9719d911017c592"},
		{"hello world", "hello world", "5eb63bbbe01eeed093cb22bb8f5acdc3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MD5([]byte(tt.input))
			if got != tt.want {
				t.Errorf("MD5(%q) = %s, want %s", tt.input, got, tt.want)
			}
		})
	}
}

func TestFileSha1(t *testing.T) {
	// 创建临时文件
	tmpFile, err := os.CreateTemp("", "test-sha1-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	tmpFile.WriteString("hello world")
	tmpFile.Close()

	// 重新打开计算 hash
	f, err := os.Open(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	got := FileSha1(f)
	want := "2aae6c35c94fcfb415dbe95f408b9ce91ee846ed"
	if got != want {
		t.Errorf("FileSha1() = %s, want %s", got, want)
	}
}

func TestPathExists(t *testing.T) {
	// 测试存在的路径
	exists, err := PathExists(".")
	if err != nil {
		t.Errorf("PathExists(\".\") error: %v", err)
	}
	if !exists {
		t.Error("PathExists(\".\") = false, want true")
	}

	// 测试不存在的路径
	exists, err = PathExists("/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Errorf("PathExists(nonexistent) error: %v", err)
	}
	if exists {
		t.Error("PathExists(nonexistent) = true, want false")
	}
}

func TestSha1Stream(t *testing.T) {
	stream := &Sha1Stream{}
	stream.Update([]byte("hello"))
	stream.Update([]byte(" "))
	stream.Update([]byte("world"))

	got := stream.Sum()
	want := "2aae6c35c94fcfb415dbe95f408b9ce91ee846ed"
	if got != want {
		t.Errorf("Sha1Stream.Sum() = %s, want %s", got, want)
	}
}
