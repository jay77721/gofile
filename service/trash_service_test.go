package service

import (
	"bytes"
	"context"
	"gofile/ai"
	"gofile/model"
	"gofile/repository"
	"gofile/storage"
	"testing"
)

type mockIndexer struct {
	deletedByHash []string
	deletedByUser []string
}

func (m *mockIndexer) EnsureCollection(ctx context.Context) error { return nil }
func (m *mockIndexer) Upsert(ctx context.Context, doc *ai.Doc) error { return nil }
func (m *mockIndexer) Delete(ctx context.Context, username, filehash string) error {
	m.deletedByUser = append(m.deletedByUser, username+":"+filehash)
	return nil
}
func (m *mockIndexer) SearchHybrid(ctx context.Context, q, username string, vector []float32, filter string, page, size int) ([]ai.Doc, error) {
	return nil, nil
}
func (m *mockIndexer) Similar(ctx context.Context, username string, vector []float32, excludeFilehash string, limit int) ([]ai.Doc, error) {
	return nil, nil
}
func (m *mockIndexer) DeleteByFilehash(ctx context.Context, filehash string) error {
	m.deletedByHash = append(m.deletedByHash, filehash)
	return nil
}

func TestTrash_List(t *testing.T) {
	repo := repository.NewMockFileRepository()
	store := storage.NewLocal(t.TempDir())
	svc := NewFileService(repo, store, nil)
	ctx := context.Background()

	const hash1 = "1111111111111111111111111111111111111111"
	const hash2 = "2222222222222222222222222222222222222222"

	_ = repo.Create(ctx, model.File{FileSha1: hash1, FileSize: 100})
	_ = repo.Create(ctx, model.File{FileSha1: hash2, FileSize: 200})
	_ = repo.CreateUserFile(ctx, model.UserFile{Username: "alice", FileSha1: hash1, FileName: "file1.txt", Status: model.UserFileStatusActive})
	_ = repo.CreateUserFile(ctx, model.UserFile{Username: "alice", FileSha1: hash2, FileName: "file2.txt", Status: model.UserFileStatusActive})

	// 软删除 file1
	if err := svc.Delete(ctx, hash1, "alice"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// 活跃列表仅剩 file2
	activeFiles, total, err := svc.ListByUserPaged(ctx, "alice", 1, 10)
	if err != nil || total != 1 || len(activeFiles) != 1 || activeFiles[0].FileSha1 != hash2 {
		t.Fatalf("active files = %+v, total = %d (want 1 / file2)", activeFiles, total)
	}

	// 回收站列表包含 file1
	trashFiles, trashTotal, err := svc.ListTrash(ctx, "alice", 1, 10)
	if err != nil || trashTotal != 1 || len(trashFiles) != 1 || trashFiles[0].FileSha1 != hash1 {
		t.Fatalf("trash files = %+v, total = %d (want 1 / file1)", trashFiles, trashTotal)
	}

	// 分页越界返回空列表
	emptyTrash, _, _ := svc.ListTrash(ctx, "alice", 2, 10)
	if len(emptyTrash) != 0 {
		t.Errorf("expected empty trash for page 2, got %d items", len(emptyTrash))
	}
}

func TestTrash_Restore_And_AIReindex(t *testing.T) {
	repo := repository.NewMockFileRepository()
	store := storage.NewLocal(t.TempDir())
	svc := NewFileService(repo, store, nil)
	ctx := context.Background()

	const hash = "abcdef0123456789abcdef0123456789abcdef01"
	_ = repo.Create(ctx, model.File{FileSha1: hash, FileSize: 100})
	_ = repo.CreateUserFile(ctx, model.UserFile{Username: "alice", FileSha1: hash, FileName: "a.txt", Status: model.UserFileStatusActive})

	// 1. 删除
	if err := svc.Delete(ctx, hash, "alice"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// 2. 恢复
	if err := svc.Restore(ctx, hash, "alice"); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	// 恢复后活跃列表可见，回收站为空
	active, total, _ := svc.ListByUserPaged(ctx, "alice", 1, 10)
	if total != 1 || len(active) != 1 {
		t.Fatalf("active list = %d, want 1", total)
	}
	_, trashTotal, _ := svc.ListTrash(ctx, "alice", 1, 10)
	if trashTotal != 0 {
		t.Fatalf("trash total = %d, want 0", trashTotal)
	}

	// 3. 再次恢复（不在回收站中）报错
	if err := svc.Restore(ctx, hash, "alice"); err == nil {
		t.Fatal("expected error on restoring non-trash file, got nil")
	}

	// 4. 越权恢复（Bob 尝试恢复 Alice 的文件）报错
	_ = svc.Delete(ctx, hash, "alice")
	if err := svc.Restore(ctx, hash, "bob"); err == nil {
		t.Fatal("expected error for unauthorized user restore, got nil")
	}
}

func TestTrash_Purge_CascadeAndZeroReference(t *testing.T) {
	repo := repository.NewMockFileRepository()
	store := storage.NewLocal(t.TempDir())
	indexer := &mockIndexer{}
	svc := NewFileService(repo, store, nil).WithIndexer(indexer)
	ctx := context.Background()

	t.Run("multi-user shared file: purge does not delete storage object or global record", func(t *testing.T) {
		const sharedHash = "shared1111111111111111111111111111111111"
		content := []byte("shared multi user content")
		_ = repo.Create(ctx, model.File{FileSha1: sharedHash, FileName: "shared.txt", FileSize: int64(len(content))})
		_ = repo.CreateUserFile(ctx, model.UserFile{Username: "alice", FileSha1: sharedHash, FileName: "alice_shared.txt", Status: model.UserFileStatusActive})
		_ = repo.CreateUserFile(ctx, model.UserFile{Username: "bob", FileSha1: sharedHash, FileName: "bob_shared.txt", Status: model.UserFileStatusActive})
		_ = store.Put(ctx, sharedHash, bytes.NewReader(content), int64(len(content)))

		// Alice 软删除并彻底删除该文件
		_ = svc.Delete(ctx, sharedHash, "alice")
		if err := svc.Purge(ctx, sharedHash, "alice"); err != nil {
			t.Fatalf("Alice Purge failed: %v", err)
		}

		// Alice 的 UserFile 已删除
		if _, err := svc.GetMeta(ctx, sharedHash, "alice"); err == nil {
			t.Fatal("Alice should not own the file after purge")
		}

		// 存储层和全局记录必须保留，因为 Bob 依然拥有该文件
		exists, err := store.Exists(ctx, sharedHash)
		if err != nil || !exists {
			t.Fatal("storage file should still exist when referenced by other users")
		}
		bobMeta, err := svc.GetMeta(ctx, sharedHash, "bob")
		if err != nil || bobMeta.FileName != "bob_shared.txt" {
			t.Fatalf("Bob should still own the file: %v", err)
		}
	})

	t.Run("single-user zero reference: purge cascades to storage, global record and index", func(t *testing.T) {
		const singleHash = "single2222222222222222222222222222222222"
		content := []byte("single user private content")
		_ = repo.Create(ctx, model.File{FileSha1: singleHash, FileName: "single.txt", FileSize: int64(len(content))})
		_ = repo.CreateUserFile(ctx, model.UserFile{Username: "alice", FileSha1: singleHash, FileName: "single.txt", Status: model.UserFileStatusActive})
		_ = store.Put(ctx, singleHash, bytes.NewReader(content), int64(len(content)))

		_ = svc.Delete(ctx, singleHash, "alice")
		if err := svc.Purge(ctx, singleHash, "alice"); err != nil {
			t.Fatalf("Purge failed: %v", err)
		}

		// 存储层文件已被删除
		exists, _ := store.Exists(ctx, singleHash)
		if exists {
			t.Fatal("storage object should be removed on zero-reference purge")
		}

		// 全局记录已移除
		if _, err := repo.GetGlobalFile(ctx, singleHash); err == nil {
			t.Fatal("global file record should be removed on zero-reference purge")
		}

		// 检索引擎已通知清理
		foundIndexClean := false
		for _, h := range indexer.deletedByHash {
			if h == singleHash {
				foundIndexClean = true
				break
			}
		}
		if !foundIndexClean {
			t.Fatal("expected indexer.DeleteByFilehash to be called")
		}

		// 重复彻底删除应报错
		if err := svc.Purge(ctx, singleHash, "alice"); err == nil {
			t.Fatal("second purge should fail, got nil")
		}
	})
}
