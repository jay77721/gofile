package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gofile/internal/config"
	"gofile/internal/domain"
	"gofile/internal/infrastructure/ai"

	"gorm.io/gorm"
)

// fakeAIConfigRepo in-memory AIConfigRepository for service tests
type fakeAIConfigRepo struct {
	cfg *model.AIConfig
}

func (f *fakeAIConfigRepo) Get(username string) (*model.AIConfig, error) {
	if f.cfg == nil || f.cfg.Username != username {
		return nil, gorm.ErrRecordNotFound
	}
	return f.cfg, nil
}

func (f *fakeAIConfigRepo) Upsert(cfg *model.AIConfig) error {
	f.cfg = cfg
	return nil
}

func (f *fakeAIConfigRepo) Delete(username string) error {
	f.cfg = nil
	return nil
}

func newTestAIConfigSvc() *AIConfigService {
	cfg := &config.Config{AIEmbedDim: 128, AllowPrivateAIURL: true}
	return NewAIConfigService(&fakeAIConfigRepo{}, cfg, ai.NewMockProvider(128))
}

const testAPIKey = "sk-test-abcdef1234567890"

func TestAIConfigSaveAndGetView(t *testing.T) {
	svc := newTestAIConfigSvc()
	ctx := context.Background()

	if err := svc.Save(ctx, "alice", "https://8.8.8.8/v1", testAPIKey, "gpt-4o", "text-embedding-3-small"); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	view, err := svc.GetView(ctx, "alice")
	if err != nil {
		t.Fatalf("GetView failed: %v", err)
	}
	if !view.Configured || !view.HasKey {
		t.Fatalf("expected configured with key, got %+v", view)
	}
	if view.BaseURL != "https://8.8.8.8/v1" || view.Model != "gpt-4o" {
		t.Fatalf("view fields mismatch: %+v", view)
	}
	if view.Mode != "openai" {
		t.Fatalf("expected mode openai, got %s", view.Mode)
	}
	if strings.Contains(view.APIKeyMask, testAPIKey) {
		t.Fatalf("view leaks api key: %s", view.APIKeyMask)
	}
	if !strings.HasPrefix(view.APIKeyMask, "sk-t") || !strings.Contains(view.APIKeyMask, "****") {
		t.Fatalf("unexpected mask format: %s", view.APIKeyMask)
	}
}

func TestAIConfigSaveRejectsPrivateURL(t *testing.T) {
	cfg := &config.Config{AIEmbedDim: 128, AllowPrivateAIURL: false}
	svc := NewAIConfigService(&fakeAIConfigRepo{}, cfg, ai.NewMockProvider(128))

	err := svc.Save(context.Background(), "alice", "http://localhost:11434", testAPIKey, "", "")
	if err == nil || !strings.Contains(err.Error(), "private network") {
		t.Fatalf("expected SSRF rejection, got %v", err)
	}
}

func TestAIConfigSaveKeepsOldKey(t *testing.T) {
	svc := newTestAIConfigSvc()
	ctx := context.Background()

	if err := svc.Save(ctx, "alice", "", testAPIKey, "", ""); err != nil {
		t.Fatal(err)
	}
	// Second save only updates model, key left empty -> keep old key
	if err := svc.Save(ctx, "alice", "", "", "gpt-4o-mini", ""); err != nil {
		t.Fatal(err)
	}
	view, _ := svc.GetView(ctx, "alice")
	if !view.HasKey {
		t.Fatal("expected old key preserved")
	}
	if view.Model != "gpt-4o-mini" {
		t.Fatalf("model not updated: %+v", view)
	}
}

func TestAIConfigSaveRequiresKey(t *testing.T) {
	svc := newTestAIConfigSvc()
	if err := svc.Save(context.Background(), "alice", "", "", "", ""); err == nil {
		t.Fatal("expected error when no key provided")
	}
}

func TestAIConfigDelete(t *testing.T) {
	svc := newTestAIConfigSvc()
	ctx := context.Background()
	_ = svc.Save(ctx, "alice", "", testAPIKey, "", "")
	if err := svc.Delete(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	view, _ := svc.GetView(ctx, "alice")
	if view.Configured {
		t.Fatal("expected config cleared")
	}
}

// mockOpenAIServer mocks OpenAI-compatible endpoints (/chat/completions + /embeddings)
func mockOpenAIServer(t *testing.T, chatOK, embedOK bool, embedDim int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			if !chatOK {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"summary\":\"ok\",\"tags\":[\"a\"]}"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/embeddings"):
			if !embedOK {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
				return
			}
			vec := make([]float64, embedDim)
			vec[0] = 1.0
			resp := map[string]any{"data": []map[string]any{{"embedding": vec}}}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestAIConfigTestConnectionAllOK(t *testing.T) {
	srv := mockOpenAIServer(t, true, true, 128)
	defer srv.Close()

	svc := newTestAIConfigSvc()
	res := svc.TestConnection(context.Background(), srv.URL, testAPIKey, "gpt-4o", "text-embedding-3-small")
	if !res.OK || !res.ChatOK || !res.EmbedOK {
		t.Fatalf("expected all ok, got %+v", res)
	}
	if res.Dim != 128 || res.DimMismatch {
		t.Fatalf("expected dim 128 no mismatch, got %+v", res)
	}
}

func TestAIConfigTestConnectionBadKey(t *testing.T) {
	srv := mockOpenAIServer(t, false, true, 128)
	defer srv.Close()

	svc := newTestAIConfigSvc()
	res := svc.TestConnection(context.Background(), srv.URL, "sk-bad", "gpt-4o", "")
	if res.OK || res.ChatOK {
		t.Fatalf("expected chat failure, got %+v", res)
	}
	if !strings.Contains(res.Error, "对话接口调用失败") {
		t.Fatalf("unexpected error: %s", res.Error)
	}
}

func TestAIConfigTestConnectionDimMismatch(t *testing.T) {
	srv := mockOpenAIServer(t, true, true, 1536)
	defer srv.Close()

	svc := newTestAIConfigSvc() // index dimension 128
	res := svc.TestConnection(context.Background(), srv.URL, testAPIKey, "gpt-4o", "")
	if !res.OK || !res.DimMismatch || res.Dim != 1536 {
		t.Fatalf("expected dim mismatch 1536 vs 128, got %+v", res)
	}
}

func TestAIConfigTestConnectionRejectsPrivate(t *testing.T) {
	cfg := &config.Config{AIEmbedDim: 128, AllowPrivateAIURL: false}
	svc := NewAIConfigService(&fakeAIConfigRepo{}, cfg, ai.NewMockProvider(128))
	res := svc.TestConnection(context.Background(), "http://127.0.0.1:9999", testAPIKey, "", "")
	if res.OK || !strings.Contains(res.Error, "private network") {
		t.Fatalf("expected private rejection, got %+v", res)
	}
}

func TestAIConfigResolveProviderAndCache(t *testing.T) {
	svc := newTestAIConfigSvc()
	ctx := context.Background()
	_ = svc.Save(ctx, "alice", "", testAPIKey, "", "")

	prov := svc.ResolveProvider(ctx, "alice")
	if prov == nil {
		t.Fatal("expected resolved provider")
	}
	openAI, ok := prov.(*ai.OpenAIProvider)
	if !ok {
		t.Fatalf("expected *ai.OpenAIProvider, got %T", prov)
	}
	if openAI.Dimension() != 128 {
		t.Fatalf("expected dim 128, got %d", openAI.Dimension())
	}

	// Cache hit: second call does not read repo
	first := svc.cache["alice"].prov
	if first != prov {
		t.Fatal("expected cached provider reuse")
	}

	// Unconfigured user returns nil
	if p := svc.ResolveProvider(ctx, "bob"); p != nil {
		t.Fatalf("expected nil for unconfigured user, got %T", p)
	}

	// Cache invalidated after Delete
	_ = svc.Delete(ctx, "alice")
	if p := svc.ResolveProvider(ctx, "alice"); p != nil {
		t.Fatalf("expected nil after delete, got %T", p)
	}
}

// Ensure providerCacheTTL is within reasonable bounds to prevent indefinite cache
func TestProviderCacheTTLSanity(t *testing.T) {
	if providerCacheTTL <= 0 || providerCacheTTL > time.Hour {
		t.Fatalf("providerCacheTTL out of range: %v", providerCacheTTL)
	}
}
