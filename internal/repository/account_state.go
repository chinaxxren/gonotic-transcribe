package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/pkg/database"
	"go.uber.org/zap"
)

// AccountStateRepository 定义用户账本快照的读写接口。
type AccountStateRepository interface {
	// GetByUserID 根据用户 ID 读取账本快照，若不存在返回 nil。
	GetByUserID(ctx context.Context, userID int) (*model.UserAccountState, error)

	// GetByUserIDForUpdate 带行级锁读取账本快照，需在事务中调用。
	GetByUserIDForUpdate(ctx context.Context, userID int) (*model.UserAccountState, error)

	// Save 使用 UPSERT 写入账本快照，自动更新时间戳。
	Save(ctx context.Context, state *model.UserAccountState) error
}

type accountStateRepository struct {
	db     *database.DB
	logger *zap.Logger
}

// NewAccountStateRepository 构造账本仓储实例。
func NewAccountStateRepository(db *database.DB, logger *zap.Logger) AccountStateRepository {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &accountStateRepository{
		db:     db,
		logger: logger,
	}
}

func (r *accountStateRepository) GetByUserID(ctx context.Context, userID int) (*model.UserAccountState, error) {
	query := `
		SELECT user_id, role, has_ever_paid,
		       free_cycle_start, free_cycle_end, free_total_seconds, free_used_seconds,
		       payg_total_seconds, payg_used_seconds,
		       premium_cycle_index, premium_cycle_start, premium_cycle_end, premium_total_seconds,
		       premium_used_seconds, premium_backlog_seconds,
		       pro_expire_at, pro_total_seconds, pro_used_seconds,
		       updated_at
		FROM user_account_states
		WHERE user_id = $1
	`

	row := r.db.QueryRowContext(ctx, query, userID)
	return scanAccountState(row)
}

func (r *accountStateRepository) GetByUserIDForUpdate(ctx context.Context, userID int) (*model.UserAccountState, error) {
	if _, ok := database.GetTx(ctx); !ok {
		return nil, fmt.Errorf("GetByUserIDForUpdate 必须在事务中调用")
	}

	query := `
		SELECT user_id, role, has_ever_paid,
		       free_cycle_start, free_cycle_end, free_total_seconds, free_used_seconds,
		       payg_total_seconds, payg_used_seconds,
		       premium_cycle_index, premium_cycle_start, premium_cycle_end, premium_total_seconds,
		       premium_used_seconds, premium_backlog_seconds,
		       pro_expire_at, pro_total_seconds, pro_used_seconds,
		       updated_at
		FROM user_account_states
		WHERE user_id = $1
		FOR UPDATE
	`

	row := r.db.QueryRowContext(ctx, query, userID)
	return scanAccountState(row)
}

func (r *accountStateRepository) Save(ctx context.Context, state *model.UserAccountState) error {
	if state == nil {
		return fmt.Errorf("state 不能为空")
	}
	if state.UserID == 0 {
		return fmt.Errorf("UserID 不能为空")
	}
	if state.UpdatedAt == 0 {
		state.UpdatedAt = time.Now().Unix()
	}

	query := `
		INSERT INTO user_account_states (
			user_id, role, has_ever_paid,
			free_cycle_start, free_cycle_end, free_total_seconds, free_used_seconds,
			payg_total_seconds, payg_used_seconds,
			premium_cycle_index, premium_cycle_start, premium_cycle_end, premium_total_seconds,
			premium_used_seconds, premium_backlog_seconds,
			pro_expire_at, pro_total_seconds, pro_used_seconds,
			updated_at
		) VALUES (
			$1, $2, $3,
			$4, $5, $6, $7,
			$8, $9,
			$10, $11, $12, $13,
			$14, $15,
			$16, $17, $18,
			$19
		)
		ON CONFLICT (user_id) DO UPDATE SET
			role = EXCLUDED.role,
			has_ever_paid = EXCLUDED.has_ever_paid,
			free_cycle_start = EXCLUDED.free_cycle_start,
			free_cycle_end = EXCLUDED.free_cycle_end,
			free_total_seconds = EXCLUDED.free_total_seconds,
			free_used_seconds = EXCLUDED.free_used_seconds,
			payg_total_seconds = EXCLUDED.payg_total_seconds,
			payg_used_seconds = EXCLUDED.payg_used_seconds,
			premium_cycle_index = EXCLUDED.premium_cycle_index,
			premium_cycle_start = EXCLUDED.premium_cycle_start,
			premium_cycle_end = EXCLUDED.premium_cycle_end,
			premium_total_seconds = EXCLUDED.premium_total_seconds,
			premium_used_seconds = EXCLUDED.premium_used_seconds,
			premium_backlog_seconds = EXCLUDED.premium_backlog_seconds,
			pro_expire_at = EXCLUDED.pro_expire_at,
			pro_total_seconds = EXCLUDED.pro_total_seconds,
			pro_used_seconds = EXCLUDED.pro_used_seconds,
			updated_at = EXCLUDED.updated_at
	`

	_, err := database.ExecContext(ctx, r.db.DB.DB, query,
		state.UserID,
		string(state.Role),
		state.HasEverPaid,
		nullableInt64(state.FreeCycleStart),
		nullableInt64(state.FreeCycleEnd),
		state.FreeTotal,
		state.FreeUsed,
		state.PaygTotal,
		state.PaygUsed,
		state.PremiumCycleIndex,
		nullableInt64(state.PremiumCycleStart),
		nullableInt64(state.PremiumCycleEnd),
		state.PremiumTotal,
		state.PremiumUsed,
		state.PremiumBacklog,
		nullableInt64(state.ProExpireAt),
		state.ProTotal,
		state.ProUsed,
		state.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("保存用户账本失败: %w", err)
	}
	return nil
}

func scanAccountState(row *sql.Row) (*model.UserAccountState, error) {
	var state model.UserAccountState
	var role string
	var freeStart, freeEnd sql.NullInt64
	var premiumStart, premiumEnd sql.NullInt64
	var proExpire sql.NullInt64

	err := row.Scan(
		&state.UserID,
		&role,
		&state.HasEverPaid,
		&freeStart,
		&freeEnd,
		&state.FreeTotal,
		&state.FreeUsed,
		&state.PaygTotal,
		&state.PaygUsed,
		&state.PremiumCycleIndex,
		&premiumStart,
		&premiumEnd,
		&state.PremiumTotal,
		&state.PremiumUsed,
		&state.PremiumBacklog,
		&proExpire,
		&state.ProTotal,
		&state.ProUsed,
		&state.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取用户账本失败: %w", err)
	}

	state.Role = model.AccountRole(role)
	if freeStart.Valid {
		value := freeStart.Int64
		state.FreeCycleStart = &value
	}
	if freeEnd.Valid {
		value := freeEnd.Int64
		state.FreeCycleEnd = &value
	}
	if premiumStart.Valid {
		value := premiumStart.Int64
		state.PremiumCycleStart = &value
	}
	if premiumEnd.Valid {
		value := premiumEnd.Int64
		state.PremiumCycleEnd = &value
	}
	if proExpire.Valid {
		value := proExpire.Int64
		state.ProExpireAt = &value
	}

	return &state, nil
}

func nullableInt64(ptr *int64) interface{} {
	if ptr == nil {
		return nil
	}
	return *ptr
}
