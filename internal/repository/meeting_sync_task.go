// Package repository 提供数据访问层实现
package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/chinaxxren/gonotic/internal/pkg/database"
	"github.com/chinaxxren/gonotic/internal/pkg/errors"
	"go.uber.org/zap"
)

// MeetingSyncTaskStatus 任务状态
const (
	MeetingSyncStatusPending         = "pending"
	MeetingSyncStatusProcessing      = "processing"
	MeetingSyncStatusSuccess         = "success"
	MeetingSyncStatusFailed          = "failed"
	MeetingSyncStatusPermanentFailed = "permanent_failed"
)

// MeetingSyncTask 步进同步任务
// 对应表 meeting_sync_tasks
//
// 字段含义：
//   - status: pending/processing/success/failed/permanent_failed
//   - current_step: 当前执行到的步骤，失败后从该步继续
//   - attempts: 总尝试次数
//   - next_retry_at: 下次允许重试的时间戳（秒），NULL 表示随时可重试
//   - last_error: 最近一次错误信息
//
// 注意：此结构只在仓储内部使用，未暴露到 service/model 层。
type MeetingSyncTask struct {
	ID          int            `db:"id"`
	MeetingID   int            `db:"meeting_id"`
	Status      string         `db:"status"`
	CurrentStep string         `db:"current_step"`
	Attempts    int            `db:"attempts"`
	LastError   sql.NullString `db:"last_error"`
	NextRetryAt sql.NullInt64  `db:"next_retry_at"`
	CreatedAt   int64          `db:"created_at"`
	UpdatedAt   int64          `db:"updated_at"`
}

// MeetingSyncTaskRepository 定义任务表操作接口
type MeetingSyncTaskRepository interface {
	// CreateOrGetPending 确保指定 meeting_id 存在一个任务记录
	// 如果已存在则返回该记录；否则创建 pending 任务并返回
	CreateOrGetPending(ctx context.Context, meetingID int, initialStep string) (*MeetingSyncTask, error)

	// GetByMeetingID 获取某个会议对应的任务（如果存在）
	GetByMeetingID(ctx context.Context, meetingID int) (*MeetingSyncTask, error)

	// FetchReadyTasks 查询可以被调度执行的任务列表
	// 仅返回 status = pending/failed 且 (next_retry_at 为空或 <= now) 的记录
	FetchReadyTasks(ctx context.Context, limit int) ([]*MeetingSyncTask, error)

	// MarkProcessing 尝试将任务状态从 pending/failed 切换为 processing
	// 使用乐观锁语义：只有当当前 status 在 allowedStatuses 中时才更新成功
	// 返回值 ok 表示是否成功取得执行权
	MarkProcessing(ctx context.Context, id int, allowedStatuses []string) (ok bool, err error)

	// UpdateOnStepSuccess 在单步执行成功后更新 current_step/status/attempts
	//   - 如果 nextStep 为空字符串，表示任务整体成功 -> status=success
	//   - 否则仅更新 current_step=nextStep，保持 status 不变（通常保持 processing 或 pending）
	UpdateOnStepSuccess(ctx context.Context, id int, nextStep string) error

	// UpdateOnError 在执行中出现错误时更新状态和重试时间
	// 内部维护 attempts 计数，并根据 attempts 设置 failed 或 permanent_failed
	UpdateOnError(ctx context.Context, id int, currentStep string, attemptIncrement int, errMsg string) error
}

// meetingSyncTaskRepository 实现 MeetingSyncTaskRepository 接口
type meetingSyncTaskRepository struct {
	db     *database.DB
	logger *zap.Logger
}

// NewMeetingSyncTaskRepository 创建任务仓储实例
func NewMeetingSyncTaskRepository(db *database.DB, logger *zap.Logger) MeetingSyncTaskRepository {
	return &meetingSyncTaskRepository{
		db:     db,
		logger: logger,
	}
}

// CreateOrGetPending 确保存在一个任务记录
func (r *meetingSyncTaskRepository) CreateOrGetPending(ctx context.Context, meetingID int, initialStep string) (*MeetingSyncTask, error) {
	now := time.Now().Unix()

	// 先尝试查询是否已存在
	var existing MeetingSyncTask
	query := `SELECT id, meeting_id, status, current_step, attempts, last_error, next_retry_at, created_at, updated_at
		FROM meeting_sync_tasks WHERE meeting_id = ? LIMIT 1`
	err := r.db.GetContext(ctx, &existing, r.db.Rebind(query), meetingID)
	if err == nil {
		return &existing, nil
	} else if err != sql.ErrNoRows {
		r.logger.Error("查询 meeting_sync_tasks 失败",
			zap.Error(err),
			zap.Int("meeting_id", meetingID))
		return nil, errors.NewDatabaseError("查询同步任务失败", err)
	}

	// 不存在则创建
	insert := `INSERT INTO meeting_sync_tasks (meeting_id, status, current_step, attempts, created_at, updated_at)
		VALUES (?, ?, ?, 0, ?, ?) RETURNING id`
	var id int
	err = r.db.GetContext(ctx, &id, r.db.Rebind(insert), meetingID, MeetingSyncStatusPending, initialStep, now, now)
	if err != nil {
		r.logger.Error("创建 meeting_sync_tasks 失败",
			zap.Error(err),
			zap.Int("meeting_id", meetingID))
		return nil, errors.NewDatabaseError("创建同步任务失败", err)
	}

	return &MeetingSyncTask{
		ID:          id,
		MeetingID:   meetingID,
		Status:      MeetingSyncStatusPending,
		CurrentStep: initialStep,
		Attempts:    0,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// GetByMeetingID 获取某个会议的任务
func (r *meetingSyncTaskRepository) GetByMeetingID(ctx context.Context, meetingID int) (*MeetingSyncTask, error) {
	var task MeetingSyncTask
	query := `SELECT id, meeting_id, status, current_step, attempts, last_error, next_retry_at, created_at, updated_at
		FROM meeting_sync_tasks WHERE meeting_id = ? LIMIT 1`
	err := r.db.GetContext(ctx, &task, r.db.Rebind(query), meetingID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		r.logger.Error("根据 meeting_id 获取同步任务失败",
			zap.Error(err),
			zap.Int("meeting_id", meetingID))
		return nil, errors.NewDatabaseError("获取同步任务失败", err)
	}
	return &task, nil
}

// FetchReadyTasks 查询可调度任务
func (r *meetingSyncTaskRepository) FetchReadyTasks(ctx context.Context, limit int) ([]*MeetingSyncTask, error) {
	if limit <= 0 {
		limit = 50
	}

	now := time.Now().Unix()
	query := `SELECT id, meeting_id, status, current_step, attempts, last_error, next_retry_at, created_at, updated_at
		FROM meeting_sync_tasks
		WHERE (status = ? OR status = ?)
		  AND (next_retry_at IS NULL OR next_retry_at <= ?)
		ORDER BY updated_at ASC
		LIMIT ?`

	var tasks []*MeetingSyncTask
	if err := r.db.SelectContext(ctx, &tasks, r.db.Rebind(query), MeetingSyncStatusPending, MeetingSyncStatusFailed, now, limit); err != nil {
		r.logger.Error("查询可执行的同步任务失败",
			zap.Error(err))
		return nil, errors.NewDatabaseError("查询可执行的同步任务失败", err)
	}

	return tasks, nil
}

// MarkProcessing 尝试抢占任务执行权
func (r *meetingSyncTaskRepository) MarkProcessing(ctx context.Context, id int, allowedStatuses []string) (bool, error) {
	if len(allowedStatuses) == 0 {
		allowedStatuses = []string{MeetingSyncStatusPending, MeetingSyncStatusFailed}
	}

	now := time.Now().Unix()

	// 构建 IN 子句
	placeholders := ""
	statusArgs := make([]interface{}, 0, len(allowedStatuses))
	for i, st := range allowedStatuses {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		statusArgs = append(statusArgs, st)
	}

	query := "UPDATE meeting_sync_tasks SET status = ?, updated_at = ? WHERE id = ? AND status IN (" + placeholders + ")"
	args := []interface{}{MeetingSyncStatusProcessing, now, id}
	args = append(args, statusArgs...)

	res, err := r.db.ExecContext(ctx, r.db.Rebind(query), args...)
	if err != nil {
		r.logger.Error("更新同步任务为 processing 失败",
			zap.Error(err),
			zap.Int("task_id", id))
		return false, errors.NewDatabaseError("更新同步任务状态失败", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return false, errors.NewDatabaseError("获取更新行数失败", err)
	}

	return affected > 0, nil
}

// UpdateOnStepSuccess 在单步成功后更新任务
func (r *meetingSyncTaskRepository) UpdateOnStepSuccess(ctx context.Context, id int, nextStep string) error {
	now := time.Now().Unix()

	// 无下一步：任务结束
	if nextStep == "" {
		query := `UPDATE meeting_sync_tasks
			SET status = ?, current_step = '', next_retry_at = NULL, updated_at = ?
			WHERE id = ?`
		_, err := r.db.ExecContext(ctx, r.db.Rebind(query), MeetingSyncStatusSuccess, now, id)
		if err != nil {
			r.logger.Error("标记同步任务成功失败",
				zap.Error(err),
				zap.Int("task_id", id))
			return errors.NewDatabaseError("标记同步任务成功失败", err)
		}
		return nil
	}

	// 仍有后续步骤：只更新 current_step
	query := `UPDATE meeting_sync_tasks
		SET current_step = ?, updated_at = ?
		WHERE id = ?`
	_, err := r.db.ExecContext(ctx, r.db.Rebind(query), nextStep, now, id)
	if err != nil {
		r.logger.Error("更新同步任务步骤失败",
			zap.Error(err),
			zap.Int("task_id", id),
			zap.String("next_step", nextStep))
		return errors.NewDatabaseError("更新同步任务步骤失败", err)
	}
	return nil
}

// UpdateOnError 在执行步骤出错时更新任务
func (r *meetingSyncTaskRepository) UpdateOnError(ctx context.Context, id int, currentStep string, attemptIncrement int, errMsg string) error {
	if attemptIncrement <= 0 {
		attemptIncrement = 1
	}

	now := time.Now().Unix()

	// 先查出当前 attempts
	var attempts int
	if err := r.db.GetContext(ctx, &attempts, r.db.Rebind(`SELECT attempts FROM meeting_sync_tasks WHERE id = ?`), id); err != nil {
		r.logger.Error("查询同步任务 attempts 失败",
			zap.Error(err),
			zap.Int("task_id", id))
		return errors.NewDatabaseError("查询同步任务 attempts 失败", err)
	}

	attempts += attemptIncrement

	// 简单的退避策略：
	//   第 1–3 次失败：5 分钟后重试
	//   第 4–5 次失败：30 分钟后重试
	//   >5 次：标记为 permanent_failed，不再自动重试
	var status string
	var nextRetry sql.NullInt64

	switch {
	case attempts <= 3:
		status = MeetingSyncStatusFailed
		nextRetry = sql.NullInt64{Int64: now + 300, Valid: true}
	case attempts <= 5:
		status = MeetingSyncStatusFailed
		nextRetry = sql.NullInt64{Int64: now + 1800, Valid: true}
	default:
		status = MeetingSyncStatusPermanentFailed
		nextRetry = sql.NullInt64{Int64: 0, Valid: false}
	}

	query := `UPDATE meeting_sync_tasks
		SET status = ?, current_step = ?, attempts = ?, last_error = ?, next_retry_at = ?, updated_at = ?
		WHERE id = ?`

	_, err := r.db.ExecContext(ctx, r.db.Rebind(query), status, currentStep, attempts, errMsg, nextRetry, now, id)
	if err != nil {
		r.logger.Error("更新同步任务错误状态失败",
			zap.Error(err),
			zap.Int("task_id", id),
			zap.Int("attempts", attempts))
		return errors.NewDatabaseError("更新同步任务错误状态失败", err)
	}

	return nil
}
