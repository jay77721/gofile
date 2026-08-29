package ai

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/typesense/typesense-go/v2/typesense"
	"github.com/typesense/typesense-go/v2/typesense/api"
	"gofile/internal/port"
)

// TypesenseIndexer is a search engine implemented with Typesense
type TypesenseIndexer struct {
	client     *typesense.Client
	collection string
	dim        int
}

// NewTypesenseIndexer creates a Typesense search engine
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

// collectionSchema returns the schema for the files collection
func (t *TypesenseIndexer) collectionSchema() *api.CollectionSchema {
	dim := t.dim
	return &api.CollectionSchema{
		Name: t.collection,
		Fields: []api.Field{
			{Name: "id", Type: "string"},
			{Name: "username", Type: "string", Facet: ptrBool(true)},
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

// EnsureCollection creates the collection idempotently
func (t *TypesenseIndexer) EnsureCollection(ctx context.Context) error {
	// Idempotent: ignore if already exists
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

func (t *TypesenseIndexer) Upsert(ctx context.Context, doc *port.Doc) error {
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

// SearchHybrid performs hybrid search (full-text + vector KNN, RRF fusion) + ownership filter
func (t *TypesenseIndexer) SearchHybrid(ctx context.Context, q, username string, vector []float32, filter string, page, size int) ([]port.Doc, error) {
	filterBy := ownershipFilter(username)
	if filter = safeTypeFilter(filter); filter != "" {
		// Keep caller-provided filters grouped so an OR cannot weaken ownership.
		filterBy = "(" + filterBy + ") && (" + filter + ")"
	}
	vectorQuery := ""
	if len(vector) > 0 {
		vectorQuery = fmt.Sprintf("content_vec:(%s, k:100)", formatVector(vector))
	}

	req := &api.SearchCollectionParams{
		Q:           ptrString(q),
		QueryBy:     ptrString("filename,summary,tags"),
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
		return []port.Doc{}, nil
	}
	return hitsToDocs(*res.Hits), nil
}

// Similar recommends similar files (vector KNN, excluding itself)
func (t *TypesenseIndexer) Similar(ctx context.Context, username string, vector []float32, excludeFilehash string, limit int) ([]port.Doc, error) {
	if len(vector) == 0 {
		return []port.Doc{}, nil
	}
	vectorQuery := fmt.Sprintf("content_vec:(%s, k:%d)", formatVector(vector), limit+1)
	filterBy := ownershipFilter(username)

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
		return []port.Doc{}, nil
	}
	docs := hitsToDocs(*res.Hits)
	// Exclude itself
	out := make([]port.Doc, 0, len(docs))
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

// ownershipFilter emits a literal exact-match value. Backticks prevent filter
// operators, whitespace, and separators in a username from changing syntax.
func ownershipFilter(username string) string {
	return "username:=" + typesenseLiteral(username)
}

func typesenseLiteral(value string) string {
	value = strings.ReplaceAll(value, "`", "\\`")
	return "`" + value + "`"
}

// safeTypeFilter accepts only filters generated by ParseQuery.
func safeTypeFilter(filter string) string {
	switch filter {
	case "tags:=[图片]", "tags:=[视频]", "tags:=[音频]", "tags:=[表格]",
		"tags:=[文档]", "tags:=[演示]", "tags:=[代码]", "tags:=[压缩包]":
		return filter
	default:
		return ""
	}
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

func hitsToDocs(hits []api.SearchResultHit) []port.Doc {
	if hits == nil {
		return []port.Doc{}
	}
	out := make([]port.Doc, 0, len(hits))
	for i := range hits {
		doc := port.Doc{}
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
		// Score: TextMatch > (1-VectorDistance)
		if hits[i].TextMatch != nil {
			doc.Score = float64(*hits[i].TextMatch)
		} else if hits[i].VectorDistance != nil {
			doc.Score = 1.0 - float64(*hits[i].VectorDistance)
		}
		out = append(out, doc)
	}
	return out
}

// DeleteByFilehash deletes all user documents for the specified filehash (for GC)
func (t *TypesenseIndexer) DeleteByFilehash(ctx context.Context, filehash string) error {
	filterBy := "filehash:=" + typesenseLiteral(filehash)
	page := 1
	for {
		req := &api.SearchCollectionParams{
			Q:        ptrString("*"),
			QueryBy:  ptrString("filename"),
			FilterBy: &filterBy,
			Page:     ptrInt(page),
			PerPage:  ptrInt(250),
		}
		res, err := t.client.Collection(t.collection).Documents().Search(ctx, req)
		if err != nil {
			return fmt.Errorf("typesense search for delete failed: %w", err)
		}
		if res.Hits == nil || len(*res.Hits) == 0 {
			return nil
		}
		for _, hit := range *res.Hits {
			if hit.Document == nil {
				continue
			}
			if id, ok := (*hit.Document)["id"].(string); ok {
				if _, err := t.client.Collection(t.collection).Document(id).Delete(ctx); err != nil {
					return fmt.Errorf("typesense delete doc %s failed: %w", id, err)
				}
			}
		}
		// Paginate until all hits are fetched: the number of users sharing this file may exceed the single-page limit (250),
		// otherwise orphan documents would remain and still be hit by semantic search
		if len(*res.Hits) < 250 {
			return nil
		}
		page++
	}
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
