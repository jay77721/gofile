package ai

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/typesense/typesense-go/v2/typesense"
	"github.com/typesense/typesense-go/v2/typesense/api"
)

// TypesenseIndexer Typesense 实现的检索引擎
type TypesenseIndexer struct {
	client    *typesense.Client
	collection string
	dim       int
}

// NewTypesenseIndexer 创建 Typesense 检索引擎
func NewTypesenseIndexer(addr, apiKey string, dim int) *TypesenseIndexer {
	client := typesense.NewClient(
		typesense.WithServer(addr),
		typesense.WithAPIKey(apiKey),
		typesense.WithConnectionTimeout(5*1000),
	)
	return &TypesenseIndexer{
		client:     client,
		collection: "files",
		dim:        dim,
	}
}

// collectionSchema 返回 files collection 的 schema
func (t *TypesenseIndexer) collectionSchema() *api.CollectionSchema {
	dim := t.dim
	return &api.CollectionSchema{
		Name: t.collection,
		Fields: []api.Field{
			{Name: "id", Type: "string"},
			{Name: "username", Type: "string", Facet: ptrBool(false)},
			{Name: "filehash", Type: "string"},
			{Name: "filename", Type: "string", Sort: ptrBool(true)},
			{Name: "summary", Type: "string"},
			{Name: "tags", Type: "string[]", Facet: ptrBool(true)},
			{Name: "size", Type: "int64", Sort: ptrBool(true)},
			{Name: "created_at", Type: "int64", Sort: ptrBool(true)},
			{Name: "content_vec", Type: "float[]", Optional: ptrBool(true), NumDim: &dim},
		},
	}
}

// EnsureCollection 幂等创建 collection
func (t *TypesenseIndexer) EnsureCollection(ctx context.Context) error {
	// 幂等：已存在则忽略
	if _, err := t.client.Collection(t.collection).Retrieve(ctx); err == nil {
		slog.InfoContext(ctx, "typesense collection exists", "collection", t.collection)
		return nil
	}
	if _, err := t.client.Collections().Create(ctx, t.collectionSchema()); err != nil {
		return fmt.Errorf("create typesense collection failed: %w", err)
	}
	slog.InfoContext(ctx, "typesense collection created", "collection", t.collection)
	return nil
}

func (t *TypesenseIndexer) Upsert(ctx context.Context, doc *Doc) error {
	if doc == nil {
		return nil
	}
	if doc.ID == "" {
		doc.ID = doc.Username + ":" + doc.Filehash
	}
	payload := map[string]any{
		"id":          doc.ID,
		"username":    doc.Username,
		"filehash":    doc.Filehash,
		"filename":    doc.Filename,
		"summary":     doc.Summary,
		"tags":        doc.Tags,
		"size":        doc.Size,
		"created_at":  doc.CreatedAt,
		"content_vec": doc.ContentVec,
	}
	if _, err := t.client.Collection(t.collection).Documents().Upsert(ctx, payload); err != nil {
		return fmt.Errorf("typesense upsert failed: %w", err)
	}
	return nil
}

func (t *TypesenseIndexer) Delete(ctx context.Context, username, filehash string) error {
	id := username + ":" + filehash
	if _, err := t.client.Collection(t.collection).Document(id).Delete(ctx); err != nil {
		return fmt.Errorf("typesense delete failed: %w", err)
	}
	return nil
}

// SearchHybrid 混合检索（全文 + 向量 KNN，RRF 融合）+ 所有权 filter
func (t *TypesenseIndexer) SearchHybrid(ctx context.Context, q, username string, vector []float32, filter string, page, size int) ([]Doc, error) {
	filterBy := fmt.Sprintf("username:=%s", username)
	if filter != "" {
		filterBy = filterBy + " && " + filter
	}
	vectorQuery := ""
	if len(vector) > 0 {
		vectorQuery = fmt.Sprintf("content_vec:(%s, k:100)", formatVector(vector))
	}

	req := &api.SearchCollectionParams{
		Q:           ptrString(q),
		QueryBy:     ptrString("filename,summary"),
		FilterBy:    &filterBy,
		VectorQuery: &vectorQuery,
		Page:        ptrInt(page),
		PerPage:     ptrInt(size),
	}

	res, err := t.client.Collection(t.collection).Documents().Search(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("typesense search failed: %w", err)
	}
	if res.Hits == nil {
		return []Doc{}, nil
	}
	return hitsToDocs(*res.Hits), nil
}

// Similar 相似文件推荐（向量 KNN，排除自身）
func (t *TypesenseIndexer) Similar(ctx context.Context, username string, vector []float32, excludeFilehash string, limit int) ([]Doc, error) {
	if len(vector) == 0 {
		return []Doc{}, nil
	}
	vectorQuery := fmt.Sprintf("content_vec:(%s, k:%d)", formatVector(vector), limit+1)
	filterBy := fmt.Sprintf("username:=%s", username)

	req := &api.SearchCollectionParams{
		Q:           ptrString("*"),
		QueryBy:     ptrString("filename"),
		FilterBy:    &filterBy,
		VectorQuery: &vectorQuery,
		PerPage:     ptrInt(limit + 1),
	}
	res, err := t.client.Collection(t.collection).Documents().Search(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("typesense similar failed: %w", err)
	}
	if res.Hits == nil {
		return []Doc{}, nil
	}
	docs := hitsToDocs(*res.Hits)
	// 排除自身
	out := make([]Doc, 0, len(docs))
	for _, d := range docs {
		if d.Filehash != excludeFilehash {
			out = append(out, d)
		}
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func formatVector(v []float32) string {
	var sb strings.Builder
	for i, x := range v {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(strconv.FormatFloat(float64(x), 'f', 4, 32))
	}
	return sb.String()
}

func hitsToDocs(hits []api.SearchResultHit) []Doc {
	if hits == nil {
		return []Doc{}
	}
	out := make([]Doc, 0, len(hits))
	for i := range hits {
		doc := Doc{}
		if hits[i].Document == nil {
			continue
		}
		m := *hits[i].Document
		if v, ok := m["id"].(string); ok {
			doc.ID = v
		}
		if v, ok := m["username"].(string); ok {
			doc.Username = v
		}
		if v, ok := m["filehash"].(string); ok {
			doc.Filehash = v
		}
		if v, ok := m["filename"].(string); ok {
			doc.Filename = v
		}
		if v, ok := m["summary"].(string); ok {
			doc.Summary = v
		}
		if v, ok := m["size"].(float64); ok {
			doc.Size = int64(v)
		}
		if v, ok := m["created_at"].(float64); ok {
			doc.CreatedAt = int64(v)
		}
		if arr, ok := m["tags"].([]any); ok {
			for _, x := range arr {
				if s, ok := x.(string); ok {
					doc.Tags = append(doc.Tags, s)
				}
			}
		}
		out = append(out, doc)
	}
	return out
}

func ptrInt(n int) *int {
	return &n
}

func ptrBool(b bool) *bool {
	return &b
}

func ptrString(s string) *string {
	return &s
}
