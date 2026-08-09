package repository

import (
	"fmt"
	"gofile/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AITaskRepository AI 异步分析任务的数据访问接口
type AITaskRepository interface {
	// CreateTask 创建任务（幂等：同一 file_sha1+username 已存在则忽略）
	CreateTask(task *model.AITask) error
	// GetTask 按 hash+username 获取任务
	GetTask(filehash, username string) (*model.AITask, error)
	// MarkProcessing 将任务置为处理中
	MarkProcessing(filehash, username string) error
	// MarkDone 将任务置为完成
	MarkDone(filehash, username string) error
	// MarkFailed 将任务置为失败，记录错误信息，retry_count+1
	MarkFailed(filehash, username, errMsg string) error
	// ListRequeueable 列出可重新入队的失败任务（retry_count < max）
	ListRequeueable(maxRetry int) ([]model.AITask, error)
}

// mysqlAITaskRepo GORM 实现的 AITaskRepository
type mysqlAITaskRepo struct {
	db *gorm.DB
}

// NewAITaskRepository 创建 GORM AI 任务仓库
func NewAITaskRepository(db *gorm.DB) AITaskRepository {
	return &mysqlAITaskRepo{db: db}
}

func (r *mysqlAITaskRepo) CreateTask(task *model.AITask) error {
	// 已存在则忽略（幂等）
	if err := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(task).Error; err != nil {
		return fmt.Errorf("create ai task failed: %w", err)
	}
	return nil
}

func (r *mysqlAITaskRepo) GetTask(filehash, username string) (*model.AITask, error) {
	var t model.AITask
	if err := r.db.Where("file_sha1 = ? AND user_name = ?", filehash, username).First(&t).Error; err != nil {
		return nil, fmt.Errorf("get ai task failed: %w", err)
	}
	return &t, nil
}

func (r *mysqlAITaskRepo) MarkProcessing(filehash, username string) error {
	res := r.db.Model(&model.AITask{}).
		Where("file_sha1 = ? AND user_name = ?", filehash, username).
		Update("status", 1)
	if res.Error != nil {
		return fmt.Errorf("mark ai task processing failed: %w", res.Error)
	}
	return nil
}

func (r *mysqlAITaskRepo) MarkDone(filehash, username string) error {
	res := r.db.Model(&model.AITask{}).
		Where("file_sha1 = ? AND user_name = ?", filehash, username).
		Update("status", 2)
	if res.Error != nil {
		return fmt.Errorf("mark ai task done failed: %w", res.Error)
	}
	return nil
}

func (r *mysqlAITaskRepo) MarkFailed(filehash, username, errMsg string) error {
	res := r.db.Model(&model.AITask{}).
		Where("file_sha1 = ? AND user_name = ?", filehash, username).
		Updates(map[string]any{"status": 3, "error_msg": errMsg, "retry_count": gorm.Expr("retry_count + 1")})
	if res.Error != nil {
		return fmt.Errorf("mark ai task failed failed: %w", res.Error)
	}
	return nil
}

func (r *mysqlAITaskRepo) ListRequeueable(maxRetry int) ([]model.AITask, error) {
	var tasks []model.AITask
	if err := r.db.Where("status = 3 AND retry_count < ?", maxRetry).
		Order("create_at ASC").
		Limit(100).
		Find(&tasks).Error; err != nil {
		return nil, fmt.Errorf("list requeueable ai tasks failed: %w", err)
	}
	return tasks, nil
}

// 确保编译时检查接口实现
var _ AITaskRepository = (*mysqlAITaskRepo)(nil)
