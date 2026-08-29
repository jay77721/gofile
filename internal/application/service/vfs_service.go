package service

import (
	"context"
	"fmt"
	"gofile/internal/domain"
	"log/slog"
	"strings"
)

// ---- VFS 虚拟文件系统目录管理 ----

// CreateFolder 创建新文件夹
func (s *FileService) CreateFolder(ctx context.Context, username string, req model.FolderCreateReq) (model.UserFile, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return model.UserFile{}, fmt.Errorf("invalid folder name")
	}

	dirPath := "/" + name + "/"
	if req.ParentID != 0 {
		parent, err := s.fileRepo.GetUserFileByID(ctx, uint(req.ParentID), username)
		if err != nil || parent.IsDir != 1 || parent.Status != model.UserFileStatusActive {
			return model.UserFile{}, fmt.Errorf("parent folder does not exist")
		}
		dirPath = parent.DirPath + name + "/"
	}

	uf, err := s.fileRepo.CreateFolder(ctx, model.UserFile{
		Username: username,
		ParentID: req.ParentID,
		FileName: name,
		DirPath:  dirPath,
		IsDir:    1,
		Status:   model.UserFileStatusActive,
	})
	if err != nil {
		return model.UserFile{}, fmt.Errorf("create folder failed: %w", err)
	}

	slog.InfoContext(ctx, "folder created", "name", name, "path", dirPath, "username", username)
	return uf, nil
}

// RenameFolderOrFile 重命名文件或文件夹
func (s *FileService) RenameFolderOrFile(ctx context.Context, username string, req model.FolderRenameReq) error {
	newName := strings.TrimSpace(req.NewName)
	if newName == "" || strings.Contains(newName, "/") || strings.Contains(newName, "\\") || strings.Contains(newName, "..") {
		return fmt.Errorf("invalid new name")
	}

	uf, err := s.fileRepo.GetUserFileByID(ctx, req.FileID, username)
	if err != nil || uf.Status != model.UserFileStatusActive {
		return fmt.Errorf("file or folder not found")
	}

	if uf.IsDir == 1 {
		// 计算当前父级目录路径
		parentDirPath := strings.TrimSuffix(uf.DirPath, uf.FileName+"/")
		newDirPath := parentDirPath + newName + "/"
		return s.fileRepo.RenameItem(ctx, req.FileID, username, newName, newDirPath)
	}

	return s.fileRepo.RenameItem(ctx, req.FileID, username, newName, uf.DirPath)
}

// MoveFolderOrFile 移动文件或文件夹（含防循环嵌套检查）
func (s *FileService) MoveFolderOrFile(ctx context.Context, username string, req model.FolderMoveReq) error {
	uf, err := s.fileRepo.GetUserFileByID(ctx, req.FileID, username)
	if err != nil || uf.Status != model.UserFileStatusActive {
		return fmt.Errorf("item not found")
	}

	targetDirPath := "/"
	if req.TargetParentID != 0 {
		parent, err := s.fileRepo.GetUserFileByID(ctx, uint(req.TargetParentID), username)
		if err != nil || parent.IsDir != 1 || parent.Status != model.UserFileStatusActive {
			return fmt.Errorf("target folder not found")
		}
		targetDirPath = parent.DirPath
	}

	if uf.IsDir == 1 {
		// 防循环移动检测：不能将文件夹移入自身子目录下
		if isVFSPathWithin(targetDirPath, uf.DirPath) {
			return fmt.Errorf("cannot move folder into its own subfolder")
		}
		newDirPath := targetDirPath + uf.FileName + "/"
		return s.fileRepo.MoveItem(ctx, req.FileID, username, req.TargetParentID, newDirPath)
	}

	return s.fileRepo.MoveItem(ctx, req.FileID, username, req.TargetParentID, targetDirPath)
}

// isVFSPathWithin reports whether path is the directory itself or a descendant
// of prefix. The separator is part of the comparison so /a/ never matches /ab/.
func isVFSPathWithin(path, prefix string) bool {
	if prefix == "/" {
		return strings.HasPrefix(path, "/")
	}
	prefix = strings.TrimSuffix(prefix, "/") + "/"
	return path == prefix || strings.HasPrefix(path, prefix)
}

// QueryDirectory 查询指定目录下的文件列表与面包屑导航
func (s *FileService) QueryDirectory(ctx context.Context, username string, parentID uint64, offset, limit int) ([]model.FileMeta, int64, []model.Breadcrumb, error) {
	files, total, err := s.fileRepo.ListByParent(ctx, username, parentID, offset, limit)
	if err != nil {
		return nil, 0, nil, err
	}
	crumbs, err := s.fileRepo.GetBreadcrumbs(ctx, username, parentID)
	if err != nil {
		crumbs = []model.Breadcrumb{{ID: 0, Name: "全部文件", Path: "/"}}
	}
	return files, total, crumbs, nil
}
