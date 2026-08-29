package hashutil

import (
	"bytes"
	"testing"
)

// 压测基准:文件哈希计算(上传/秒传主路径)
// 运行: go test ./util/ -bench Benchmark -benchmem

func BenchmarkSha1_1MB(b *testing.B) {
	data := bytes.Repeat([]byte("gofile-bench-"), 1<<20/12)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Sha1(data)
	}
}

func BenchmarkSha1Stream_16MB(b *testing.B) {
	data := bytes.Repeat([]byte("gofile-bench-"), 16<<20/12)
	chunk := make([]byte, 32*1024)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := &Sha1Stream{}
		for off := 0; off < len(data); off += len(chunk) {
			end := off + len(chunk)
			if end > len(data) {
				end = len(data)
			}
			s.Update(data[off:end])
		}
		_ = s.Sum()
	}
}

func BenchmarkMD5_1MB(b *testing.B) {
	data := bytes.Repeat([]byte("gofile-bench-"), 1<<20/12)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MD5(data)
	}
}
