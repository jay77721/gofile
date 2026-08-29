package model

import "time"

// File is the global file registry, deduped by SHA1.
// Corresponds to tbl_file.
type File struct {
	FileSha1 string    `gorm:"column:file_sha1;primaryKey;size:40"`
	FileName string    `gorm:"column:file_name;size:256;default:''"` // filename at first upload (reused for fast-dedup ownership binding)
	FileSize int64     `gorm:"column:file_size;default:0"`
	FileAddr string    `gorm:"column:file_addr;size:512;default:''"`
	Summary  string    `gorm:"column:file_summary;type:text"`   // AI-generated content summary
	Tags     string    `gorm:"column:tags;size:255;default:''"` // AI-generated tags, comma separated
	CreateAt time.Time `gorm:"column:create_at;autoCreateTime"`
}

func (File) TableName() string { return "tbl_file" }

// UserFile is the user ownership relation supporting VFS hierarchical directories.
// Corresponds to tbl_user_file.
//
// Status semantics:
//
//	1 = UserFileStatusActive  user owns the file (normal)
//	2 = UserFileStatusDeleted user has soft-deleted the file (trash)
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

// FileMeta is the file metadata DTO returned as JSON by handlers.
type FileMeta struct {
	ID       uint   `json:"id,omitempty"`
	FileSha1 string `json:"filehash"`
	FileName string `json:"filename"`
	FileSize int64  `json:"size"`
	Username string `json:"username"`
	ParentID uint64 `json:"parent_id"`
	IsDir    int    `json:"is_dir"`
	DirPath  string `json:"dir_path"`
	UploadAt string `json:"upload_time"` // string format for direct frontend display
	Summary  string `json:"summary"`     // AI-generated summary, empty before analysis
	Tags     string `json:"tags"`        // AI-generated tags, comma separated
}

// Breadcrumb is a breadcrumb navigation node.
type Breadcrumb struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// FolderCreateReq is the create-folder request DTO.
type FolderCreateReq struct {
	Name     string `json:"name" binding:"required"`
	ParentID uint64 `json:"parent_id"`
}

// FolderRenameReq is the rename file/folder request DTO.
type FolderRenameReq struct {
	FileID  uint   `json:"file_id" binding:"required"`
	NewName string `json:"new_name" binding:"required"`
}

// FolderMoveReq is the move file/folder request DTO.
type FolderMoveReq struct {
	FileID         uint   `json:"file_id" binding:"required"`
	TargetParentID uint64 `json:"target_parent_id"`
}
