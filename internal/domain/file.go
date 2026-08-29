package model

import "time"

// File 全局文件注册表，按 SHA1 去重
// 对应 tbl_file 表
type File struct {
	FileSha1 string    `gorm:"column:file_sha1;primaryKey;size:40"`
	FileName string    `gorm:"column:file_name;size:256;default:''"` // 首次上传时的文件名(秒传所有权关联复用)
	FileSize int64     `gorm:"column:file_size;default:0"`
	FileAddr string    `gorm:"column:file_addr;size:512;default:''"`
	Summary  string    `gorm:"column:file_summary;type:text"`   // AI 生成的内容摘要
	Tags     string    `gorm:"column:tags;size:255;default:''"` // AI 生成的标签，逗号分隔
	CreateAt time.Time `gorm:"column:create_at;autoCreateTime"`
}

func (File) TableName() string { return "tbl_file" }

// UserFile 用户拥有关系，支持虚拟文件系统（VFS）树形目录
// 对应 tbl_user_file 表
//
// Status 语义：
//
//	1 = UserFileStatusActive  用户拥有该文件（正常状态）
//	2 = UserFileStatusDeleted 用户已软删除该文件（回收站）
const (
	UserFileStatusActive  = 1
	UserFileStatusDeleted = 2
)

type UserFile struct {
	ID       uint      `gorm:"primaryKey;autoIncrement"`
	Username string    `gorm:"column:user_name;size:64;not null;index:idx_user_parent;index:idx_user_path;index:idx_user_sha1"`
	ParentID uint64    `gorm:"column:parent_id;default:0;not null;index:idx_user_parent"`
	IsDir    int       `gorm:"column:is_dir;default:0;not null"`
	DirPath  string    `gorm:"column:dir_path;size:512;default:'/';not null;index:idx_user_path"`
	FileSha1 string    `gorm:"column:file_sha1;size:40;not null;index:idx_user_sha1"`
	FileName string    `gorm:"column:file_name;size:256;default:''"`
	CreateAt time.Time `gorm:"column:create_at;autoCreateTime"`
	Status   int       `gorm:"column:status;default:1;index:idx_user_parent"`
}

func (UserFile) TableName() string { return "tbl_user_file" }

// FileMeta 文件元信息 DTO，用于 handler 返回 JSON
type FileMeta struct {
	ID       uint   `json:"id,omitempty"`
	FileSha1 string `json:"filehash"`
	FileName string `json:"filename"`
	FileSize int64  `json:"size"`
	Username string `json:"username"`
	ParentID uint64 `json:"parent_id"`
	IsDir    int    `json:"is_dir"`
	DirPath  string `json:"dir_path"`
	UploadAt string `json:"upload_time"` // 字符串格式，方便前端直接展示
	Summary  string `json:"summary"`     // AI 生成摘要，未分析时为空
	Tags     string `json:"tags"`        // AI 生成标签，逗号分隔
}

// Breadcrumb 面包屑路径导航节点
type Breadcrumb struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// FolderCreateReq 创建文件夹请求 DTO
type FolderCreateReq struct {
	Name     string `json:"name" binding:"required"`
	ParentID uint64 `json:"parent_id"`
}

// FolderRenameReq 重命名文件夹或文件请求 DTO
type FolderRenameReq struct {
	FileID  uint   `json:"file_id" binding:"required"`
	NewName string `json:"new_name" binding:"required"`
}

// FolderMoveReq 移动文件或文件夹请求 DTO
type FolderMoveReq struct {
	FileID         uint   `json:"file_id" binding:"required"`
	TargetParentID uint64 `json:"target_parent_id"`
}
