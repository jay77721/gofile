// Package port contains contracts owned by the application core. Concrete
// adapters in infrastructure implement these interfaces; application code
// depends on these contracts instead of provider packages.
package port

import (
	"context"
	model "gofile/internal/domain"
	"io"
	"time"
)

type FileRepository interface {
	Create(context.Context, model.File) error
	CreateUserFile(context.Context, model.UserFile) error
	GetByHash(context.Context, string, string) (model.FileMeta, error)
	ListByUser(context.Context, string) ([]model.FileMeta, error)
	CountByUser(context.Context, string) (int64, error)
	ListTrash(context.Context, string, int, int) ([]model.FileMeta, int64, error)
	Restore(context.Context, string, string) (bool, error)
	PurgeUserFile(context.Context, string, string) (bool, error)
	ListByUserPaged(context.Context, string, int, int) ([]model.FileMeta, error)
	Delete(context.Context, string, string) (bool, error)
	UpdateName(context.Context, string, string, string) (bool, error)
	CountRefs(context.Context, string) (int64, error)
	ListOldest(context.Context, time.Time) ([]model.File, error)
	RemoveOrphan(context.Context, string) error
	SaveAnalysis(context.Context, string, string, string) error
	GetGlobalFile(context.Context, string) (model.File, error)
	GetUserFileByID(context.Context, uint, string) (model.UserFile, error)
	ListByParent(context.Context, string, uint64, int, int) ([]model.FileMeta, int64, error)
	CreateFolder(context.Context, model.UserFile) (model.UserFile, error)
	MoveItem(context.Context, uint, string, uint64, string) error
	UpdateDirPathPrefix(context.Context, string, string, string) error
	RenameItem(context.Context, uint, string, string, string) error
	SoftDeleteDir(context.Context, string, string) error
	GetBreadcrumbs(context.Context, string, uint64) ([]model.Breadcrumb, error)
}

type MultipartRepository interface {
	Create(context.Context, model.MultipartUpload) error
	GetByUploadID(context.Context, string, string) (model.MultipartUpload, error)
	UpdateStatus(context.Context, string, string, int) error
	ListExpired(context.Context, time.Time) ([]model.MultipartUpload, error)
	Delete(context.Context, string) error
}

type ShareRepository interface {
	CreateShare(context.Context, *model.Share) error
	GetShareByToken(context.Context, string) (*model.Share, error)
	ListShares(context.Context, string) ([]model.Share, error)
	DeleteShare(context.Context, string, string) (bool, error)
	DeleteExpired(context.Context, time.Time) error
}

type UserRepository interface {
	Create(context.Context, string, string) (bool, error)
	GetPasswordHash(context.Context, string) (string, error)
	GetInfo(context.Context, string) (model.User, error)
}

type TokenRepository interface {
	Upsert(context.Context, string, string, time.Time) (bool, error)
	Get(context.Context, string) (model.Token, error)
	Delete(context.Context, string) error
}

type AITaskRepository interface {
	CreateTask(context.Context, *model.AITask) error
	GetTask(context.Context, string, string) (*model.AITask, error)
	MarkProcessing(context.Context, string, string) error
	MarkDone(context.Context, string, string) error
	MarkFailed(context.Context, string, string, string) error
	ListRequeueable(context.Context, int) ([]model.AITask, error)
	CleanupExpired(context.Context, time.Time) error
}

type AIConfigRepository interface {
	Get(string) (*model.AIConfig, error)
	Upsert(*model.AIConfig) error
	Delete(string) error
}

type CompletePart = model.MultipartCompletePart

type Storage interface {
	Put(context.Context, string, io.Reader, int64) error
	Get(context.Context, string) (io.ReadCloser, error)
	GetRange(context.Context, string, int64, int64) (io.ReadCloser, error)
	FileSize(context.Context, string) (int64, error)
	Exists(context.Context, string) (bool, error)
	Delete(context.Context, string) error
	PresignPut(context.Context, string, time.Duration) (string, error)
	PresignGet(context.Context, string, time.Duration) (string, error)
	InitMultipart(context.Context, string) (string, error)
	PresignPartPut(context.Context, string, string, int, time.Duration) (string, error)
	CompleteMultipart(context.Context, string, string, []CompletePart) error
	AbortMultipart(context.Context, string, string) error
}

type Cache interface {
	Ping(context.Context) error
	Close() error
	HashExists(context.Context, string) (bool, error)
	MarkHash(context.Context, string) error
	AcquireLock(context.Context, string, string, time.Duration) (bool, error)
	ReleaseLock(context.Context, string, string) error
}

type Analysis struct {
	Summary string   `json:"summary"`
	Tags    []string `json:"tags"`
}

type Doc struct {
	ID         string
	Username   string
	Filehash   string
	Filename   string
	Summary    string
	Tags       []string
	Size       int64
	CreatedAt  int64
	ContentVec []float32
	Score      float64
}

type Provider interface {
	Analyze(context.Context, string, string) (*Analysis, error)
	Embed(context.Context, string) ([]float32, error)
	Dimension() int
}

type Indexer interface {
	EnsureCollection(context.Context) error
	Upsert(context.Context, *Doc) error
	Delete(context.Context, string, string) error
	SearchHybrid(context.Context, string, string, []float32, string, int, int) ([]Doc, error)
	Similar(context.Context, string, []float32, string, int) ([]Doc, error)
	DeleteByFilehash(context.Context, string) error
}

type TaskEnqueuer interface {
	Enqueue(context.Context, string, string, string) error
}
