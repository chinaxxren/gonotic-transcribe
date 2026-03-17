package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/pkg/database"
)

// SubscriptionRepository 负责订阅表的 CRUD 操作，供支付/调度等服务调用。
type SubscriptionRepository interface {
	Create(ctx context.Context, sub *model.Subscription) error                                        // 创建订阅
	GetByID(ctx context.Context, id int64) (*model.Subscription, error)                               // 通过 ID 查询
	GetByOriginalTxn(ctx context.Context, provider, originalTxn string) (*model.Subscription, error)  // 通过原始交易查询
	UpdateProductType(ctx context.Context, id int64, productType model.SubscriptionProductType) error // 更新产品类型
	UpdatePeriodWindow(ctx context.Context, id int64, currentStart, currentEnd int64, nextGrantAt *int64, periodsGranted int, periodsConsumed int) error
	ClearNextGrantAt(ctx context.Context, id int64) error
	UpdateStatus(ctx context.Context, id int64, status model.SubscriptionStatus) error              // 更新状态
	UpdateRenewalState(ctx context.Context, id int64, renewal model.SubscriptionRenewalState) error // 更新自动续订状态
	UpdateLatestTxn(ctx context.Context, id int64, latestTxn string) error                          // 更新最新交易号
	UpdateNextGrant(ctx context.Context, id int64, nextGrant int64, periodsGranted int) error       // 更新下一次发放时间
	UpdateExpiresAt(ctx context.Context, id int64, expiresAt int64) error                           // 更新订阅过期时间
	IncrementConsumed(ctx context.Context, id int64, delta int) error                               // 消耗周期计数
	ListDueForGrant(ctx context.Context, cutoff int64, limit int) ([]*model.Subscription, error)    // 查找到期发放的订阅
}

// GetByID 根据订阅 ID 查询记录。
func (r *subscriptionRepository) GetByID(ctx context.Context, id int64) (*model.Subscription, error) {
	query := `
		SELECT id, user_id, provider, product_type, original_txn_id, latest_txn_id,
			status, renewal_state, current_period_start, current_period_end,
			next_grant_at, expires_at, periods_granted, periods_consumed, created_at, updated_at
		FROM subscriptions
		WHERE id = ?
		LIMIT 1
	`

	var sub model.Subscription
	var latestTxn sql.NullString
	var nextGrant sql.NullInt64
	var expires sql.NullInt64
	row := database.QueryRowContext(ctx, r.db, query, id)
	if err := row.Scan(
		&sub.ID,
		&sub.UserID,
		&sub.Provider,
		&sub.ProductType,
		&sub.OriginalTxnID,
		&latestTxn,
		&sub.Status,
		&sub.RenewalState,
		&sub.CurrentPeriodStart,
		&sub.CurrentPeriodEnd,
		&nextGrant,
		&expires,
		&sub.PeriodsGranted,
		&sub.PeriodsConsumed,
		&sub.CreatedAt,
		&sub.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("通过 id 查询订阅失败: %w", err)
	}

	if latestTxn.Valid {
		value := latestTxn.String
		sub.LatestTxnID = &value
	}
	if nextGrant.Valid {
		value := nextGrant.Int64
		sub.NextGrantAt = &value
	}
	if expires.Valid {
		value := expires.Int64
		sub.ExpiresAt = &value
	}
	return &sub, nil
}

func (r *subscriptionRepository) UpdateProductType(ctx context.Context, id int64, productType model.SubscriptionProductType) error {
	query := `
		UPDATE subscriptions
		SET product_type = ?, updated_at = ?
		WHERE id = ?
	`
	if _, err := database.ExecContext(ctx, r.db, query, productType, time.Now().Unix(), id); err != nil {
		return fmt.Errorf("更新订阅产品类型失败: %w", err)
	}
	return nil
}

func (r *subscriptionRepository) ClearNextGrantAt(ctx context.Context, id int64) error {
	query := `
		UPDATE subscriptions
		SET next_grant_at = NULL, updated_at = ?
		WHERE id = ?
	`
	if _, err := database.ExecContext(ctx, r.db, query, time.Now().Unix(), id); err != nil {
		return fmt.Errorf("清空 next_grant_at 失败: %w", err)
	}
	return nil
}

func (r *subscriptionRepository) UpdatePeriodWindow(ctx context.Context, id int64, currentStart, currentEnd int64, nextGrantAt *int64, periodsGranted int, periodsConsumed int) error {
	query := `
		UPDATE subscriptions
		SET current_period_start = ?,
			current_period_end = ?,
			next_grant_at = ?,
			periods_granted = ?,
			periods_consumed = ?,
			updated_at = ?
		WHERE id = ?
	`
	var next interface{}
	if nextGrantAt != nil {
		next = *nextGrantAt
	}
	if _, err := database.ExecContext(ctx, r.db, query, currentStart, currentEnd, next, periodsGranted, periodsConsumed, time.Now().Unix(), id); err != nil {
		return fmt.Errorf("更新订阅周期窗口失败: %w", err)
	}
	return nil
}

// subscriptionRepository is SubscriptionRepository's PostgreSQL implementation.
type subscriptionRepository struct {
	db *sql.DB
}

// NewSubscriptionRepository 创建订阅仓储实例。
func NewSubscriptionRepository(db *database.DB) SubscriptionRepository {
	return &subscriptionRepository{db: db.DB.DB}
}

// Create 插入订阅记录。
func (r *subscriptionRepository) Create(ctx context.Context, sub *model.Subscription) error {
	query := `
		INSERT INTO subscriptions (
			user_id, provider, product_type, original_txn_id, latest_txn_id,
			status, renewal_state, current_period_start, current_period_end,
			next_grant_at, expires_at, periods_granted, periods_consumed,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`

	var latest interface{}
	if sub.LatestTxnID != nil {
		latest = *sub.LatestTxnID
	}

	var nextGrant interface{}
	if sub.NextGrantAt != nil {
		nextGrant = *sub.NextGrantAt
	}

	var expires interface{}
	if sub.ExpiresAt != nil {
		expires = *sub.ExpiresAt
	}

	now := time.Now().Unix()

	var id int64
	err := database.QueryRowContext(ctx, r.db, query,
		sub.UserID,
		sub.Provider,
		sub.ProductType,
		sub.OriginalTxnID,
		latest,
		sub.Status,
		sub.RenewalState,
		sub.CurrentPeriodStart,
		sub.CurrentPeriodEnd,
		nextGrant,
		expires,
		sub.PeriodsGranted,
		sub.PeriodsConsumed,
		now,
		now,
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("创建订阅失败: %w", err)
	}

	sub.ID = id
	sub.CreatedAt = now
	sub.UpdatedAt = now
	return nil
}

// GetByOriginalTxn 根据提供商+原始交易号查询订阅。
func (r *subscriptionRepository) GetByOriginalTxn(ctx context.Context, provider, originalTxn string) (*model.Subscription, error) {
	query := `
		SELECT id, user_id, provider, product_type, original_txn_id, latest_txn_id,
			status, renewal_state, current_period_start, current_period_end,
			next_grant_at, expires_at, periods_granted, periods_consumed, created_at, updated_at
		FROM subscriptions
		WHERE provider = ? AND original_txn_id = ?
		LIMIT 1
	`

	var sub model.Subscription
	var latestTxn sql.NullString
	var nextGrant sql.NullInt64
	var expires sql.NullInt64
	row := database.QueryRowContext(ctx, r.db, query, provider, originalTxn)
	if err := row.Scan(
		&sub.ID,
		&sub.UserID,
		&sub.Provider,
		&sub.ProductType,
		&sub.OriginalTxnID,
		&latestTxn,
		&sub.Status,
		&sub.RenewalState,
		&sub.CurrentPeriodStart,
		&sub.CurrentPeriodEnd,
		&nextGrant,
		&expires,
		&sub.PeriodsGranted,
		&sub.PeriodsConsumed,
		&sub.CreatedAt,
		&sub.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("查询订阅失败: %w", err)
	}

	if latestTxn.Valid {
		value := latestTxn.String
		sub.LatestTxnID = &value
	}
	if nextGrant.Valid {
		value := nextGrant.Int64
		sub.NextGrantAt = &value
	}
	if expires.Valid {
		value := expires.Int64
		sub.ExpiresAt = &value
	}
	return &sub, nil
}

// UpdateStatus 更新订阅状态。
func (r *subscriptionRepository) UpdateStatus(ctx context.Context, id int64, status model.SubscriptionStatus) error {
	query := `
		UPDATE subscriptions
		SET status = ?, updated_at = ?
		WHERE id = ?
	`
	if _, err := database.ExecContext(ctx, r.db, query, status, time.Now().Unix(), id); err != nil {
		return fmt.Errorf("更新订阅状态失败: %w", err)
	}
	return nil
}

// UpdateRenewalState 更新自动续订标识。
func (r *subscriptionRepository) UpdateRenewalState(ctx context.Context, id int64, renewal model.SubscriptionRenewalState) error {
	query := `
		UPDATE subscriptions
		SET renewal_state = ?, updated_at = ?
		WHERE id = ?
	`
	if _, err := database.ExecContext(ctx, r.db, query, renewal, time.Now().Unix(), id); err != nil {
		return fmt.Errorf("更新续订状态失败: %w", err)
	}
	return nil
}

// UpdateLatestTxn 更新 latest_txn_id 字段，便于追踪最近一次 Apple 交易。
func (r *subscriptionRepository) UpdateLatestTxn(ctx context.Context, id int64, latestTxn string) error {
	query := `
		UPDATE subscriptions
		SET latest_txn_id = ?, updated_at = ?
		WHERE id = ?
	`
	if _, err := database.ExecContext(ctx, r.db, query, latestTxn, time.Now().Unix(), id); err != nil {
		return fmt.Errorf("更新 latest_txn_id 失败: %w", err)
	}
	return nil
}

// UpdateNextGrant 更新 next_grant_at 以及 periods_granted。
func (r *subscriptionRepository) UpdateNextGrant(ctx context.Context, id int64, nextGrant int64, periodsGranted int) error {
	query := `
		UPDATE subscriptions
		SET next_grant_at = ?, periods_granted = ?, updated_at = ?
		WHERE id = ?
	`
	if _, err := database.ExecContext(ctx, r.db, query, nextGrant, periodsGranted, time.Now().Unix(), id); err != nil {
		return fmt.Errorf("更新 next_grant_at 失败: %w", err)
	}
	return nil
}

// UpdateExpiresAt 更新订阅的 expires_at 字段。
func (r *subscriptionRepository) UpdateExpiresAt(ctx context.Context, id int64, expiresAt int64) error {
	query := `
		UPDATE subscriptions
		SET expires_at = ?, updated_at = ?
		WHERE id = ?
	`
	if _, err := database.ExecContext(ctx, r.db, query, expiresAt, time.Now().Unix(), id); err != nil {
		return fmt.Errorf("更新 expires_at 失败: %w", err)
	}
	return nil
}

// IncrementConsumed 增加已消耗周期，配合调度统计使用。
func (r *subscriptionRepository) IncrementConsumed(ctx context.Context, id int64, delta int) error {
	query := `
		UPDATE subscriptions
		SET periods_consumed = periods_consumed + ?, updated_at = ?
		WHERE id = ?
	`
	if _, err := database.ExecContext(ctx, r.db, query, delta, time.Now().Unix(), id); err != nil {
		return fmt.Errorf("更新 periods_consumed 失败: %w", err)
	}
	return nil
}

// ListDueForGrant 查询 next_grant_at 到期需要发放额度的订阅。
func (r *subscriptionRepository) ListDueForGrant(ctx context.Context, cutoff int64, limit int) ([]*model.Subscription, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `
		SELECT id, user_id, provider, product_type, original_txn_id, latest_txn_id,
			status, renewal_state, current_period_start, current_period_end,
			next_grant_at, expires_at, periods_granted, periods_consumed, created_at, updated_at
		FROM subscriptions
		WHERE next_grant_at IS NOT NULL
			AND next_grant_at <= ?
			AND status = 'ACTIVE'
		ORDER BY next_grant_at ASC
		LIMIT ?
	`
	rows, err := database.QueryContext(ctx, r.db, query, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("查询到期订阅失败: %w", err)
	}
	defer rows.Close()

	var subs []*model.Subscription
	for rows.Next() {
		var sub model.Subscription
		var latestTxn sql.NullString
		var nextGrant sql.NullInt64
		var expires sql.NullInt64
		if err := rows.Scan(
			&sub.ID,
			&sub.UserID,
			&sub.Provider,
			&sub.ProductType,
			&sub.OriginalTxnID,
			&latestTxn,
			&sub.Status,
			&sub.RenewalState,
			&sub.CurrentPeriodStart,
			&sub.CurrentPeriodEnd,
			&nextGrant,
			&expires,
			&sub.PeriodsGranted,
			&sub.PeriodsConsumed,
			&sub.CreatedAt,
			&sub.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("扫描订阅失败: %w", err)
		}
		if latestTxn.Valid {
			value := latestTxn.String
			sub.LatestTxnID = &value
		}
		if nextGrant.Valid {
			value := nextGrant.Int64
			sub.NextGrantAt = &value
		}
		if expires.Valid {
			value := expires.Int64
			sub.ExpiresAt = &value
		}
		subs = append(subs, &sub)
	}
	return subs, nil
}
