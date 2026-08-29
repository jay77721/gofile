package ai

import (
	"context"
	"gofile/internal/port"
	"math"
	"strings"
	"sync"
)

// MockIndexer is an in-memory Indexer (for testing, no external dependencies)
//
// Implements minimal semantics: substring matching on filename/summary + vector cosine similarity ranking.
// Only used to verify orchestration logic in unit tests, not a replacement for real Typesense behavior.
type MockIndexer struct {
	mu   sync.RWMutex
	docs map[string]*port.Doc // key: username:filehash
}

// NewMockIndexer creates an in-memory mock search engine
func NewMockIndexer() *MockIndexer {
	return &MockIndexer{docs: make(map[string]*port.Doc)}
}

func (m *MockIndexer) EnsureCollection(_ context.Context) error { return nil }

func (m *MockIndexer) Upsert(_ context.Context, doc *port.Doc) error {
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

func (m *MockIndexer) SearchHybrid(ctx context.Context, q, username string, vector []float32, filter string, page, size int) ([]port.Doc, error) {
	m.mu.RLock()
	all := make([]*port.Doc, 0, len(m.docs))
	for _, d := range m.docs {
		if d.Username == username {
			all = append(all, d)
		}
	}
	m.mu.RUnlock()

	// Substring matching and scoring
	q = strings.ToLower(q)
	var scored []port.Doc
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

	// Simple pagination
	start := (page - 1) * size
	if start < 0 {
		start = 0
	}
	if start >= len(scored) {
		return []port.Doc{}, nil
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

func (m *MockIndexer) Similar(_ context.Context, username string, vector []float32, excludeFilehash string, limit int) ([]port.Doc, error) {
	m.mu.RLock()
	var candidates []*port.Doc
	for _, d := range m.docs {
		if d.Username == username && d.Filehash != excludeFilehash {
			candidates = append(candidates, d)
		}
	}
	m.mu.RUnlock()

	// Sort by cosine similarity
	type scoredDoc struct {
		doc   port.Doc
		score float64
	}
	var scored []scoredDoc
	for _, d := range candidates {
		s := cosine(vector, d.ContentVec)
		scored = append(scored, scoredDoc{doc: *d, score: s})
	}
	// Sort by score descending
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
	out := make([]port.Doc, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, scored[i].doc)
	}
	return out, nil
}

// cosine computes cosine similarity (returns 0 for zero vectors)
func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
