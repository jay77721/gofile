package ai

import (
	"context"
	"math"
	"strings"
	"sync"
)

// MockIndexer 内存版 Indexer（测试用，无外部依赖）
//
// 实现极简语义：按 filename/summary 子串匹配 + 向量余弦相似度排序。
// 仅用于单测验证编排逻辑，不替代真实 Typesense 行为。
type MockIndexer struct {
	mu   sync.RWMutex
	docs map[string]*Doc // key: username:filehash
}

// NewMockIndexer 创建内存 mock 检索引擎
func NewMockIndexer() *MockIndexer {
	return &MockIndexer{docs: make(map[string]*Doc)}
}

func (m *MockIndexer) EnsureCollection(_ context.Context) error { return nil }

func (m *MockIndexer) Upsert(_ context.Context, doc *Doc) error {
	if doc == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docs[doc.ID] = doc
	return nil
}

func (m *MockIndexer) Delete(_ context.Context, username, filehash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.docs, username+":"+filehash)
	return nil
}

func (m *MockIndexer) SearchHybrid(ctx context.Context, q, username string, vector []float32, filter string, page, size int) ([]Doc, error) {
	m.mu.RLock()
	all := make([]*Doc, 0, len(m.docs))
	for _, d := range m.docs {
		if d.Username == username {
			all = append(all, d)
		}
	}
	m.mu.RUnlock()

	// 子串匹配打分
	q = strings.ToLower(q)
	var scored []Doc
	for _, d := range all {
		score := 0.0
		if q == "" {
			score = 1.0
		} else {
			if strings.Contains(strings.ToLower(d.Filename), q) {
				score += 2.0
			}
			if strings.Contains(strings.ToLower(d.Summary), q) {
				score += 1.0
			}
			for _, tag := range d.Tags {
				if strings.Contains(strings.ToLower(tag), q) {
					score += 0.5
				}
			}
		}
		if score > 0 {
			doc := *d
			_ = doc
			scored = append(scored, *d)
		}
	}

	// 简单分页
	start := (page - 1) * size
	if start < 0 {
		start = 0
	}
	if start >= len(scored) {
		return []Doc{}, nil
	}
	end := start + size
	if end > len(scored) {
		end = len(scored)
	}
	return scored[start:end], nil
}

func (m *MockIndexer) DeleteByFilehash(_ context.Context, filehash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, d := range m.docs {
		if d.Filehash == filehash {
			delete(m.docs, id)
		}
	}
	return nil
}

func (m *MockIndexer) Similar(_ context.Context, username string, vector []float32, excludeFilehash string, limit int) ([]Doc, error) {
	m.mu.RLock()
	var candidates []*Doc
	for _, d := range m.docs {
		if d.Username == username && d.Filehash != excludeFilehash {
			candidates = append(candidates, d)
		}
	}
	m.mu.RUnlock()

	// 余弦相似度排序
	type scoredDoc struct {
		doc   Doc
		score float64
	}
	var scored []scoredDoc
	for _, d := range candidates {
		s := cosine(vector, d.ContentVec)
		scored = append(scored, scoredDoc{doc: *d, score: s})
	}
	// 按分数降序
	for i := 0; i < len(scored); i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].score > scored[i].score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}
	if limit > len(scored) {
		limit = len(scored)
	}
	out := make([]Doc, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, scored[i].doc)
	}
	return out, nil
}

// cosine 余弦相似度（零向量返回 0）
func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(b[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
