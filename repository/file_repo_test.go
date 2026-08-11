package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"gofile/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newTestDB 创建 sqlite 内存库并迁移全部表(与 MySQL 行为一致,纯 Go 无 CGO)
// 每个测试使用独立库名,避免 shared cache 串库
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(
		&model.File{}, &model.UserFile{}, &model.User{},
		&model.Token{}, &model.AITask{}, &model.Share{}, &model.AIConfig{},
	); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	return db
}

const (
	testHash = "0123456789abcdef0123456789abcdef01234567"
)

func TestFileRepoCreateIdempotent(t *testing.T) {
	db := newTestDB(t)
	repo := NewFileRepository(db)

	f := model.File{FileSha1: testHash, FileName: "a.txt", FileSize: 10}
	if err := repo.Create(context.Background(), f); err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	// 幂等:重复创建不报错
	if err := repo.Create(context.Background(), model.File{FileSha1: testHash, FileName: "b.txt", FileSize: 20}); err != nil {
		t.Fatalf("second create should be ignored: %v", err)
	}
	global, err := repo.GetGlobalFile(context.Background(), testHash)
	if err != nil {
		t.Fatalf("GetGlobalFile failed: %v", err)
	}
	// INSERT IGNORE 语义:首次记录不被覆盖
	if global.FileName != "a.txt" || global.FileSize != 10 {
		t.Fatalf("expected first record preserved, got %+v", global)
	}
}

func TestFileRepoOwnership(t *testing.T) {
	db := newTestDB(t)
	repo := NewFileRepository(db)

	if err := repo.Create(context.Background(), model.File{FileSha1: testHash, FileName: "a.txt", FileSize: 10}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateUserFile(context.Background(), model.UserFile{Username: "alice", FileSha1: testHash, FileName: "a.txt", Status: model.UserFileStatusActive}); err != nil {
		t.Fatal(err)
	}

	// 所有者可见
	meta, err := repo.GetByHash(context.Background(), testHash, "alice")
	if err != nil {
		t.Fatalf("owner lookup failed: %v", err)
	}
	if meta.FileName != "a.txt" || meta.FileSize != 10 {
		t.Fatalf("meta mismatch: %+v", meta)
	}

	// 非所有者不可见
	if _, err := repo.GetByHash(context.Background(), testHash, "bob"); err == nil {
		t.Fatal("bob should not see alice's file")
	}

	// 幂等:重复建立关联不报错、不重复
	if err := repo.CreateUserFile(context.Background(), model.UserFile{Username: "alice", FileSha1: testHash, FileName: "a.txt", Status: model.UserFileStatusActive}); err != nil {
		t.Fatalf("duplicate user file should be ignored: %v", err)
	}
	if n, _ := repo.CountByUser(context.Background(), "alice"); n != 1 {
		t.Fatalf("expected 1 file for alice, got %d", n)
	}
}

func TestFileRepoTrashLifecycle(t *testing.T) {
	db := newTestDB(t)
	repo := NewFileRepository(db)

	_ = repo.Create(context.Background(), model.File{FileSha1: testHash, FileName: "a.txt", FileSize: 10})
	_ = repo.CreateUserFile(context.Background(), model.UserFile{Username: "alice", FileSha1: testHash, FileName: "a.txt", Status: model.UserFileStatusActive})

	// 软删 → 列表不可见、回收站可见
	ok, err := repo.Delete(context.Background(), testHash, "alice")
	if err != nil || !ok {
		t.Fatalf("delete failed: ok=%v err=%v", ok, err)
	}
	if _, err := repo.GetByHash(context.Background(), testHash, "alice"); err == nil {
		t.Fatal("soft-deleted file should be invisible")
	}
	list, total, err := repo.ListTrash(context.Background(), "alice", 1, 10)
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("expected 1 trash item, got total=%d len=%d err=%v", total, len(list), err)
	}

	// 恢复 → 重新可见
	if ok, err := repo.Restore(context.Background(), testHash, "alice"); err != nil || !ok {
		t.Fatalf("restore failed: ok=%v err=%v", ok, err)
	}
	if _, err := repo.GetByHash(context.Background(), testHash, "alice"); err != nil {
		t.Fatalf("restored file should be visible: %v", err)
	}

	// 再次软删 → 彻底删除
	_, _ = repo.Delete(context.Background(), testHash, "alice")
	if ok, err := repo.PurgeUserFile(context.Background(), testHash, "alice"); err != nil || !ok {
		t.Fatalf("purge failed: ok=%v err=%v", ok, err)
	}
	if _, err := repo.GetByHash(context.Background(), testHash, "alice"); err == nil {
		t.Fatal("purged file should be gone")
	}
}

func TestFileRepoListByUserPaged(t *testing.T) {
	db := newTestDB(t)
	repo := NewFileRepository(db)

	for i := 0; i < 3; i++ {
		h := testHash[:39] + string(rune('a'+i))
		_ = repo.Create(context.Background(), model.File{FileSha1: h, FileName: "f", FileSize: 1})
		_ = repo.CreateUserFile(context.Background(), model.UserFile{Username: "alice", FileSha1: h, FileName: "f", Status: model.UserFileStatusActive})
	}
	page, err := repo.ListByUserPaged(context.Background(), "alice", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 {
		t.Fatalf("page 2 size 2 of 3 items should return 1, got %d", len(page))
	}
	// bob 无文件
	empty, err := repo.ListByUserPaged(context.Background(), "bob", 1, 10)
	if err != nil || len(empty) != 0 {
		t.Fatalf("bob should have 0 files, got %d err=%v", len(empty), err)
	}
}

func TestFileRepoSaveAnalysisAndGC(t *testing.T) {
	db := newTestDB(t)
	repo := NewFileRepository(db)

	_ = repo.Create(context.Background(), model.File{FileSha1: testHash, FileName: "a.txt", FileSize: 10})
	if err := repo.SaveAnalysis(context.Background(), testHash, "摘要", "标签1,标签2"); err != nil {
		t.Fatalf("SaveAnalysis failed: %v", err)
	}
	global, err := repo.GetGlobalFile(context.Background(), testHash)
	if err != nil || global.Summary != "摘要" || global.Tags != "标签1,标签2" {
		t.Fatalf("analysis not saved: %+v err=%v", global, err)
	}

	// GC 候选:无活跃引用的旧文件
	if err := repo.RemoveOrphan(context.Background(), testHash); err != nil {
		t.Fatalf("RemoveOrphan failed: %v", err)
	}
	if _, err := repo.GetGlobalFile(context.Background(), testHash); err == nil {
		t.Fatal("orphan should be removed")
	}
	// ListOldest:时间过滤
	_ = repo.Create(context.Background(), model.File{FileSha1: testHash, FileSize: 1})
	if err := repo.RemoveOrphan(context.Background(), testHash); err == nil {
		// 已删除,重建后应可再次移除(无引用)
	}
	if old, err := repo.ListOldest(context.Background(), time.Now().Add(time.Hour)); err != nil || len(old) != 0 {
		t.Fatalf("ListOldest before any create should be empty, got %d err=%v", len(old), err)
	}
}
