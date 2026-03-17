package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/pkg/database"
	"go.uber.org/zap"
)

// UserCycleRepository defines persistence operations for user_cycle_states.
type UserCycleRepository interface {
	Create(ctx context.Context, cycle *model.UserCycleState) error
	FindSystemFreeCycle(ctx context.Context, userID int) (*model.UserCycleState, error)
	ResetSystemFreeCycle(ctx context.Context, id int64, periodStart int64, periodEnd int64, totalSeconds int, summaryTotal int) error
	UpdateState(ctx context.Context, id int64, from, to model.CycleState) error
	UpdatePeriod(ctx context.Context, id int64, periodStart, periodEnd *int64) error
	IncrementUsage(ctx context.Context, id int64, usedDelta int) error
	IncrementSummaryUsage(ctx context.Context, id int64, usedDelta int) error
	ListByUserAndStates(ctx context.Context, userID int, states []model.CycleState) ([]*model.UserCycleState, error)
	ListEffectiveByUserAndStates(ctx context.Context, userID int, states []model.CycleState, now int64) ([]*model.UserCycleState, error)
	ListConsumable(ctx context.Context, userID int) ([]*model.UserCycleState, error)
	ListActiveByPremium(ctx context.Context, userID int, states []model.CycleState, subscriptionID int64) ([]*model.UserCycleState, error)
	ListByTierAndStates(ctx context.Context, userID int, tier model.AccountRole, states []model.CycleState) ([]*model.UserCycleState, error)
	ListAllActiveExpired(ctx context.Context, now int64) ([]*model.UserCycleState, error)
	FindLatestEndedPaidCycle(ctx context.Context, userID int) (*model.UserCycleState, error)
}

type userCycleRepository struct {
	db     *sql.DB
	logger *zap.Logger
}

// NewUserCycleRepository creates a repository bound to the app database.
func NewUserCycleRepository(db *database.DB, logger *zap.Logger) UserCycleRepository {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &userCycleRepository{
		db:     db.DB.DB,
		logger: logger,
	}
}

func (r *userCycleRepository) Create(ctx context.Context, cycle *model.UserCycleState) error {
	if cycle == nil {
		return fmt.Errorf("cycle cannot be nil")
	}
	query := `
		INSERT INTO user_cycle_states (
			user_id, tier, state, origin_type, origin_id,
			cycle_no, period_start, period_end, total_seconds, used_seconds,
			summary_total, summary_used,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	now := time.Now().Unix()
	if cycle.CreatedAt == 0 {
		cycle.CreatedAt = now
	}
	if cycle.UpdatedAt == 0 {
		cycle.UpdatedAt = now
	}
	_, err := database.ExecContext(ctx, r.db, query,
		cycle.UserID,
		string(cycle.Tier),
		string(cycle.State),
		string(cycle.OriginType),
		nullableInt64Ptr(cycle.OriginID),
		cycle.CycleNo,
		nullableInt64Ptr(cycle.PeriodStart),
		nullableInt64Ptr(cycle.PeriodEnd),
		cycle.TotalSeconds,
		cycle.UsedSeconds,
		cycle.SummaryTotal,
		cycle.SummaryUsed,
		cycle.CreatedAt,
		cycle.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create user cycle failed: %w", err)
	}
	return nil
}

func (r *userCycleRepository) IncrementSummaryUsage(ctx context.Context, id int64, usedDelta int) error {
	query := `
		UPDATE user_cycle_states
		SET summary_used = LEAST(summary_used + ?, summary_total), updated_at = ?
		WHERE id = ? AND summary_used + ? <= summary_total
	`
	result, err := database.ExecContext(ctx, r.db, query, usedDelta, time.Now().Unix(), id, usedDelta)
	if err != nil {
		return fmt.Errorf("increment summary usage failed: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("increment summary usage failed: would exceed cycle capacity or cycle not found")
	}
	return nil
}

func (r *userCycleRepository) FindSystemFreeCycle(ctx context.Context, userID int) (*model.UserCycleState, error) {
	if userID == 0 {
		return nil, nil
	}
	query := `
		SELECT id, user_id, tier, state, origin_type, origin_id,
		       cycle_no, period_start, period_end, total_seconds, used_seconds,
		       summary_total, summary_used,
		       created_at, updated_at
		FROM user_cycle_states
		WHERE user_id = ?
		  AND tier = ?
		  AND origin_type = ?
		ORDER BY updated_at DESC, created_at DESC, id DESC
		LIMIT 1
	`
	row := database.QueryRowContext(ctx, r.db, query, userID, string(model.AccountRoleFree), string(model.CycleOriginSystem))
	var cycle model.UserCycleState
	var tier string
	var state string
	var originType string
	var originID sql.NullInt64
	var periodStart sql.NullInt64
	var periodEnd sql.NullInt64
	if err := row.Scan(
		&cycle.ID,
		&cycle.UserID,
		&tier,
		&state,
		&originType,
		&originID,
		&cycle.CycleNo,
		&periodStart,
		&periodEnd,
		&cycle.TotalSeconds,
		&cycle.UsedSeconds,
		&cycle.SummaryTotal,
		&cycle.SummaryUsed,
		&cycle.CreatedAt,
		&cycle.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find system free cycle failed: %w", err)
	}
	cycle.Tier = model.AccountRole(tier)
	cycle.State = model.CycleState(state)
	cycle.OriginType = model.CycleOriginType(originType)
	if originID.Valid {
		value := originID.Int64
		cycle.OriginID = &value
	}
	if periodStart.Valid {
		value := periodStart.Int64
		cycle.PeriodStart = &value
	}
	if periodEnd.Valid {
		value := periodEnd.Int64
		cycle.PeriodEnd = &value
	}
	return &cycle, nil
}

func (r *userCycleRepository) ResetSystemFreeCycle(ctx context.Context, id int64, periodStart int64, periodEnd int64, totalSeconds int, summaryTotal int) error {
	if id == 0 {
		return fmt.Errorf("cycle id is required")
	}
	query := `
		UPDATE user_cycle_states
		SET period_start = ?,
		    period_end = ?,
		    total_seconds = ?,
		    used_seconds = 0,
		    summary_total = ?,
		    summary_used = 0,
		    state = ?,
		    updated_at = ?
		WHERE id = ?
	`
	_, err := database.ExecContext(ctx, r.db, query, periodStart, periodEnd, totalSeconds, summaryTotal, string(model.CycleStateActive), time.Now().Unix(), id)
	if err != nil {
		return fmt.Errorf("reset system free cycle failed: %w", err)
	}
	return nil
}

func (r *userCycleRepository) ListAllActiveExpired(ctx context.Context, now int64) ([]*model.UserCycleState, error) {
	query := `
		SELECT id, user_id, tier, state, origin_type, origin_id,
		       cycle_no, period_start, period_end, total_seconds, used_seconds,
		       summary_total, summary_used,
		       created_at, updated_at
		FROM user_cycle_states
		WHERE state IN (?, ?)
		  AND period_end IS NOT NULL
		  AND period_end <= ?
		ORDER BY period_end ASC
	`
	rows, err := database.QueryContext(ctx, r.db, query, string(model.CycleStateActive), string(model.CycleStateCompleted), now)
	if err != nil {
		return nil, fmt.Errorf("list all expired active cycles failed: %w", err)
	}
	defer rows.Close()

	var result []*model.UserCycleState
	for rows.Next() {
		var cycle model.UserCycleState
		var tier string
		var state string
		var originType string
		var originID sql.NullInt64
		var periodStart sql.NullInt64
		var periodEnd sql.NullInt64
		if err := rows.Scan(
			&cycle.ID,
			&cycle.UserID,
			&tier,
			&state,
			&originType,
			&originID,
			&cycle.CycleNo,
			&periodStart,
			&periodEnd,
			&cycle.TotalSeconds,
			&cycle.UsedSeconds,
			&cycle.SummaryTotal,
			&cycle.SummaryUsed,
			&cycle.CreatedAt,
			&cycle.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan expired active cycle failed: %w", err)
		}
		cycle.Tier = model.AccountRole(tier)
		cycle.State = model.CycleState(state)
		cycle.OriginType = model.CycleOriginType(originType)
		if originID.Valid {
			value := originID.Int64
			cycle.OriginID = &value
		}
		if periodStart.Valid {
			value := periodStart.Int64
			cycle.PeriodStart = &value
		}
		if periodEnd.Valid {
			value := periodEnd.Int64
			cycle.PeriodEnd = &value
		}
		result = append(result, &cycle)
	}
	return result, nil
}

func (r *userCycleRepository) ListEffectiveByUserAndStates(ctx context.Context, userID int, states []model.CycleState, now int64) ([]*model.UserCycleState, error) {
	if userID == 0 {
		return []*model.UserCycleState{}, nil
	}
	if len(states) == 0 {
		return []*model.UserCycleState{}, nil
	}
	placeholders := make([]string, len(states))
	args := make([]interface{}, 0, len(states)+3)
	args = append(args, userID)
	for i, state := range states {
		placeholders[i] = "?"
		args = append(args, string(state))
	}
	args = append(args, now, now)

	query := fmt.Sprintf(`
		SELECT id, user_id, tier, state, origin_type, origin_id,
		       cycle_no, period_start, period_end, total_seconds, used_seconds,
		       summary_total, summary_used,
		       created_at, updated_at
		FROM user_cycle_states
		WHERE user_id = ?
		  AND state IN (%s)
		  AND (period_start IS NULL OR period_start <= ?)
		  AND (period_end IS NULL OR period_end > ?)
		ORDER BY (period_end IS NOT NULL) ASC, period_end ASC, created_at ASC, id ASC
	`, strings.Join(placeholders, ","))
	rows, err := database.QueryContext(ctx, r.db, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list effective user cycles failed: %w", err)
	}
	defer rows.Close()

	var result []*model.UserCycleState
	for rows.Next() {
		var cycle model.UserCycleState
		var tier string
		var state string
		var originType string
		var originID sql.NullInt64
		var periodStart sql.NullInt64
		var periodEnd sql.NullInt64
		if err := rows.Scan(
			&cycle.ID,
			&cycle.UserID,
			&tier,
			&state,
			&originType,
			&originID,
			&cycle.CycleNo,
			&periodStart,
			&periodEnd,
			&cycle.TotalSeconds,
			&cycle.UsedSeconds,
			&cycle.SummaryTotal,
			&cycle.SummaryUsed,
			&cycle.CreatedAt,
			&cycle.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan effective user cycle failed: %w", err)
		}
		cycle.Tier = model.AccountRole(tier)
		cycle.State = model.CycleState(state)
		cycle.OriginType = model.CycleOriginType(originType)
		if originID.Valid {
			value := originID.Int64
			cycle.OriginID = &value
		}
		if periodStart.Valid {
			value := periodStart.Int64
			cycle.PeriodStart = &value
		}
		if periodEnd.Valid {
			value := periodEnd.Int64
			cycle.PeriodEnd = &value
		}
		result = append(result, &cycle)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate effective user cycles failed: %w", err)
	}
	return result, nil
}

func (r *userCycleRepository) FindLatestEndedPaidCycle(ctx context.Context, userID int) (*model.UserCycleState, error) {
	if userID == 0 {
		return nil, nil
	}
	query := `
		SELECT id, user_id, tier, state, origin_type, origin_id,
		       cycle_no, period_start, period_end, total_seconds, used_seconds,
		       summary_total, summary_used,
		       created_at, updated_at
		FROM user_cycle_states
		WHERE user_id = ?
		  AND tier IN (?, ?, ?)
		  AND state IN (?, ?, ?)
		  AND period_end IS NOT NULL
		ORDER BY period_end DESC, created_at DESC
		LIMIT 1
	`
	row := database.QueryRowContext(
		ctx,
		r.db,
		query,
		userID,
		string(model.AccountRolePayg),
		string(model.AccountRolePremium),
		string(model.AccountRolePro),
		string(model.CycleStateActive),
		string(model.CycleStateCompleted),
		string(model.CycleStateExpired),
	)

	var cycle model.UserCycleState
	var tier string
	var state string
	var originType string
	var originID sql.NullInt64
	var periodStart sql.NullInt64
	var periodEnd sql.NullInt64
	if err := row.Scan(
		&cycle.ID,
		&cycle.UserID,
		&tier,
		&state,
		&originType,
		&originID,
		&cycle.CycleNo,
		&periodStart,
		&periodEnd,
		&cycle.TotalSeconds,
		&cycle.UsedSeconds,
		&cycle.SummaryTotal,
		&cycle.SummaryUsed,
		&cycle.CreatedAt,
		&cycle.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find latest ended paid cycle failed: %w", err)
	}
	cycle.Tier = model.AccountRole(tier)
	cycle.State = model.CycleState(state)
	cycle.OriginType = model.CycleOriginType(originType)
	if originID.Valid {
		value := originID.Int64
		cycle.OriginID = &value
	}
	if periodStart.Valid {
		value := periodStart.Int64
		cycle.PeriodStart = &value
	}
	if periodEnd.Valid {
		value := periodEnd.Int64
		cycle.PeriodEnd = &value
	}
	return &cycle, nil
}

func (r *userCycleRepository) UpdateState(ctx context.Context, id int64, from, to model.CycleState) error {
	query := `
		UPDATE user_cycle_states
		SET state = ?, updated_at = ?
		WHERE id = ? AND state = ?
	`
	res, err := database.ExecContext(ctx, r.db, query, string(to), time.Now().Unix(), id, string(from))
	if err != nil {
		return fmt.Errorf("update user cycle state failed: %w", err)
	}
	affected, err := res.RowsAffected()
	if err == nil && affected == 0 {
		return sql.ErrNoRows
	}
	return err
}

func (r *userCycleRepository) UpdatePeriod(ctx context.Context, id int64, periodStart, periodEnd *int64) error {
	query := `
		UPDATE user_cycle_states
		SET period_start = ?, period_end = ?, updated_at = ?
		WHERE id = ?
	`
	_, err := database.ExecContext(ctx, r.db, query,
		nullableInt64Ptr(periodStart),
		nullableInt64Ptr(periodEnd),
		time.Now().Unix(),
		id,
	)
	if err != nil {
		return fmt.Errorf("update user cycle period failed: %w", err)
	}
	return nil
}

func (r *userCycleRepository) ListByUserAndStates(ctx context.Context, userID int, states []model.CycleState) ([]*model.UserCycleState, error) {
	if len(states) == 0 {
		return []*model.UserCycleState{}, nil
	}
	placeholders := make([]string, len(states))
	args := make([]interface{}, 0, len(states)+1)
	args = append(args, userID)
	for i, state := range states {
		placeholders[i] = "?"
		args = append(args, string(state))
	}
	query := fmt.Sprintf(`
		SELECT id, user_id, tier, state, origin_type, origin_id,
		       cycle_no, period_start, period_end, total_seconds, used_seconds,
		       summary_total, summary_used,
		       created_at, updated_at
		FROM user_cycle_states
		WHERE user_id = ? AND state IN (%s)
		ORDER BY period_start ASC
	`, strings.Join(placeholders, ","))
	rows, err := database.QueryContext(ctx, r.db, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list user cycles failed: %w", err)
	}
	defer rows.Close()

	var result []*model.UserCycleState
	for rows.Next() {
		var cycle model.UserCycleState
		var tier string
		var state string
		var originType string
		var originID sql.NullInt64
		var periodStart sql.NullInt64
		var periodEnd sql.NullInt64
		if err := rows.Scan(
			&cycle.ID,
			&cycle.UserID,
			&tier,
			&state,
			&originType,
			&originID,
			&cycle.CycleNo,
			&periodStart,
			&periodEnd,
			&cycle.TotalSeconds,
			&cycle.UsedSeconds,
			&cycle.SummaryTotal,
			&cycle.SummaryUsed,
			&cycle.CreatedAt,
			&cycle.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan user cycle failed: %w", err)
		}
		cycle.Tier = model.AccountRole(tier)
		cycle.State = model.CycleState(state)
		cycle.OriginType = model.CycleOriginType(originType)
		if originID.Valid {
			value := originID.Int64
			cycle.OriginID = &value
		}
		if periodStart.Valid {
			value := periodStart.Int64
			cycle.PeriodStart = &value
		}
		if periodEnd.Valid {
			value := periodEnd.Int64
			cycle.PeriodEnd = &value
		}
		result = append(result, &cycle)
	}
	return result, nil
}

func (r *userCycleRepository) IncrementUsage(ctx context.Context, id int64, usedDelta int) error {
	// 添加边界检查，确保不会超过周期的总时长
	query := `
		UPDATE user_cycle_states
		SET used_seconds = LEAST(used_seconds + ?, total_seconds), updated_at = ?
		WHERE id = ? AND used_seconds + ? <= total_seconds
	`
	result, err := database.ExecContext(ctx, r.db, query, usedDelta, time.Now().Unix(), id, usedDelta)
	if err != nil {
		return fmt.Errorf("increment user cycle usage failed: %w", err)
	}

	// 检查是否有行被更新
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("increment usage failed: would exceed cycle capacity or cycle not found")
	}

	return nil
}

func (r *userCycleRepository) ListConsumable(ctx context.Context, userID int) ([]*model.UserCycleState, error) {
	if userID == 0 {
		return []*model.UserCycleState{}, nil
	}
	now := time.Now().Unix()
	query := `
		SELECT id, user_id, tier, state, origin_type, origin_id,
		       cycle_no, period_start, period_end, total_seconds, used_seconds,
		       summary_total, summary_used,
		       created_at, updated_at
		FROM user_cycle_states
		WHERE user_id = ?
		  AND state = ?
		  AND (period_start IS NULL OR period_start <= ?)
		  AND (period_end IS NULL OR period_end > ?)
		ORDER BY (period_end IS NOT NULL) ASC, period_end ASC, created_at ASC, id ASC
	`
	rows, err := database.QueryContext(ctx, r.db, query, userID, string(model.CycleStateActive), now, now)
	if err != nil {
		return nil, fmt.Errorf("list consumable cycles failed: %w", err)
	}
	defer rows.Close()

	var result []*model.UserCycleState
	for rows.Next() {
		var cycle model.UserCycleState
		var tier string
		var state string
		var originType string
		var originID sql.NullInt64
		var periodStart sql.NullInt64
		var periodEnd sql.NullInt64
		if err := rows.Scan(
			&cycle.ID,
			&cycle.UserID,
			&tier,
			&state,
			&originType,
			&originID,
			&cycle.CycleNo,
			&periodStart,
			&periodEnd,
			&cycle.TotalSeconds,
			&cycle.UsedSeconds,
			&cycle.SummaryTotal,
			&cycle.SummaryUsed,
			&cycle.CreatedAt,
			&cycle.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan consumable cycle failed: %w", err)
		}
		cycle.Tier = model.AccountRole(tier)
		cycle.State = model.CycleState(state)
		cycle.OriginType = model.CycleOriginType(originType)
		if originID.Valid {
			value := originID.Int64
			cycle.OriginID = &value
		}
		if periodStart.Valid {
			value := periodStart.Int64
			cycle.PeriodStart = &value
		}
		if periodEnd.Valid {
			value := periodEnd.Int64
			cycle.PeriodEnd = &value
		}
		result = append(result, &cycle)
	}
	return result, nil
}

func (r *userCycleRepository) ListByTierAndStates(ctx context.Context, userID int, tier model.AccountRole, states []model.CycleState) ([]*model.UserCycleState, error) {
	if userID == 0 {
		return []*model.UserCycleState{}, nil
	}
	if len(states) == 0 {
		return []*model.UserCycleState{}, nil
	}
	placeholders := make([]string, len(states))
	args := make([]interface{}, 0, len(states)+2)
	args = append(args, userID, string(tier))
	for i, state := range states {
		placeholders[i] = "?"
		args = append(args, string(state))
	}
	query := fmt.Sprintf(`
		SELECT id, user_id, tier, state, origin_type, origin_id,
		       cycle_no, period_start, period_end, total_seconds, used_seconds,
		       summary_total, summary_used,
		       created_at, updated_at
		FROM user_cycle_states
		WHERE user_id = ?
		  AND tier = ?
		  AND state IN (%s)
		ORDER BY period_start ASC
	`, strings.Join(placeholders, ","))
	rows, err := database.QueryContext(ctx, r.db, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list user cycles by tier failed: %w", err)
	}
	defer rows.Close()

	var result []*model.UserCycleState
	for rows.Next() {
		var cycle model.UserCycleState
		var tierVal string
		var stateVal string
		var originType string
		var originID sql.NullInt64
		var periodStart sql.NullInt64
		var periodEnd sql.NullInt64
		if err := rows.Scan(
			&cycle.ID,
			&cycle.UserID,
			&tierVal,
			&stateVal,
			&originType,
			&originID,
			&cycle.CycleNo,
			&periodStart,
			&periodEnd,
			&cycle.TotalSeconds,
			&cycle.UsedSeconds,
			&cycle.SummaryTotal,
			&cycle.SummaryUsed,
			&cycle.CreatedAt,
			&cycle.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan user cycle by tier failed: %w", err)
		}
		cycle.Tier = model.AccountRole(tierVal)
		cycle.State = model.CycleState(stateVal)
		cycle.OriginType = model.CycleOriginType(originType)
		if originID.Valid {
			value := originID.Int64
			cycle.OriginID = &value
		}
		if periodStart.Valid {
			value := periodStart.Int64
			cycle.PeriodStart = &value
		}
		if periodEnd.Valid {
			value := periodEnd.Int64
			cycle.PeriodEnd = &value
		}
		result = append(result, &cycle)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user cycles by tier failed: %w", err)
	}
	return result, nil
}

func (r *userCycleRepository) ListActiveByPremium(ctx context.Context, userID int, states []model.CycleState, subscriptionID int64) ([]*model.UserCycleState, error) {
	if userID == 0 || subscriptionID == 0 {
		return []*model.UserCycleState{}, nil
	}
	if len(states) == 0 {
		return []*model.UserCycleState{}, nil
	}
	placeholders := make([]string, len(states))
	args := make([]interface{}, 0, len(states)+3)
	args = append(args, userID, string(model.AccountRolePremium))
	for i, st := range states {
		placeholders[i] = "?"
		args = append(args, string(st))
	}
	args = append(args, string(model.CycleOriginSubscription), subscriptionID)
	query := fmt.Sprintf(`
		SELECT id, user_id, tier, state, origin_type, origin_id,
		       cycle_no, period_start, period_end, total_seconds, used_seconds,
		       created_at, updated_at
		FROM user_cycle_states
		WHERE user_id = ?
		  AND tier = ?
		  AND state IN (%s)
		  AND origin_type = ?
		  AND origin_id = ?
		ORDER BY period_start ASC
	`, strings.Join(placeholders, ","))
	rows, err := database.QueryContext(ctx, r.db, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list subscription cycles failed: %w", err)
	}
	defer rows.Close()

	var result []*model.UserCycleState
	for rows.Next() {
		var cycle model.UserCycleState
		var tierVal string
		var stateVal string
		var originType string
		var originID sql.NullInt64
		var periodStart sql.NullInt64
		var periodEnd sql.NullInt64
		if err := rows.Scan(
			&cycle.ID,
			&cycle.UserID,
			&tierVal,
			&stateVal,
			&originType,
			&originID,
			&cycle.CycleNo,
			&periodStart,
			&periodEnd,
			&cycle.TotalSeconds,
			&cycle.UsedSeconds,
			&cycle.CreatedAt,
			&cycle.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan subscription cycle failed: %w", err)
		}
		cycle.Tier = model.AccountRole(tierVal)
		cycle.State = model.CycleState(stateVal)
		cycle.OriginType = model.CycleOriginType(originType)
		if originID.Valid {
			value := originID.Int64
			cycle.OriginID = &value
		}
		if periodStart.Valid {
			value := periodStart.Int64
			cycle.PeriodStart = &value
		}
		if periodEnd.Valid {
			value := periodEnd.Int64
			cycle.PeriodEnd = &value
		}
		result = append(result, &cycle)
	}
	return result, nil
}

func nullableInt64Ptr(ptr *int64) interface{} {
	if ptr == nil {
		return nil
	}
	return *ptr
}
