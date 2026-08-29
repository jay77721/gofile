package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"gofile/internal/domain"

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

func TestFileRepoVFS(t *testing.T) {
	db := newTestDB(t)
	repo := NewFileRepository(db)
	ctx := context.Background()

	// 1. 创建根目录下的文件夹 A
	folderA, err := repo.CreateFolder(ctx, model.UserFile{
		Username: "alice",
		ParentID: 0,
		FileName: "学习资料",
		DirPath:  "/学习资料/",
	})
	if err != nil {
		t.Fatalf("create folderA failed: %v", err)
	}
	if folderA.ID == 0 || folderA.IsDir != 1 {
		t.Fatalf("expected folder with is_dir=1 and ID>0, got %+v", folderA)
	}

	// 2. 创建子文件夹 B (/学习资料/Go/)
	folderB, err := repo.CreateFolder(ctx, model.UserFile{
		Username: "alice",
		ParentID: uint64(folderA.ID),
		FileName: "Go",
		DirPath:  "/学习资料/Go/",
	})
	if err != nil {
		t.Fatalf("create folderB failed: %v", err)
	}

	// 3. 在 folderB 下创建文件
	_ = repo.Create(ctx, model.File{FileSha1: testHash, FileName: "main.go", FileSize: 100})
	if err := repo.CreateUserFile(ctx, model.UserFile{
		Username: "alice",
		ParentID: uint64(folderB.ID),
		FileSha1: testHash,
		FileName: "main.go",
		DirPath:  "/学习资料/Go/",
		Status:   model.UserFileStatusActive,
	}); err != nil {
		t.Fatalf("create user file in folderB failed: %v", err)
	}

	// 4. 查询 folderB 下的文件
	files, total, err := repo.ListByParent(ctx, "alice", uint64(folderB.ID), 0, 10)
	if err != nil {
		t.Fatalf("list by parent failed: %v", err)
	}
	if total != 1 || len(files) != 1 || files[0].FileName != "main.go" {
		t.Fatalf("expected 1 file in folderB, got %d files: %+v", total, files)
	}

	// 5. 面包屑测试
	crumbs, err := repo.GetBreadcrumbs(ctx, "alice", uint64(folderB.ID))
	if err != nil {
		t.Fatalf("get breadcrumbs failed: %v", err)
	}
	if len(crumbs) != 3 { // 全部文件 -> 学习资料 -> Go
		t.Fatalf("expected 3 breadcrumbs, got %d: %+v", len(crumbs), crumbs)
	}
	if crumbs[0].Name != "全部文件" || crumbs[1].Name != "学习资料" || crumbs[2].Name != "Go" {
		t.Fatalf("breadcrumb hierarchy mismatch: %+v", crumbs)
	}

	// 6. 重命名与前缀迁移
	err = repo.RenameItem(ctx, folderA.ID, "alice", "资料库", "/资料库/")
	if err != nil {
		t.Fatalf("rename folderA failed: %v", err)
	}
	updatedB, err := repo.GetUserFileByID(ctx, folderB.ID, "alice")
	if err != nil || updatedB.DirPath != "/资料库/Go/" {
		t.Fatalf("rename should atomically update subtree, got %+v err=%v", updatedB, err)
	}

	// 7. 软删除目录级联
	err = repo.SoftDeleteDir(ctx, "alice", "/资料库/")
	if err != nil {
		t.Fatalf("soft delete dir failed: %v", err)
	}
	listAfterDelete, totalAfter, _ := repo.ListByParent(ctx, "alice", uint64(folderB.ID), 0, 10)
	if totalAfter != 0 || len(listAfterDelete) != 0 {
		t.Fatalf("expected 0 files after cascade delete, got %d", totalAfter)
	}
}

func TestFileRepoVFSPathBoundary(t *testing.T) {
	db := newTestDB(t)
	repo := NewFileRepository(db)
	ctx := context.Background()

	keep, err := repo.CreateFolder(ctx, model.UserFile{Username: "alice", FileName: "foobar", DirPath: "/foobar/"})
	if err != nil {
		t.Fatal(err)
	}
	move, err := repo.CreateFolder(ctx, model.UserFile{Username: "alice", FileName: "foo", DirPath: "/foo/"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := repo.CreateFolder(ctx, model.UserFile{Username: "alice", ParentID: uint64(move.ID), FileName: "child", DirPath: "/foo/child/"})
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.UpdateDirPathPrefix(ctx, "alice", "/foo/", "/renamed/"); err != nil {
		t.Fatal(err)
	}
	kept, err := repo.GetUserFileByID(ctx, keep.ID, "alice")
	if err != nil || kept.DirPath != "/foobar/" {
		t.Fatalf("sibling prefix was changed: %+v err=%v", kept, err)
	}
	updated, err := repo.GetUserFileByID(ctx, child.ID, "alice")
	if err != nil || updated.DirPath != "/renamed/child/" {
		t.Fatalf("descendant was not changed: %+v err=%v", updated, err)
	}
}

func TestFileRepoVFSWriteMissingOrDeleted(t *testing.T) {
	db := newTestDB(t)
	repo := NewFileRepository(db)
	ctx := context.Background()

	if err := repo.RenameItem(ctx, 999, "alice", "new", "/new/"); err == nil {
		t.Fatal("rename of missing item should fail")
	}
	folder, err := repo.CreateFolder(ctx, model.UserFile{Username: "alice", FileName: "folder", DirPath: "/folder/"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SoftDeleteDir(ctx, "alice", folder.DirPath); err != nil {
		t.Fatal(err)
	}
	if err := repo.RenameItem(ctx, folder.ID, "alice", "new", "/new/"); err == nil {
		t.Fatal("rename of deleted item should fail")
	}
	if err := repo.UpdateDirPathPrefix(ctx, "alice", "/missing/", "/new/"); err == nil {
		t.Fatal("prefix update with no matching rows should fail")
	}
}
