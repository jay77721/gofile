package ai

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"gofile/config"
	"gofile/model"
	"gofile/repository"
	"gofile/storage"
)

// --- 内存 mock：AITaskRepository ---

type mockAITaskRepo struct {
	mu    sync.Mutex
	tasks map[string]*model.AITask // key: filehash:username
}

func newMockAITaskRepo() *mockAITaskRepo {
	return &mockAITaskRepo{tasks: make(map[string]*model.AITask)}
}

func taskKey(filehash, username string) string {
	return filehash + ":" + username
}

func (m *mockAITaskRepo) CreateTask(task *model.AITask) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := taskKey(task.FileSha1, task.Username)
	if _, ok := m.tasks[k]; ok {
		return nil // 幂等
	}
	cp := *task
	m.tasks[k] = &cp
	return nil
}

func (m *mockAITaskRepo) GetTask(filehash, username string) (*model.AITask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := taskKey(filehash, username)
	if t, ok := m.tasks[k]; ok {
		cp := *t
		return &cp, nil
	}
	return nil, errors.New("not found")
}

func (m *mockAITaskRepo) MarkProcessing(filehash, username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[taskKey(filehash, username)]; ok {
		t.Status = 1
	}
	return nil
}

func (m *mockAITaskRepo) MarkDone(filehash, username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[taskKey(filehash, username)]; ok {
		t.Status = 2
	}
	return nil
}

func (m *mockAITaskRepo) MarkFailed(filehash, username, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[taskKey(filehash, username)]; ok {
		t.Status = 3
		t.RetryCount++
		t.ErrorMsg = errMsg
	}
	return nil
}

func (m *mockAITaskRepo) ListRequeueable(maxRetry int) ([]model.AITask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []model.AITask
	for _, t := range m.tasks {
		if t.Status == 3 && t.RetryCount < maxRetry {
			out = append(out, *t)
		}
	}
	return out, nil
}

// --- 内存 mock：FileRepository（最小实现，只覆盖 processor 用到的方法） ---

type mockFileRepoForProc struct {
	mu    sync.Mutex
	files map[string]model.File
}

func newMockFileRepoForProc() *mockFileRepoForProc {
	return &mockFileRepoForProc{files: make(map[string]model.File)}
}

func (m *mockFileRepoForProc) SaveAnalysis(filehash, summary, tags string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.files[filehash]
	f.Summary = summary
	f.Tags = tags
	m.files[filehash] = f
	return nil
}

func (m *mockFileRepoForProc) GetGlobalFile(filehash string) (model.File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.files[filehash]; ok {
		return f, nil
	}
	return model.File{}, errors.New("not found")
}

// 以下方法仅为满足 repository.FileRepository 接口，processor 不调用
func (m *mockFileRepoForProc) Create(f model.File) error                     { return nil }
func (m *mockFileRepoForProc) CreateUserFile(uf model.UserFile) error        { return nil }
func (m *mockFileRepoForProc) GetByHash(filehash, username string) (model.FileMeta, error) {
	return model.FileMeta{}, nil
}
func (m *mockFileRepoForProc) ListByUser(username string) ([]model.FileMeta, error) {
	return nil, nil
}
func (m *mockFileRepoForProc) CountByUser(username string) (int64, error) { return 0, nil }
func (m *mockFileRepoForProc) ListByUserPaged(username string, page, size int) ([]model.FileMeta, error) {
	return nil, nil
}
func (m *mockFileRepoForProc) Delete(filehash, username string) (bool, error) { return false, nil }
func (m *mockFileRepoForProc) UpdateName(filehash, username, newFilename string) (bool, error) {
	return false, nil
}
func (m *mockFileRepoForProc) CountRefs(filehash string) (int64, error) { return 0, nil }
func (m *mockFileRepoForProc) ListOldest(before time.Time) ([]model.File, error) {
	return nil, nil
}
func (m *mockFileRepoForProc) RemoveOrphan(filehash string) error { return nil }

// storage mock（最小实现，满足 storage.Storage 接口）
type mockStorage struct {
	content []byte
}

func (m mockStorage) Put(ctx context.Context, key string, reader io.Reader, size int64) error {
	return nil
}
func (m mockStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m.content)), nil
}
func (m mockStorage) GetRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	end := int64(len(m.content))
	if offset >= end {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	if offset+length > end {
		length = end - offset
	}
	return io.NopCloser(bytes.NewReader(m.content[offset : offset+length])), nil
}
func (m mockStorage) FileSize(ctx context.Context, key string) (int64, error) {
	return int64(len(m.content)), nil
}
func (m mockStorage) Exists(ctx context.Context, key string) (bool, error) { return true, nil }
func (m mockStorage) Delete(ctx context.Context, key string) error        { return nil }
func (m mockStorage) PresignPut(ctx context.Context, key string, expiry time.Duration) (string, error) {
	return "", nil
}
func (m mockStorage) PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	return "", nil
}

// --- 测试 ---

func newTestProcessor(provider Provider, indexer Indexer, fr repository.FileRepository, ar *mockAITaskRepo, st storage.Storage) *Processor {
	cfg := &config.Config{AIWorkers: 1}
	p := NewProcessor(provider, indexer, fr, ar, st, cfg)
	return p
}

func TestProcessor_Idempotent(t *testing.T) {
	provider := NewMockProvider(16)
	idx := NewMockIndexer()
	indexer := idx
	aiRepo := newMockAITaskRepo()
	fileRepo := newMockFileRepoForProc()
	store := mockStorage{content: []byte("hello world test content for alice")}

	p := newTestProcessor(provider, indexer, fileRepo, aiRepo, store)
	p.Start()

	// 入队一次
	p.Enqueue(context.Background(), "hash1", "test.txt", "alice")

	// 等待处理完成
	time.Sleep(500 * time.Millisecond)

	// 任务应为 done
	task, err := aiRepo.GetTask("hash1", "alice")
	if err != nil {
		t.Fatalf("task not found: %v", err)
	}
	if task.Status != 2 {
		t.Errorf("task status should be done(2), got %d", task.Status)
	}
	// 全局 summary 应被写入
	if f, ok := fileRepo.files["hash1"]; !ok || f.Summary == "" {
		t.Errorf("global summary should be saved, got %+v", f)
	}
	// 索引应有文档
	if _, ok := idx.docs["alice:hash1"]; !ok {
		t.Error("indexer should have doc alice:hash1")
	}

	// 再次入队（已 done）→ 应跳过，不重复处理
	p.Enqueue(context.Background(), "hash1", "test.txt", "alice")
	time.Sleep(300 * time.Millisecond)
	// 状态仍为 done，且未新增文档（mock indexer 只有一个）
	if len(idx.docs) != 1 {
		t.Errorf("should not re-process done task, doc count=%d", len(idx.docs))
	}
}

func TestProcessor_FailAndRequeue(t *testing.T) {
	// 让 provider.Analyze 失败
	failProvider := &failAnalyzeProvider{dim: 16}
	indexer := NewMockIndexer()
	aiRepo := newMockAITaskRepo()
	fileRepo := newMockFileRepoForProc()
	store := mockStorage{}

	p := newTestProcessor(failProvider, indexer, fileRepo, aiRepo, store)
	p.Start()

	p.Enqueue(context.Background(), "hash2", "bad.txt", "bob")
	time.Sleep(500 * time.Millisecond)

	task, _ := aiRepo.GetTask("hash2", "bob")
	if task.Status != 3 {
		t.Errorf("task should be failed(3), got %d", task.Status)
	}
	if task.RetryCount < 1 {
		t.Errorf("retry_count should be >=1, got %d", task.RetryCount)
	}

	// RequeueFailed 应重新入队
	p.RequeueFailed(context.Background())
	time.Sleep(500 * time.Millisecond)
	task2, _ := aiRepo.GetTask("hash2", "bob")
	if task2.RetryCount < 2 {
		t.Errorf("after requeue retry_count should increase, got %d", task2.RetryCount)
	}
}

func TestProcessor_FastDedupSkipsLLM(t *testing.T) {
	// 全局 summary 已存在 → 应跳过 Analyze
	callCount := 0
	provider := &countingProvider{MockProvider: NewMockProvider(16).(*MockProvider), counter: &callCount}
	indexer := NewMockIndexer()
	aiRepo := newMockAITaskRepo()
	fileRepo := newMockFileRepoForProc()
	// 预先写入全局 summary（模拟秒传命中：A 已分析过）
	fileRepo.files["hash3"] = model.File{FileSha1: "hash3", Summary: "pre-existing summary", Tags: "文档"}
	store := mockStorage{}

	p := newTestProcessor(provider, indexer, fileRepo, aiRepo, store)
	p.Start()

	p.Enqueue(context.Background(), "hash3", "report.pdf", "carol")
	time.Sleep(500 * time.Millisecond)

	if callCount != 0 {
		t.Errorf("should skip LLM on fast-dedup, but Analyze called %d times", callCount)
	}
	task, _ := aiRepo.GetTask("hash3", "carol")
	if task.Status != 2 {
		t.Errorf("task should be done, got %d", task.Status)
	}
}

// failAnalyzeProvider 总是让 Analyze 失败
type failAnalyzeProvider struct {
	dim int
}

func (p *failAnalyzeProvider) Analyze(ctx context.Context, fileName, content string) (*Analysis, error) {
	return nil, errors.New("mock analyze failure")
}
func (p *failAnalyzeProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	v := make([]float32, p.dim)
	return v, nil
}
func (p *failAnalyzeProvider) Dimension() int { return p.dim }

// countingProvider 包装 MockProvider，计数 Analyze 调用
type countingProvider struct {
	*MockProvider
	counter *int
}

func (p *countingProvider) Analyze(ctx context.Context, fileName, content string) (*Analysis, error) {
	*p.counter++
	return p.MockProvider.Analyze(ctx, fileName, content)
}
