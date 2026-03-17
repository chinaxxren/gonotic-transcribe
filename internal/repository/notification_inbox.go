package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/pkg/database"
)

// NotificationInboxRepository 负责 Apple/Provider 通知任务的持久化与调度。
type NotificationInboxRepository interface {
	Enqueue(ctx context.Context, item *model.NotificationInbox) error                 // 新增通知
	ClaimPending(ctx context.Context, limit int) ([]*model.NotificationInbox, error)  // 领取待处理通知
	MarkDone(ctx context.Context, id int64) error                                     // 标记处理完成
	MarkRetry(ctx context.Context, id int64, delaySeconds int64, errMsg string) error // 重新入队
	MarkDead(ctx context.Context, id int64, errMsg string) error                      // 标记死信
}

type notificationInboxRepository struct {
	db *sql.DB
}

// NewNotificationInboxRepository 创建仓储实例。
func NewNotificationInboxRepository(db *database.DB) NotificationInboxRepository {
	return &notificationInboxRepository{db: db.DB.DB}
}

// Enqueue 写入一条通知，供 worker 后续领取。
func (r *notificationInboxRepository) Enqueue(ctx context.Context, item *model.NotificationInbox) error {
	query := `
		INSERT INTO notification_inbox (
			provider, original_transaction_id, event_type, sequence,
			payload, state, retry_count, available_at, error_message,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`

	now := time.Now().Unix()

	var id int64
	err := r.db.QueryRowContext(ctx, sqlx.Rebind(sqlx.DOLLAR, query),
		item.Provider,
		item.OriginalTransactionID,
		item.EventType,
		item.Sequence,
		item.Payload,
		item.State,
		item.RetryCount,
		item.AvailableAt,
		item.ErrorMessage,
		now,
		now,
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("写入 notification_inbox 失败: %w", err)
	}
	item.ID = id
	return nil
}

// ClaimPending 领取指定数量的待处理通知，内部使用 FOR UPDATE SKIP LOCKED 保证并发安全。
func (r *notificationInboxRepository) ClaimPending(ctx context.Context, limit int) ([]*model.NotificationInbox, error) {
	if limit <= 0 {
		limit = 10
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("开启事务失败: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	now := time.Now().Unix()
	query := fmt.Sprintf(`
		SELECT id, provider, original_transaction_id, event_type, sequence,
			payload, state, retry_count, available_at, error_message,
			created_at, updated_at
		FROM notification_inbox
		WHERE state IN ('PENDING','RETRY_PENDING')
			AND available_at <= ?
		ORDER BY available_at ASC
		LIMIT %d
		FOR UPDATE SKIP LOCKED
	`, limit)

	query = sqlx.Rebind(sqlx.DOLLAR, query)
	rows, qErr := tx.QueryContext(ctx, query, now)
	if qErr != nil {
		err = fmt.Errorf("查询待处理通知失败: %w", qErr)
		return nil, err
	}

	items, scanErr := scanNotifications(rows)
	if scanErr != nil {
		err = scanErr
		return nil, err
	}

	if len(items) == 0 {
		tx.Rollback()
		return nil, nil
	}

	placeholders := make([]string, len(items))
	args := make([]interface{}, len(items))
	for i, item := range items {
		placeholders[i] = "?"
		args[i] = item.ID
	}

	updateQuery := fmt.Sprintf(
		"UPDATE notification_inbox SET state = 'PROCESSING', updated_at = ? WHERE id IN (%s)",
		strings.Join(placeholders, ","),
	)
	updateQuery = sqlx.Rebind(sqlx.DOLLAR, updateQuery)
	if _, uErr := tx.ExecContext(ctx, updateQuery, append([]interface{}{time.Now().Unix()}, args...)...); uErr != nil {
		err = fmt.Errorf("更新通知状态失败: %w", uErr)
		return nil, err
	}

	if cErr := tx.Commit(); cErr != nil {
		return nil, fmt.Errorf("提交事务失败: %w", cErr)
	}

	return items, nil
}

// MarkDone 处理成功。
func (r *notificationInboxRepository) MarkDone(ctx context.Context, id int64) error {
	query := `
		UPDATE notification_inbox
		SET state = 'DONE', updated_at = ?
		WHERE id = ?
	`
	if _, err := r.db.ExecContext(ctx, sqlx.Rebind(sqlx.DOLLAR, query), time.Now().Unix(), id); err != nil {
		return fmt.Errorf("标记 DONE 失败: %w", err)
	}
	return nil
}

// MarkRetry 更新状态为 RETRY_PENDING，并设置下一次可用时间。
func (r *notificationInboxRepository) MarkRetry(ctx context.Context, id int64, delaySeconds int64, errMsg string) error {
	availableAt := time.Now().Unix() + delaySeconds
	query := `
		UPDATE notification_inbox
		SET state = 'RETRY_PENDING', retry_count = retry_count + 1,
			available_at = ?, error_message = ?, updated_at = ?
		WHERE id = ?
	`
	if _, err := r.db.ExecContext(ctx, sqlx.Rebind(sqlx.DOLLAR, query), availableAt, errMsg, time.Now().Unix(), id); err != nil {
		return fmt.Errorf("标记 RETRY 失败: %w", err)
	}
	return nil
}

// MarkDead 将任务打入死信队列，需人工介入。
func (r *notificationInboxRepository) MarkDead(ctx context.Context, id int64, errMsg string) error {
	query := `
		UPDATE notification_inbox
		SET state = 'DEAD', error_message = ?, updated_at = ?
		WHERE id = ?
	`
	if _, err := r.db.ExecContext(ctx, sqlx.Rebind(sqlx.DOLLAR, query), errMsg, time.Now().Unix(), id); err != nil {
		return fmt.Errorf("标记 DEAD 失败: %w", err)
	}
	return nil
}

// scanNotifications 将 rows 解析成结构体切片。
func scanNotifications(rows *sql.Rows) ([]*model.NotificationInbox, error) {
	defer rows.Close()

	var results []*model.NotificationInbox
	for rows.Next() {
		var item model.NotificationInbox
		var errMsg sql.NullString
		if err := rows.Scan(
			&item.ID,
			&item.Provider,
			&item.OriginalTransactionID,
			&item.EventType,
			&item.Sequence,
			&item.Payload,
			&item.State,
			&item.RetryCount,
			&item.AvailableAt,
			&errMsg,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("扫描通知失败: %w", err)
		}
		if errMsg.Valid {
			value := errMsg.String
			item.ErrorMessage = &value
		}
		results = append(results, &item)
	}

	return results, nil
}
