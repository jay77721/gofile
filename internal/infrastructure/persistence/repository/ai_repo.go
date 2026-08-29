package repository

import (
	"context"
	"fmt"
	"gofile/internal/domain"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AITaskRepository is the data access interface for AI async analysis tasks
type AITaskRepository interface {
	// CreateTask creates a task (idempotent: ignore if same file_sha1+username already exists)
	CreateTask(ctx context.Context, task *model.AITask) error
	// GetTask retrieves a task by hash+username
	GetTask(ctx context.Context, filehash, username string) (*model.AITask, error)
	// MarkProcessing marks the task as processing
	MarkProcessing(ctx context.Context, filehash, username string) error
	// MarkDone marks the task as completed
	MarkDone(ctx context.Context, filehash, username string) error
	// MarkFailed marks the task as failed, records error message, and increments retry_count by 1
	MarkFailed(ctx context.Context, filehash, username, errMsg string) error
	// ListRequeueable lists failed tasks that can be re-enqueued (retry_count < max)
	ListRequeueable(ctx context.Context, maxRetry int) ([]model.AITask, error)
	// CleanupExpired deletes expired done/failed tasks
	CleanupExpired(ctx context.Context, before time.Time) error
}

// mysqlAITaskRepo is the GORM implementation of AITaskRepository
type mysqlAITaskRepo struct {
	db *gorm.DB
}

// NewAITaskRepository creates a GORM AI task repository
func NewAITaskRepository(db *gorm.DB) AITaskRepository {
	return &mysqlAITaskRepo{db: db}
}

func (r *mysqlAITaskRepo) CreateTask(ctx context.Context, task *model.AITask) error {
	// Ignore if already exists (idempotent)
	// Explicitly set expiration time: zero value time.Time would be explicitly INSERTed by GORM, bypassing the schema DEFAULT,
	// and in MySQL strict mode would fail directly due to out-of-range datetime, causing tasks not to be persisted and breaking the idempotency anchor
	if task.ExpiredAt.IsZero() {
		task.ExpiredAt = time.Now().Add(7 * 24 * time.Hour)
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(task).Error; err != nil {
		return fmt.Errorf("create ai task failed: %w", err)
	}
	return nil
}

func (r *mysqlAITaskRepo) GetTask(ctx context.Context, filehash, username string) (*model.AITask, error) {
	var t model.AITask
	if err := r.db.WithContext(ctx).Where("file_sha1 = ? AND user_name = ?", filehash, username).First(&t).Error; err != nil {
		return nil, fmt.Errorf("get ai task failed: %w", err)
	}
	return &t, nil
}

func (r *mysqlAITaskRepo) MarkProcessing(ctx context.Context, filehash, username string) error {
	res := r.db.WithContext(ctx).Model(&model.AITask{}).
		Where("file_sha1 = ? AND user_name = ?", filehash, username).
		Update("status", 1)
	if res.Error != nil {
		return fmt.Errorf("mark ai task processing failed: %w", res.Error)
	}
	return nil
}

func (r *mysqlAITaskRepo) MarkDone(ctx context.Context, filehash, username string) error {
	res := r.db.WithContext(ctx).Model(&model.AITask{}).
		Where("file_sha1 = ? AND user_name = ?", filehash, username).
		Update("status", 2)
	if res.Error != nil {
		return fmt.Errorf("mark ai task done failed: %w", res.Error)
	}
	return nil
}

func (r *mysqlAITaskRepo) MarkFailed(ctx context.Context, filehash, username, errMsg string) error {
	res := r.db.WithContext(ctx).Model(&model.AITask{}).
		Where("file_sha1 = ? AND user_name = ?", filehash, username).
		Updates(map[string]any{"status": 3, "error_msg": errMsg, "retry_count": gorm.Expr("retry_count + 1")})
	if res.Error != nil {
		return fmt.Errorf("mark ai task failed failed: %w", res.Error)
	}
	return nil
}

func (r *mysqlAITaskRepo) ListRequeueable(ctx context.Context, maxRetry int) ([]model.AITask, error) {
	var tasks []model.AITask
	if err := r.db.WithContext(ctx).Where("status = 3 AND retry_count < ?", maxRetry).
		Order("create_at ASC").
		Limit(100).
		Find(&tasks).Error; err != nil {
		return nil, fmt.Errorf("list requeueable ai tasks failed: %w", err)
	}
	return tasks, nil
}

// CleanupExpired deletes expired tasks (done/failed and expired_at < before)
func (r *mysqlAITaskRepo) CleanupExpired(ctx context.Context, before time.Time) error {
	if err := r.db.WithContext(ctx).Where("status IN (2, 3) AND expired_at < ?", before).Delete(&model.AITask{}).Error; err != nil {
		return fmt.Errorf("cleanup expired ai tasks failed: %w", err)
	}
	return nil
}

// Ensure interface implementation is checked at compile time
var _ AITaskRepository = (*mysqlAITaskRepo)(nil)
