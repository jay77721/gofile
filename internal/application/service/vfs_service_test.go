package service

import (
	"context"
	"gofile/internal/domain"
	"gofile/internal/infrastructure/persistence/repository"
	"gofile/internal/infrastructure/storage"
	"testing"
)

func TestVFS_CreateFolder(t *testing.T) {
	repo := repository.NewMockFileRepository()
	store := storage.NewLocal(t.TempDir())
	svc := NewFileService(repo, store, nil)
	ctx := context.Background()

	t.Run("create root folder", func(t *testing.T) {
		folder, err := svc.CreateFolder(ctx, "alice", model.FolderCreateReq{
			Name:     "Docs",
			ParentID: 0,
		})
		if err != nil {
			t.Fatalf("CreateFolder failed: %v", err)
		}
		if folder.DirPath != "/Docs/" {
			t.Errorf("dirPath = %q, want %q", folder.DirPath, "/Docs/")
		}
		if folder.IsDir != 1 {
			t.Errorf("isDir = %d, want 1", folder.IsDir)
		}
		if folder.Status != model.UserFileStatusActive {
			t.Errorf("status = %d, want %d", folder.Status, model.UserFileStatusActive)
		}
	})

	t.Run("create nested subfolders", func(t *testing.T) {
		root, err := svc.CreateFolder(ctx, "alice", model.FolderCreateReq{
			Name:     "Projects",
			ParentID: 0,
		})
		if err != nil {
			t.Fatalf("create root failed: %v", err)
		}

		sub, err := svc.CreateFolder(ctx, "alice", model.FolderCreateReq{
			Name:     "GoFile",
			ParentID: uint64(root.ID),
		})
		if err != nil {
			t.Fatalf("create subfolder failed: %v", err)
		}
		if sub.DirPath != "/Projects/GoFile/" {
			t.Errorf("sub dirPath = %q, want %q", sub.DirPath, "/Projects/GoFile/")
		}

		grandchild, err := svc.CreateFolder(ctx, "alice", model.FolderCreateReq{
			Name:     "service",
			ParentID: uint64(sub.ID),
		})
		if err != nil {
			t.Fatalf("create grandchild failed: %v", err)
		}
		if grandchild.DirPath != "/Projects/GoFile/service/" {
			t.Errorf("grandchild dirPath = %q, want %q", grandchild.DirPath, "/Projects/GoFile/service/")
		}
	})

	t.Run("invalid folder names", func(t *testing.T) {
		invalidNames := []string{"", "   ", "a/b", "a\\b", "..", "../evil", "folder/sub"}
		for _, name := range invalidNames {
			_, err := svc.CreateFolder(ctx, "alice", model.FolderCreateReq{
				Name:     name,
				ParentID: 0,
			})
			if err == nil {
				t.Errorf("expected error for invalid folder name %q, got nil", name)
			}
		}
	})

	t.Run("parent not found or invalid", func(t *testing.T) {
		// Non-existent parent ID
		_, err := svc.CreateFolder(ctx, "alice", model.FolderCreateReq{
			Name:     "Child",
			ParentID: 99999,
		})
		if err == nil {
			t.Errorf("expected error for non-existent parent, got nil")
		}

		// Parent exists but is a file, not a folder
		if err := repo.CreateUserFile(ctx, model.UserFile{
			Username: "alice",
			FileName: "plain.txt",
			FileSha1: "abcdef0123456789abcdef0123456789abcdef01",
			IsDir:    0,
			Status:   model.UserFileStatusActive,
		}); err != nil {
			t.Fatalf("create test file failed: %v", err)
		}

		// Get the file ID
		items, _ := repo.ListByUser(ctx, "alice")
		if len(items) > 0 {
			_, err = svc.CreateFolder(ctx, "alice", model.FolderCreateReq{
				Name:     "ChildOfFile",
				ParentID: uint64(items[0].ID),
			})
			if err == nil {
				t.Errorf("expected error when parent is not a directory, got nil")
			}
		}
	})
}

func TestVFS_RenameFolderOrFile(t *testing.T) {
	repo := repository.NewMockFileRepository()
	store := storage.NewLocal(t.TempDir())
	svc := NewFileService(repo, store, nil)
	ctx := context.Background()

	// 1. Create folder tree /Media/Photos/
	media, err := svc.CreateFolder(ctx, "alice", model.FolderCreateReq{Name: "Media", ParentID: 0})
	if err != nil {
		t.Fatalf("create Media failed: %v", err)
	}
	photos, err := svc.CreateFolder(ctx, "alice", model.FolderCreateReq{Name: "Photos", ParentID: uint64(media.ID)})
	if err != nil {
		t.Fatalf("create Photos failed: %v", err)
	}

	// 2. Create child file
	if err := repo.CreateUserFile(ctx, model.UserFile{
		Username: "alice",
		ParentID: uint64(photos.ID),
		FileName: "pic.jpg",
		FileSha1: "1111111111111111111111111111111111111111",
		DirPath:  photos.DirPath,
		IsDir:    0,
		Status:   model.UserFileStatusActive,
	}); err != nil {
		t.Fatalf("create pic.jpg failed: %v", err)
	}

	t.Run("rename root folder updates subtree dir_paths", func(t *testing.T) {
		err := svc.RenameFolderOrFile(ctx, "alice", model.FolderRenameReq{
			FileID:  media.ID,
			NewName: "Assets",
		})
		if err != nil {
			t.Fatalf("RenameFolderOrFile failed: %v", err)
		}

		// Verify new path of root folder
		updatedMedia, err := repo.GetUserFileByID(ctx, media.ID, "alice")
		if err != nil || updatedMedia.DirPath != "/Assets/" || updatedMedia.FileName != "Assets" {
			t.Fatalf("updated media = %+v, want DirPath /Assets/, name Assets", updatedMedia)
		}

		// Verify child folder prefix auto-updated
		updatedPhotos, err := repo.GetUserFileByID(ctx, photos.ID, "alice")
		if err != nil || updatedPhotos.DirPath != "/Assets/Photos/" {
			t.Fatalf("updated photos = %+v, want DirPath /Assets/Photos/", updatedPhotos)
		}
	})

	t.Run("rename file keeps dir_path", func(t *testing.T) {
		items, _, _, err := svc.QueryDirectory(ctx, "alice", uint64(photos.ID), 0, 10)
		if err != nil || len(items) == 0 {
			t.Fatalf("QueryDirectory failed or empty: %v", err)
		}
		fileID := items[0].ID

		err = svc.RenameFolderOrFile(ctx, "alice", model.FolderRenameReq{
			FileID:  fileID,
			NewName: "vacation.jpg",
		})
		if err != nil {
			t.Fatalf("rename file failed: %v", err)
		}

		updatedFile, err := repo.GetUserFileByID(ctx, fileID, "alice")
		if err != nil || updatedFile.FileName != "vacation.jpg" {
			t.Fatalf("updated file = %+v, want FileName vacation.jpg", updatedFile)
		}
	})

	t.Run("invalid rename params", func(t *testing.T) {
		invalidNames := []string{"", "  ", "new/name", "new\\name", ".."}
		for _, name := range invalidNames {
			err := svc.RenameFolderOrFile(ctx, "alice", model.FolderRenameReq{
				FileID:  media.ID,
				NewName: name,
			})
			if err == nil {
				t.Errorf("expected error for invalid name %q, got nil", name)
			}
		}

		// Item not found
		err := svc.RenameFolderOrFile(ctx, "alice", model.FolderRenameReq{
			FileID:  99999,
			NewName: "valid",
		})
		if err == nil {
			t.Errorf("expected error for non-existent file_id, got nil")
		}

		// Unauthorized user
		err = svc.RenameFolderOrFile(ctx, "bob", model.FolderRenameReq{
			FileID:  media.ID,
			NewName: "valid",
		})
		if err == nil {
			t.Errorf("expected error for unauthorized user, got nil")
		}
	})
}

func TestVFS_MoveFolderOrFile_And_CyclePrevention(t *testing.T) {
	repo := repository.NewMockFileRepository()
	store := storage.NewLocal(t.TempDir())
	svc := NewFileService(repo, store, nil)
	ctx := context.Background()

	// Initialize structure:
	// /A/
	// /A/B/
	// /A/B/C/
	// /Target/
	folderA, err := svc.CreateFolder(ctx, "alice", model.FolderCreateReq{Name: "A", ParentID: 0})
	if err != nil {
		t.Fatal(err)
	}
	folderB, err := svc.CreateFolder(ctx, "alice", model.FolderCreateReq{Name: "B", ParentID: uint64(folderA.ID)})
	if err != nil {
		t.Fatal(err)
	}
	folderC, err := svc.CreateFolder(ctx, "alice", model.FolderCreateReq{Name: "C", ParentID: uint64(folderB.ID)})
	if err != nil {
		t.Fatal(err)
	}
	target, err := svc.CreateFolder(ctx, "alice", model.FolderCreateReq{Name: "Target", ParentID: 0})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("deep cycle prevention: moving /A/ to /A/B/ must fail", func(t *testing.T) {
		err := svc.MoveFolderOrFile(ctx, "alice", model.FolderMoveReq{
			FileID:         folderA.ID,
			TargetParentID: uint64(folderB.ID),
		})
		if err == nil {
			t.Fatal("expected error when moving /A/ to /A/B/, got nil")
		}
		if err.Error() != "cannot move folder into its own subfolder" {
			t.Errorf("err = %q, want 'cannot move folder into its own subfolder'", err.Error())
		}
	})

	t.Run("deep cycle prevention: moving /A/ to /A/B/C/ must fail", func(t *testing.T) {
		err := svc.MoveFolderOrFile(ctx, "alice", model.FolderMoveReq{
			FileID:         folderA.ID,
			TargetParentID: uint64(folderC.ID),
		})
		if err == nil {
			t.Fatal("expected error when moving /A/ to /A/B/C/, got nil")
		}
	})

	t.Run("deep cycle prevention: moving /A/ to itself must fail", func(t *testing.T) {
		err := svc.MoveFolderOrFile(ctx, "alice", model.FolderMoveReq{
			FileID:         folderA.ID,
			TargetParentID: uint64(folderA.ID),
		})
		if err == nil {
			t.Fatal("expected error when moving /A/ to itself, got nil")
		}
	})

	t.Run("multi-level valid move: moving /A/B/ to /Target/", func(t *testing.T) {
		err := svc.MoveFolderOrFile(ctx, "alice", model.FolderMoveReq{
			FileID:         folderB.ID,
			TargetParentID: uint64(target.ID),
		})
		if err != nil {
			t.Fatalf("valid move failed: %v", err)
		}

		// Verify new path of B is /Target/B/ and ParentID is target.ID
		updatedB, err := repo.GetUserFileByID(ctx, folderB.ID, "alice")
		if err != nil || updatedB.DirPath != "/Target/B/" || updatedB.ParentID != uint64(target.ID) {
			t.Fatalf("updated B = %+v, want DirPath /Target/B/, ParentID %d", updatedB, target.ID)
		}

		// Verify prefix of B's child C auto-updated to /Target/B/C/
		updatedC, err := repo.GetUserFileByID(ctx, folderC.ID, "alice")
		if err != nil || updatedC.DirPath != "/Target/B/C/" {
			t.Fatalf("updated C = %+v, want DirPath /Target/B/C/", updatedC)
		}
	})

	t.Run("move to non-existent target parent fails", func(t *testing.T) {
		err := svc.MoveFolderOrFile(ctx, "alice", model.FolderMoveReq{
			FileID:         folderA.ID,
			TargetParentID: 88888,
		})
		if err == nil {
			t.Fatal("expected error for non-existent target parent, got nil")
		}
	})
}

func TestVFS_QueryDirectory_And_Breadcrumbs(t *testing.T) {
	repo := repository.NewMockFileRepository()
	store := storage.NewLocal(t.TempDir())
	svc := NewFileService(repo, store, nil)
	ctx := context.Background()

	// 1. Create /RootFolder/SubFolder/
	rootFolder, _ := svc.CreateFolder(ctx, "alice", model.FolderCreateReq{Name: "RootFolder", ParentID: 0})
	subFolder, _ := svc.CreateFolder(ctx, "alice", model.FolderCreateReq{Name: "SubFolder", ParentID: uint64(rootFolder.ID)})

	// 2. Add files under rootFolder and subFolder
	_ = repo.Create(ctx, model.File{FileSha1: "hash1", FileSize: 100})
	_ = repo.Create(ctx, model.File{FileSha1: "hash2", FileSize: 200})
	_ = repo.CreateUserFile(ctx, model.UserFile{
		Username: "alice",
		ParentID: uint64(rootFolder.ID),
		FileName: "doc1.pdf",
		FileSha1: "hash1",
		DirPath:  rootFolder.DirPath,
		IsDir:    0,
		Status:   model.UserFileStatusActive,
	})
	_ = repo.CreateUserFile(ctx, model.UserFile{
		Username: "alice",
		ParentID: uint64(subFolder.ID),
		FileName: "doc2.pdf",
		FileSha1: "hash2",
		DirPath:  subFolder.DirPath,
		IsDir:    0,
		Status:   model.UserFileStatusActive,
	})

	t.Run("query root level (parent_id = 0)", func(t *testing.T) {
		files, total, crumbs, err := svc.QueryDirectory(ctx, "alice", 0, 0, 10)
		if err != nil {
			t.Fatalf("QueryDirectory failed: %v", err)
		}
		if total != 1 || len(files) != 1 {
			t.Errorf("total/len = %d/%d, want 1/1", total, len(files))
		}
		if len(crumbs) != 1 || crumbs[0].Name != "全部文件" || crumbs[0].Path != "/" {
			t.Errorf("crumbs = %+v, want root crumb", crumbs)
		}
	})

	t.Run("query nested folder (parent_id = subFolder.ID)", func(t *testing.T) {
		files, total, crumbs, err := svc.QueryDirectory(ctx, "alice", uint64(subFolder.ID), 0, 10)
		if err != nil {
			t.Fatalf("QueryDirectory failed: %v", err)
		}
		if total != 1 || len(files) != 1 || files[0].FileName != "doc2.pdf" {
			t.Errorf("files = %+v, total = %d, want doc2.pdf", files, total)
		}

		// Breadcrumbs should be: All Files (/) -> RootFolder(/RootFolder/) -> SubFolder(/RootFolder/SubFolder/)
		if len(crumbs) != 3 {
			t.Fatalf("expected 3 breadcrumbs, got %d: %+v", len(crumbs), crumbs)
		}
		if crumbs[0].Path != "/" || crumbs[1].Name != "RootFolder" || crumbs[2].Name != "SubFolder" {
			t.Errorf("unexpected breadcrumbs order/content: %+v", crumbs)
		}
	})
}
