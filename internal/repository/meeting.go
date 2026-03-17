// Package repository 提供数据访问层实现
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/pkg/database"
	"github.com/chinaxxren/gonotic/internal/pkg/errors"
	"go.uber.org/zap"
)

// MeetingRepository 定义会议数据访问操作的接口
// 所有方法使用预处理语句防止 SQL 注入
type MeetingRepository interface {
	// Create 在数据库中创建新会议
	Create(ctx context.Context, meeting *model.Meeting) error

	// GetByID 根据 ID 获取会议
	GetByID(ctx context.Context, id int) (*model.Meeting, error)

	// GetByUUID 根据 UUID 获取会议
	GetByUUID(ctx context.Context, uuid string) (*model.Meeting, error)

	// List 获取用户的会议列表（支持分页）
	List(ctx context.Context, userID int, offset, limit int) ([]*model.Meeting, error)

	// Count 获取用户的会议总数
	Count(ctx context.Context, userID int) (int, error)

	// Update 更新会议字段
	Update(ctx context.Context, id int, updates map[string]interface{}) error

	// Delete 删除会议
	Delete(ctx context.Context, id int) error

	// UpdateStatus 更新会议状态
	UpdateStatus(ctx context.Context, id int, status model.MeetingStatus) error

	// UpdateDuration 更新会议时长
	UpdateDuration(ctx context.Context, id int, duration int) error

	// GetMeetingWithConsumption 获取会议及其计费信息
	GetMeetingWithConsumption(ctx context.Context, meetingID int) (*model.MeetingWithConsumption, error)

	// ListMeetingsWithConsumption 获取用户会议列表及计费信息
	ListMeetingsWithConsumption(ctx context.Context, userID int, offset, limit int) ([]*model.MeetingWithConsumption, error)
}

// meetingRepository 实现 MeetingRepository 接口
type meetingRepository struct {
	db     *database.DB
	logger *zap.Logger
}

// NewMeetingRepository 创建新的会议仓储实例
//
// 参数:
//   - db: 数据库连接
//   - logger: 日志记录器实例
//
// 返回:
//   - MeetingRepository: 会议仓储实现
func NewMeetingRepository(db *database.DB, logger *zap.Logger) MeetingRepository {
	return &meetingRepository{
		db:     db,
		logger: logger,
	}
}

// Create 在数据库中创建新会议
// 会议 ID 会自动生成并设置到会议对象上
//
// 参数:
//   - ctx: 用于超时和取消的上下文
//   - meeting: 要创建的会议
//
// 返回:
//   - error: 如果创建失败返回错误
func (r *meetingRepository) Create(ctx context.Context, meeting *model.Meeting) error {
	// 验证 session_uuid 不能为空
	sessionUUID := meeting.SessionUUID
	if sessionUUID == "" {
		return fmt.Errorf("meeting.SessionUUID cannot be empty, must be set before creating meeting record")
	}

	query := `
		INSERT INTO meetings (
			user_id,
			title,
			session_uuid,
			start_time,
			end_time,
			duration_seconds,
			file_path,
			status,
			created_at,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`

	var id int64
	err := r.db.QueryRowContext(ctx, r.db.Rebind(query),
		meeting.UserID,
		meeting.Title,
		sessionUUID,
		meeting.StartTime,
		meeting.EndTime,
		meeting.Duration,
		meeting.FilePath,
		meeting.Status,
		meeting.CreatedAt,
		meeting.UpdatedAt,
	).Scan(&id)

	if err != nil {
		r.logger.Error("创建会议失败",
			zap.Error(err),
			zap.Int("user_id", meeting.UserID),
			zap.String("title", meeting.Title))
		return errors.NewDatabaseError("创建会议失败", err)
	}

	meeting.ID = int(id)
	meeting.SessionUUID = sessionUUID

	r.logger.Info("会议创建成功",
		zap.Int("meeting_id", meeting.ID),
		zap.Int("user_id", meeting.UserID),
		zap.String("title", meeting.Title))

	return nil
}

// GetByID 根据 ID 获取会议
//
// 参数:
//   - ctx: 用于超时和取消的上下文
//   - id: 会议 ID
//
// 返回:
//   - *model.Meeting: 如果找到返回会议
//   - error: 如果查询失败或未找到会议返回错误
func (r *meetingRepository) GetByID(ctx context.Context, id int) (*model.Meeting, error) {
	query := `
		SELECT
			id,
			user_id,
			title,
			session_uuid,
			COALESCE(start_time, 0) AS start_time,
			COALESCE(end_time, 0) AS end_time,
			COALESCE(duration_seconds, 0) AS duration_seconds,
			0 AS total_words,
			0 AS tokens,
			COALESCE(file_path, '') AS file_path,
			COALESCE(status, '') AS status,
			created_at,
			updated_at,
			COALESCE(deleted_at, 0) AS deleted_at
		FROM meetings
		WHERE id = ? AND deleted_at IS NULL
	`

	var meeting model.Meeting
	err := r.db.GetContext(ctx, &meeting, r.db.Rebind(query), id)

	if err == sql.ErrNoRows {
		return nil, errors.New(errors.ErrMeetingNotFound, "会议不存在")
	}

	if err != nil {
		r.logger.Error("根据 ID 获取会议失败",
			zap.Error(err),
			zap.Int("meeting_id", id))
		return nil, errors.NewDatabaseError("获取会议失败", err)
	}

	return &meeting, nil
}

// GetByUUID 根据 UUID 获取会议
//
// 参数:
//   - ctx: 用于超时和取消的上下文
//   - uuid: 会议 UUID
//
// 返回:
//   - *model.Meeting: 如果找到返回会议
//   - error: 如果查询失败或未找到会议返回错误
func (r *meetingRepository) GetByUUID(ctx context.Context, uuid string) (*model.Meeting, error) {
	query := `
		SELECT
			id,
			user_id,
			title,
			session_uuid,
			COALESCE(start_time, 0) AS start_time,
			COALESCE(end_time, 0) AS end_time,
			COALESCE(duration_seconds, 0) AS duration_seconds,
			0 AS total_words,
			0 AS tokens,
			COALESCE(file_path, '') AS file_path,
			COALESCE(status, '') AS status,
			created_at,
			updated_at,
			COALESCE(deleted_at, 0) AS deleted_at
		FROM meetings
		WHERE session_uuid = ? AND deleted_at IS NULL
	`

	var meeting model.Meeting
	err := r.db.GetContext(ctx, &meeting, r.db.Rebind(query), uuid)

	if err == sql.ErrNoRows {
		return nil, errors.New(errors.ErrMeetingNotFound, "会议不存在")
	}

	if err != nil {
		r.logger.Error("根据 UUID 获取会议失败",
			zap.Error(err),
			zap.String("uuid", uuid))
		return nil, errors.NewDatabaseError("获取会议失败", err)
	}

	return &meeting, nil
}

// List 获取用户的会议列表（支持分页）
// 会议按创建时间降序排列（最新的在前）
//
// 参数:
//   - ctx: 用于超时和取消的上下文
//   - userID: 用户 ID
//   - offset: 分页偏移量
//   - limit: 每页数量
//
// 返回:
//   - []*model.Meeting: 会议列表
//   - error: 如果查询失败返回错误
func (r *meetingRepository) List(ctx context.Context, userID int, offset, limit int) ([]*model.Meeting, error) {
	query := `
		SELECT
			id,
			user_id,
			title,
			session_uuid,
			COALESCE(start_time, 0) AS start_time,
			COALESCE(end_time, 0) AS end_time,
			COALESCE(duration_seconds, 0) AS duration_seconds,
			0 AS total_words,
			0 AS tokens,
			COALESCE(file_path, '') AS file_path,
			COALESCE(status, '') AS status,
			created_at,
			updated_at,
			COALESCE(deleted_at, 0) AS deleted_at
		FROM meetings
		WHERE user_id = ? AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`

	var meetings []*model.Meeting
	err := r.db.SelectContext(ctx, &meetings, r.db.Rebind(query), userID, limit, offset)

	if err != nil {
		r.logger.Error("获取会议列表失败",
			zap.Error(err),
			zap.Int("user_id", userID))
		return nil, errors.NewDatabaseError("获取会议列表失败", err)
	}

	return meetings, nil
}

// Count 获取用户的会议总数
// 用于分页计算
//
// 参数:
//   - ctx: 用于超时和取消的上下文
//   - userID: 用户 ID
//
// 返回:
//   - int: 会议总数
//   - error: 如果查询失败返回错误
func (r *meetingRepository) Count(ctx context.Context, userID int) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM meetings
		WHERE user_id = ? AND deleted_at IS NULL
	`

	var count int
	err := r.db.GetContext(ctx, &count, r.db.Rebind(query), userID)

	if err != nil {
		r.logger.Error("获取会议数量失败",
			zap.Error(err),
			zap.Int("user_id", userID))
		return 0, errors.NewDatabaseError("获取会议数量失败", err)
	}

	return count, nil
}

// Update 更新会议字段
// 支持动态更新指定字段
//
// 参数:
//   - ctx: 用于超时和取消的上下文
//   - id: 会议 ID
//   - updates: 要更新的字段映射（字段名 -> 值）
//
// 返回:
//   - error: 如果更新失败返回错误
//
// 示例:
//
//	updates := map[string]interface{}{
//	    "title": "新标题",
//	    "duration": 3600,
//	}
//	err := repo.Update(ctx, meetingID, updates)
func (r *meetingRepository) Update(ctx context.Context, id int, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}

	// 自动添加 updated_at 字段
	updates["updated_at"] = time.Now().Unix()

	// 构建动态 SQL 查询
	query := "UPDATE meetings SET "
	args := make([]interface{}, 0, len(updates)+1)

	i := 0
	for field, value := range updates {
		if i > 0 {
			query += ", "
		}
		query += field + " = ?"
		args = append(args, value)
		i++
	}

	query += " WHERE id = ?"
	args = append(args, id)

	result, err := r.db.ExecContext(ctx, r.db.Rebind(query), args...)

	if err != nil {
		r.logger.Error("更新会议失败",
			zap.Error(err),
			zap.Int("meeting_id", id))
		return errors.NewDatabaseError("更新会议失败", err)
	}

	// 检查是否找到会议
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		r.logger.Error("获取影响行数失败", zap.Error(err))
		return errors.NewDatabaseError("验证更新失败", err)
	}

	if rowsAffected == 0 {
		var exists int
		checkQuery := r.db.Rebind(`
			SELECT COUNT(1) FROM meetings WHERE id = ? AND deleted_at IS NULL
		`)
		checkErr := r.db.GetContext(ctx, &exists, checkQuery, id)
		if checkErr != nil {
			r.logger.Error("验证会议是否存在失败",
				zap.Error(checkErr),
				zap.Int("meeting_id", id))
			return errors.NewDatabaseError("验证会议是否存在失败", checkErr)
		}
		if exists == 0 {
			return errors.New(errors.ErrMeetingNotFound, "会议不存在")
		}
		// 记录存在但字段值未变化，视为成功
		r.logger.Debug("会议更新未改变字段值",
			zap.Int("meeting_id", id))
		return nil
	}

	r.logger.Info("会议更新成功",
		zap.Int("meeting_id", id))

	return nil
}

// Delete 删除会议
// 注意：这会从数据库中永久删除会议记录
//
// 参数:
//   - ctx: 用于超时和取消的上下文
//   - id: 会议 ID
//
// 返回:
//   - error: 如果删除失败返回错误
func (r *meetingRepository) Delete(ctx context.Context, id int) error {
	// 使用软删除
	query := `UPDATE meetings SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`

	now := time.Now().Unix()
	result, err := r.db.ExecContext(ctx, r.db.Rebind(query), now, id)

	if err != nil {
		r.logger.Error("删除会议失败",
			zap.Error(err),
			zap.Int("meeting_id", id))
		return errors.NewDatabaseError("删除会议失败", err)
	}

	// 检查是否找到会议
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		r.logger.Error("获取影响行数失败", zap.Error(err))
		return errors.NewDatabaseError("验证删除失败", err)
	}

	if rowsAffected == 0 {
		return errors.New(errors.ErrMeetingNotFound, "会议不存在")
	}

	r.logger.Info("会议删除成功",
		zap.Int("meeting_id", id))

	return nil
}

// UpdateStatus 更新会议状态
//
// 参数:
//   - ctx: 用于超时和取消的上下文
//   - id: 会议 ID
//   - status: 新状态
//
// 返回:
//   - error: 如果更新失败返回错误
func (r *meetingRepository) UpdateStatus(ctx context.Context, id int, status model.MeetingStatus) error {
	query := `
		UPDATE meetings
		SET status = ?, updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`

	now := time.Now().Unix()

	result, err := r.db.ExecContext(ctx, r.db.Rebind(query), status.String(), now, id)

	if err != nil {
		r.logger.Error("更新会议状态失败",
			zap.Error(err),
			zap.Int("meeting_id", id),
			zap.String("status", status.String()))
		return errors.NewDatabaseError("更新会议状态失败", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errors.NewDatabaseError("验证更新失败", err)
	}

	if rowsAffected == 0 {
		return errors.New(errors.ErrMeetingNotFound, "会议不存在")
	}

	r.logger.Info("会议状态更新成功",
		zap.Int("meeting_id", id),
		zap.String("status", status.String()))

	return nil
}

// UpdateDuration 更新会议时长
//
// 参数:
//   - ctx: 用于超时和取消的上下文
//   - id: 会议 ID
//   - duration: 新时长（秒）
//
// 返回:
//   - error: 如果更新失败返回错误
func (r *meetingRepository) UpdateDuration(ctx context.Context, id int, duration int) error {
	query := `
		UPDATE meetings
		SET duration_seconds = ?, updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`

	now := time.Now().Unix()

	result, err := r.db.ExecContext(ctx, r.db.Rebind(query), duration, now, id)

	if err != nil {
		r.logger.Error("更新会议时长失败",
			zap.Error(err),
			zap.Int("meeting_id", id),
			zap.Int("duration", duration))
		return errors.NewDatabaseError("更新会议时长失败", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errors.NewDatabaseError("验证更新失败", err)
	}

	if rowsAffected == 0 {
		return errors.New(errors.ErrMeetingNotFound, "会议不存在")
	}

	r.logger.Info("会议时长更新成功",
		zap.Int("meeting_id", id),
		zap.Int("duration", duration))

	return nil
}

// GetMeetingWithConsumption 获取会议及其计费信息
func (r *meetingRepository) GetMeetingWithConsumption(ctx context.Context, meetingID int) (*model.MeetingWithConsumption, error) {
	query := `
		SELECT 
			m.id, m.user_id, m.title, m.session_uuid, m.start_time, m.end_time,
			m.duration_seconds, 0 AS total_words, 0 AS tokens, m.file_path, m.language,
			m.status, m.created_at, m.updated_at, m.deleted_at,
			COALESCE(SUM(ul.seconds_consumed), 0) as consumed_seconds,
			COALESCE(SUM(ul.transcription_seconds), 0) as transcription_seconds,
			COALESCE(SUM(ul.translation_seconds), 0) as translation_seconds
		FROM meetings m 
		LEFT JOIN usage_ledger ul ON m.id = ul.business_id 
		WHERE m.id = ? AND m.deleted_at = 0
		GROUP BY m.id
	`

	var result model.MeetingWithConsumption
	var meeting model.Meeting

	err := database.QueryRowContext(ctx, r.db.DB.DB, query, meetingID).Scan(
		&meeting.ID, &meeting.UserID, &meeting.Title, &meeting.SessionUUID,
		&meeting.StartTime, &meeting.EndTime, &meeting.Duration, &meeting.TotalWords,
		&meeting.Tokens, &meeting.FilePath, &meeting.Language, &meeting.Status,
		&meeting.CreatedAt, &meeting.UpdatedAt, &meeting.DeletedAt,
		&result.ConsumedSeconds, &result.TranscriptionSeconds, &result.TranslationSeconds,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New(errors.ErrMeetingNotFound, "会议不存在")
		}
		return nil, errors.NewDatabaseError("查询会议计费信息失败", err)
	}

	result.Meeting = &meeting
	return &result, nil
}

// ListMeetingsWithConsumption 获取用户会议列表及计费信息
func (r *meetingRepository) ListMeetingsWithConsumption(ctx context.Context, userID int, offset, limit int) ([]*model.MeetingWithConsumption, error) {
	query := `
		SELECT 
			m.id, m.user_id, m.title, m.session_uuid, m.start_time, m.end_time,
			m.duration_seconds, 0 AS total_words, 0 AS tokens, m.file_path, m.language,
			m.status, m.created_at, m.updated_at, m.deleted_at,
			COALESCE(SUM(ul.seconds_consumed), 0) as consumed_seconds,
			COALESCE(SUM(ul.transcription_seconds), 0) as transcription_seconds,
			COALESCE(SUM(ul.translation_seconds), 0) as translation_seconds
		FROM meetings m 
		LEFT JOIN usage_ledger ul ON m.id = ul.business_id 
		WHERE m.user_id = ? AND m.deleted_at = 0
		GROUP BY m.id
		ORDER BY m.created_at DESC
		LIMIT ? OFFSET ?
	`

	rows, err := database.QueryContext(ctx, r.db.DB.DB, query, userID, limit, offset)
	if err != nil {
		return nil, errors.NewDatabaseError("查询用户会议列表失败", err)
	}
	defer rows.Close()

	var results []*model.MeetingWithConsumption
	for rows.Next() {
		var result model.MeetingWithConsumption
		var meeting model.Meeting

		err := rows.Scan(
			&meeting.ID, &meeting.UserID, &meeting.Title, &meeting.SessionUUID,
			&meeting.StartTime, &meeting.EndTime, &meeting.Duration, &meeting.TotalWords,
			&meeting.Tokens, &meeting.FilePath, &meeting.Language, &meeting.Status,
			&meeting.CreatedAt, &meeting.UpdatedAt, &meeting.DeletedAt,
			&result.ConsumedSeconds, &result.TranscriptionSeconds, &result.TranslationSeconds,
		)
		if err != nil {
			return nil, errors.NewDatabaseError("扫描会议数据失败", err)
		}

		result.Meeting = &meeting
		results = append(results, &result)
	}

	if err = rows.Err(); err != nil {
		return nil, errors.NewDatabaseError("遍历会议数据失败", err)
	}

	return results, nil
}

// generateSessionUUID 生成会议的唯一 session UUID
func generateSessionUUID() string {
	return uuid.New().String()
}
