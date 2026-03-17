package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/pkg/database"
)

// SchedulerJobRepository 管理 scheduler_jobs 与 scheduler_job_items 表。
type SchedulerJobRepository interface {
	CreateJob(ctx context.Context, job *model.SchedulerJob) error                                         // 新建 Job
	UpdateJobStatus(ctx context.Context, id int64, status model.SchedulerJobStatus, errMsg *string) error // 更新 Job 状态
	CreateJobItems(ctx context.Context, items []*model.SchedulerJobItem) error                            // 批量添加 Job 明细
	FetchPendingItems(ctx context.Context, jobType string, limit int) ([]*model.SchedulerJobItem, error)  // 获取待处理明细
	UpdateJobItem(ctx context.Context, item *model.SchedulerJobItem) error                                // 更新明细状态
}

type schedulerJobRepository struct {
	db *sql.DB
}

// NewSchedulerJobRepository 创建仓储。
func NewSchedulerJobRepository(db *database.DB) SchedulerJobRepository {
	return &schedulerJobRepository{db: db.DB.DB}
}

// CreateJob 插入一条 Job。
func (r *schedulerJobRepository) CreateJob(ctx context.Context, job *model.SchedulerJob) error {
	query := `
		INSERT INTO scheduler_jobs (job_type, status, started_at, finished_at, attempts, error_message, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`
	now := time.Now().Unix()
	var id int64
	err := database.QueryRowContext(ctx, r.db, query,
		job.JobType,
		job.Status,
		job.StartedAt,
		job.FinishedAt,
		job.Attempts,
		job.ErrorMsg,
		now,
		now,
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("创建 scheduler_job 失败: %w", err)
	}
	job.ID = id
	return nil
}

// UpdateJobStatus 修改 Job 状态及错误信息。
func (r *schedulerJobRepository) UpdateJobStatus(ctx context.Context, id int64, status model.SchedulerJobStatus, errMsg *string) error {
	query := `
		UPDATE scheduler_jobs
		SET status = ?, error_message = ?, finished_at = ?, updated_at = ?
		WHERE id = ?
	`
	now := time.Now().Unix()
	if _, err := database.ExecContext(ctx, r.db, query, status, errMsg, now, now, id); err != nil {
		return fmt.Errorf("更新 scheduler_job 状态失败: %w", err)
	}
	return nil
}

// CreateJobItems 批量插入 Job 明细。
func (r *schedulerJobRepository) CreateJobItems(ctx context.Context, items []*model.SchedulerJobItem) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	for _, item := range items {
		now := time.Now().Unix()
		query := `
			INSERT INTO scheduler_job_items (job_id, item_key, status, retry_count, started_at, finished_at, error_message, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			RETURNING id
		`
		var itemID int64
		rebindQuery := sqlx.Rebind(sqlx.DOLLAR, query)
		err := tx.QueryRowContext(ctx, rebindQuery,
			item.JobID,
			item.ItemKey,
			item.Status,
			item.RetryCount,
			item.StartedAt,
			item.FinishedAt,
			item.ErrorMsg,
			now,
			now,
		).Scan(&itemID)
		if err != nil {
			return fmt.Errorf("插入 job item 失败: %w", err)
		}
		item.ID = itemID
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交 job items 事务失败: %w", err)
	}
	return nil
}

// FetchPendingItems 查询某类 Job 的待处理明细。
func (r *schedulerJobRepository) FetchPendingItems(ctx context.Context, jobType string, limit int) ([]*model.SchedulerJobItem, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT i.id, i.job_id, i.item_key, i.status, i.retry_count, i.started_at, i.finished_at, i.error_message,
			i.created_at, i.updated_at
		FROM scheduler_job_items i
		JOIN scheduler_jobs j ON i.job_id = j.id
		WHERE j.job_type = ? AND i.status IN ('PENDING','FAILED')
		ORDER BY i.created_at ASC
		LIMIT ?
	`

	rows, err := r.db.QueryContext(ctx, sqlx.Rebind(sqlx.DOLLAR, query), jobType, limit)
	if err != nil {
		return nil, fmt.Errorf("查询 job items 失败: %w", err)
	}
	defer rows.Close()

	var results []*model.SchedulerJobItem
	for rows.Next() {
		var item model.SchedulerJobItem
		var started sql.NullInt64
		var finished sql.NullInt64
		var errMsg sql.NullString
		if err := rows.Scan(
			&item.ID,
			&item.JobID,
			&item.ItemKey,
			&item.Status,
			&item.RetryCount,
			&started,
			&finished,
			&errMsg,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("扫描 job item 失败: %w", err)
		}
		if started.Valid {
			value := started.Int64
			item.StartedAt = &value
		}
		if finished.Valid {
			value := finished.Int64
			item.FinishedAt = &value
		}
		if errMsg.Valid {
			value := errMsg.String
			item.ErrorMsg = &value
		}
		results = append(results, &item)
	}

	return results, nil
}

// UpdateJobItem 更新单条明细。
func (r *schedulerJobRepository) UpdateJobItem(ctx context.Context, item *model.SchedulerJobItem) error {
	query := `
		UPDATE scheduler_job_items
		SET status = ?, retry_count = ?, started_at = ?, finished_at = ?, error_message = ?, updated_at = ?
		WHERE id = ?
	`
	if _, err := r.db.ExecContext(ctx, sqlx.Rebind(sqlx.DOLLAR, query),
		item.Status,
		item.RetryCount,
		item.StartedAt,
		item.FinishedAt,
		item.ErrorMsg,
		time.Now().Unix(),
		item.ID,
	); err != nil {
		return fmt.Errorf("更新 job item 失败: %w", err)
	}
	return nil
}
