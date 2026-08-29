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

// newTestDB creates sqlite in-memory DB and migrates all tables (consistent with MySQL behavior, pure Go without CGO)
// Each test uses independent DB name to avoid shared cache cross-contamination
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
	// Idempotent: repeated creation does not error
	if err := repo.Create(context.Background(), model.File{FileSha1: testHash, FileName: "b.txt", FileSize: 20}); err != nil {
		t.Fatalf("second create should be ignored: %v", err)
	}
	global, err := repo.GetGlobalFile(context.Background(), testHash)
	if err != nil {
		t.Fatalf("GetGlobalFile failed: %v", err)
	}
	// INSERT IGNORE semantics: first record not overwritten
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

	// Owner visible
	meta, err := repo.GetByHash(context.Background(), testHash, "alice")
	if err != nil {
		t.Fatalf("owner lookup failed: %v", err)
	}
	if meta.FileName != "a.txt" || meta.FileSize != 10 {
		t.Fatalf("meta mismatch: %+v", meta)
	}

	// Non-owner invisible
	if _, err := repo.GetByHash(context.Background(), testHash, "bob"); err == nil {
		t.Fatal("bob should not see alice's file")
	}

	// Idempotent: repeated association does not error or duplicate
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

	// Soft delete -> invisible in list, visible in trash
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

	// Restore -> visible again
	if ok, err := repo.Restore(context.Background(), testHash, "alice"); err != nil || !ok {
		t.Fatalf("restore failed: ok=%v err=%v", ok, err)
	}
	if _, err := repo.GetByHash(context.Background(), testHash, "alice"); err != nil {
		t.Fatalf("restored file should be visible: %v", err)
	}

	// Soft delete again -> purge
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
	// bob has no files
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

	// GC candidate: old file without active references
	if err := repo.RemoveOrphan(context.Background(), testHash); err != nil {
		t.Fatalf("RemoveOrphan failed: %v", err)
	}
	if _, err := repo.GetGlobalFile(context.Background(), testHash); err == nil {
		t.Fatal("orphan should be removed")
	}
	// ListOldest: time filter
	_ = repo.Create(context.Background(), model.File{FileSha1: testHash, FileSize: 1})
	if err := repo.RemoveOrphan(context.Background(), testHash); err == nil {
		// Already deleted, should be removable again after recreation (no references)
	}
	if old, err := repo.ListOldest(context.Background(), time.Now().Add(time.Hour)); err != nil || len(old) != 0 {
		t.Fatalf("ListOldest before any create should be empty, got %d err=%v", len(old), err)
	}
}

func TestFileRepoVFS(t *testing.T) {
	db := newTestDB(t)
	repo := NewFileRepository(db)
	ctx := context.Background()

	// 1. Create folder A under root
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

	// 2. Create subfolder B (/学习资料/Go/)
	folderB, err := repo.CreateFolder(ctx, model.UserFile{
		Username: "alice",
		ParentID: uint64(folderA.ID),
		FileName: "Go",
		DirPath:  "/学习资料/Go/",
	})
	if err != nil {
		t.Fatalf("create folderB failed: %v", err)
	}

	// 3. Create file under folderB
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

	// 4. Query files under folderB
	files, total, err := repo.ListByParent(ctx, "alice", uint64(folderB.ID), 0, 10)
	if err != nil {
		t.Fatalf("list by parent failed: %v", err)
	}
	if total != 1 || len(files) != 1 || files[0].FileName != "main.go" {
		t.Fatalf("expected 1 file in folderB, got %d files: %+v", total, files)
	}

	// 5. Breadcrumb test
	crumbs, err := repo.GetBreadcrumbs(ctx, "alice", uint64(folderB.ID))
	if err != nil {
		t.Fatalf("get breadcrumbs failed: %v", err)
	}
	if len(crumbs) != 3 { // All Files -> 学习资料 -> Go
		t.Fatalf("expected 3 breadcrumbs, got %d: %+v", len(crumbs), crumbs)
	}
	if crumbs[0].Name != "全部文件" || crumbs[1].Name != "学习资料" || crumbs[2].Name != "Go" {
		t.Fatalf("breadcrumb hierarchy mismatch: %+v", crumbs)
	}

	// 6. Rename and prefix migration
	err = repo.RenameItem(ctx, folderA.ID, "alice", "资料库", "/资料库/")
	if err != nil {
		t.Fatalf("rename folderA failed: %v", err)
	}
	updatedB, err := repo.GetUserFileByID(ctx, folderB.ID, "alice")
	if err != nil || updatedB.DirPath != "/资料库/Go/" {
		t.Fatalf("rename should atomically update subtree, got %+v err=%v", updatedB, err)
	}

	// 7. Soft-delete directory cascade
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
