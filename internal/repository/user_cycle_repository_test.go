package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/pkg/database"
)

func newUserCycleTestDB(t *testing.T) (*database.DB, sqlmock.Sqlmock, func()) {
	t.Helper()
	mdb, mock, err := sqlmock.New()
	require.NoError(t, err)

	sqlxDB := sqlx.NewDb(mdb, "mysql")
	appDB := database.NewFromSQLX(sqlxDB, zap.NewNop())
	cleanup := func() {
		sqlxDB.Close()
		mdb.Close()
	}
	return appDB, mock, cleanup
}

func TestUserCycleRepository_Create(t *testing.T) {
	appDB, mock, cleanup := newUserCycleTestDB(t)
	defer cleanup()

	repo := NewUserCycleRepository(appDB, zap.NewNop())
	now := time.Now().Unix()
	start := now
	end := now + 3600
	originID := int64(42)

	cycle := &model.UserCycleState{
		UserID:       1,
		Tier:         model.AccountRolePremium,
		State:        model.CycleStateActive,
		OriginType:   model.CycleOriginSubscription,
		OriginID:     &originID,
		CycleNo:      1,
		PeriodStart:  &start,
		PeriodEnd:    &end,
		TotalSeconds: 3600,
		UsedSeconds:  0,
		SummaryTotal: 10,
		SummaryUsed:  0,
	}

	mock.ExpectExec(regexp.QuoteMeta(`
        INSERT INTO user_cycle_states (
            user_id, tier, state, origin_type, origin_id,
            cycle_no, period_start, period_end, total_seconds, used_seconds,
            summary_total, summary_used,
            created_at, updated_at
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
    `)).
		WithArgs(
			cycle.UserID,
			string(cycle.Tier),
			string(cycle.State),
			string(cycle.OriginType),
			*cycle.OriginID,
			cycle.CycleNo,
			*cycle.PeriodStart,
			*cycle.PeriodEnd,
			cycle.TotalSeconds,
			cycle.UsedSeconds,
			cycle.SummaryTotal,
			cycle.SummaryUsed,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.Create(context.Background(), cycle)
	require.NoError(t, err)
	require.NotZero(t, cycle.CreatedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserCycleRepository_FindSystemFreeCycle(t *testing.T) {
	appDB, mock, cleanup := newUserCycleTestDB(t)
	defer cleanup()

	repo := NewUserCycleRepository(appDB, zap.NewNop())
	userID := 1
	now := time.Now().Unix()
	start := now - 1800
	end := now + 1800

	rows := sqlmock.NewRows([]string{
		"id", "user_id", "tier", "state", "origin_type", "origin_id",
		"cycle_no", "period_start", "period_end", "total_seconds", "used_seconds",
		"summary_total", "summary_used", "created_at", "updated_at",
	}).AddRow(
		int64(5), userID, string(model.AccountRoleFree), string(model.CycleStateActive),
		string(model.CycleOriginSystem), nil,
		1, start, end, 3600, 900, 5, 2, now-3600, now-1800,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
        SELECT id, user_id, tier, state, origin_type, origin_id,
               cycle_no, period_start, period_end, total_seconds, used_seconds,
               summary_total, summary_used,
               created_at, updated_at
        FROM user_cycle_states
        WHERE user_id = $1
          AND tier = $2
          AND origin_type = $3
        ORDER BY updated_at DESC, created_at DESC, id DESC
        LIMIT 1
    `)).WithArgs(userID, string(model.AccountRoleFree), string(model.CycleOriginSystem)).WillReturnRows(rows)

	got, err := repo.FindSystemFreeCycle(context.Background(), userID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, userID, got.UserID)
	require.Equal(t, model.AccountRoleFree, got.Tier)
	require.Nil(t, got.OriginID)
	require.Equal(t, start, *got.PeriodStart)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserCycleRepository_ResetSystemFreeCycle(t *testing.T) {
	appDB, mock, cleanup := newUserCycleTestDB(t)
	defer cleanup()

	repo := NewUserCycleRepository(appDB, zap.NewNop())
	id := int64(5)
	periodStart := time.Now().Unix()
	periodEnd := periodStart + 3600
	totalSeconds := 7200
	summaryTotal := 15

	mock.ExpectExec(regexp.QuoteMeta(`
        UPDATE user_cycle_states
        SET period_start = $1,
            period_end = $2,
            total_seconds = $3,
            used_seconds = 0,
            summary_total = $4,
            summary_used = 0,
            state = $5,
            updated_at = $6
        WHERE id = $7
    `)).
		WithArgs(periodStart, periodEnd, totalSeconds, summaryTotal, string(model.CycleStateActive), sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.ResetSystemFreeCycle(context.Background(), id, periodStart, periodEnd, totalSeconds, summaryTotal)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserCycleRepository_IncrementUsage(t *testing.T) {
	appDB, mock, cleanup := newUserCycleTestDB(t)
	defer cleanup()

	repo := NewUserCycleRepository(appDB, zap.NewNop())
	id := int64(1)
	delta := 300

	mock.ExpectExec(regexp.QuoteMeta(`
        UPDATE user_cycle_states
        SET used_seconds = LEAST(used_seconds + $1, total_seconds), updated_at = $2
        WHERE id = $3 AND used_seconds + $4 <= total_seconds
    `)).
		WithArgs(delta, sqlmock.AnyArg(), id, delta).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.IncrementUsage(context.Background(), id, delta)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserCycleRepository_IncrementSummaryUsage(t *testing.T) {
	appDB, mock, cleanup := newUserCycleTestDB(t)
	defer cleanup()

	repo := NewUserCycleRepository(appDB, zap.NewNop())
	id := int64(2)
	delta := 1

	mock.ExpectExec(regexp.QuoteMeta(`
        UPDATE user_cycle_states
        SET summary_used = LEAST(summary_used + $1, summary_total), updated_at = $2
        WHERE id = $3 AND summary_used + $4 <= summary_total
    `)).
		WithArgs(delta, sqlmock.AnyArg(), id, delta).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.IncrementSummaryUsage(context.Background(), id, delta)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserCycleRepository_UpdateState(t *testing.T) {
	appDB, mock, cleanup := newUserCycleTestDB(t)
	defer cleanup()

	repo := NewUserCycleRepository(appDB, zap.NewNop())
	id := int64(3)
	from := model.CycleStateActive
	to := model.CycleStateCompleted

	mock.ExpectExec(regexp.QuoteMeta(`
        UPDATE user_cycle_states
        SET state = $1, updated_at = $2
        WHERE id = $3 AND state = $4
    `)).
		WithArgs(string(to), sqlmock.AnyArg(), id, string(from)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateState(context.Background(), id, from, to)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserCycleRepository_UpdatePeriod(t *testing.T) {
	appDB, mock, cleanup := newUserCycleTestDB(t)
	defer cleanup()

	repo := NewUserCycleRepository(appDB, zap.NewNop())
	id := int64(4)
	start := int64(100)
	end := int64(200)

	mock.ExpectExec(regexp.QuoteMeta(`
        UPDATE user_cycle_states
        SET period_start = $1, period_end = $2, updated_at = $3
        WHERE id = $4
    `)).
		WithArgs(start, end, sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdatePeriod(context.Background(), id, &start, &end)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserCycleRepository_ListByUserAndStates(t *testing.T) {
	appDB, mock, cleanup := newUserCycleTestDB(t)
	defer cleanup()

	repo := NewUserCycleRepository(appDB, zap.NewNop())
	userID := 1
	states := []model.CycleState{model.CycleStateActive, model.CycleStateCompleted}
	now := time.Now().Unix()

	rows := sqlmock.NewRows([]string{
		"id", "user_id", "tier", "state", "origin_type", "origin_id",
		"cycle_no", "period_start", "period_end", "total_seconds", "used_seconds",
		"summary_total", "summary_used", "created_at", "updated_at",
	}).AddRow(
		int64(10), userID, string(model.AccountRolePremium), string(model.CycleStateActive),
		string(model.CycleOriginSubscription), int64(99),
		2, now-7200, now+7200, 3600, 1800, 20, 5, now-86400, now-3600,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
        SELECT id, user_id, tier, state, origin_type, origin_id,
               cycle_no, period_start, period_end, total_seconds, used_seconds,
               summary_total, summary_used,
               created_at, updated_at
        FROM user_cycle_states
        WHERE user_id = $1 AND state IN ($2,$3)
        ORDER BY period_start ASC
    `)).WithArgs(userID, string(states[0]), string(states[1])).WillReturnRows(rows)

	result, err := repo.ListByUserAndStates(context.Background(), userID, states)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, userID, result[0].UserID)
	require.Equal(t, model.AccountRolePremium, result[0].Tier)
	require.Equal(t, model.CycleStateActive, result[0].State)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserCycleRepository_ListEffectiveByUserAndStates(t *testing.T) {
	appDB, mock, cleanup := newUserCycleTestDB(t)
	defer cleanup()

	repo := NewUserCycleRepository(appDB, zap.NewNop())
	userID := 11
	states := []model.CycleState{model.CycleStateActive, model.CycleStateCompleted}
	now := time.Now().Unix()

	rows := sqlmock.NewRows([]string{
		"id", "user_id", "tier", "state", "origin_type", "origin_id",
		"cycle_no", "period_start", "period_end", "total_seconds", "used_seconds",
		"summary_total", "summary_used", "created_at", "updated_at",
	}).AddRow(
		int64(20), userID, string(model.AccountRolePremium), string(model.CycleStateActive),
		string(model.CycleOriginSubscription), int64(199),
		1, now-1200, now+1200, 3600, 10, 20, 1, now-86400, now-3600,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, tier, state, origin_type, origin_id,
		       cycle_no, period_start, period_end, total_seconds, used_seconds,
		       summary_total, summary_used,
		       created_at, updated_at
		FROM user_cycle_states
		WHERE user_id = $1
		  AND state IN ($2,$3)
		  AND (period_start IS NULL OR period_start <= $4)
		  AND (period_end IS NULL OR period_end > $5)
		ORDER BY (period_end IS NOT NULL) ASC, period_end ASC, created_at ASC, id ASC
	`)).
		WithArgs(userID, string(states[0]), string(states[1]), now, now).
		WillReturnRows(rows)

	result, err := repo.ListEffectiveByUserAndStates(context.Background(), userID, states, now)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, userID, result[0].UserID)
	require.Equal(t, model.AccountRolePremium, result[0].Tier)
	require.Equal(t, model.CycleStateActive, result[0].State)
	require.NotNil(t, result[0].OriginID)
	require.Equal(t, int64(199), *result[0].OriginID)
	require.NotNil(t, result[0].PeriodStart)
	require.NotNil(t, result[0].PeriodEnd)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserCycleRepository_ListByTierAndStates(t *testing.T) {
	appDB, mock, cleanup := newUserCycleTestDB(t)
	defer cleanup()

	repo := NewUserCycleRepository(appDB, zap.NewNop())
	userID := 2
	tier := model.AccountRolePro
	states := []model.CycleState{model.CycleStateActive, model.CycleStateCompleted}
	now := time.Now().Unix()

	rows := sqlmock.NewRows([]string{
		"id", "user_id", "tier", "state", "origin_type", "origin_id",
		"cycle_no", "period_start", "period_end", "total_seconds", "used_seconds",
		"summary_total", "summary_used", "created_at", "updated_at",
	}).AddRow(
		int64(30), userID, string(tier), string(model.CycleStateCompleted),
		string(model.CycleOriginSubscription), int64(300),
		3, now-7200, now+7200, 3600, 100, 10, 2, now-86400, now-3600,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, tier, state, origin_type, origin_id,
		       cycle_no, period_start, period_end, total_seconds, used_seconds,
		       summary_total, summary_used,
		       created_at, updated_at
		FROM user_cycle_states
		WHERE user_id = $1
		  AND tier = $2
		  AND state IN ($3,$4)
		ORDER BY period_start ASC
	`)).
		WithArgs(userID, string(tier), string(states[0]), string(states[1])).
		WillReturnRows(rows)

	result, err := repo.ListByTierAndStates(context.Background(), userID, tier, states)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, userID, result[0].UserID)
	require.Equal(t, tier, result[0].Tier)
	require.Equal(t, model.CycleStateCompleted, result[0].State)
	require.NotNil(t, result[0].OriginID)
	require.Equal(t, int64(300), *result[0].OriginID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserCycleRepository_ListConsumable(t *testing.T) {
	appDB, mock, cleanup := newUserCycleTestDB(t)
	defer cleanup()

	repo := NewUserCycleRepository(appDB, zap.NewNop())
	userID := 9
	now := time.Now().Unix()

	rows := sqlmock.NewRows([]string{
		"id", "user_id", "tier", "state", "origin_type", "origin_id",
		"cycle_no", "period_start", "period_end", "total_seconds", "used_seconds",
		"summary_total", "summary_used", "created_at", "updated_at",
	}).AddRow(
		int64(1), userID, string(model.AccountRolePayg), string(model.CycleStateActive),
		string(model.CycleOriginSubscription), int64(10),
		1, now-10, now+10, 100, 0, 0, 0, now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, tier, state, origin_type, origin_id,
		       cycle_no, period_start, period_end, total_seconds, used_seconds,
		       summary_total, summary_used,
		       created_at, updated_at
		FROM user_cycle_states
		WHERE user_id = $1
		  AND state = $2
		  AND (period_start IS NULL OR period_start <= $3)
		  AND (period_end IS NULL OR period_end > $4)
		ORDER BY (period_end IS NOT NULL) ASC, period_end ASC, created_at ASC, id ASC
	`)).WithArgs(userID, string(model.CycleStateActive), sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnRows(rows)

	got, err := repo.ListConsumable(context.Background(), userID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, model.AccountRolePayg, got[0].Tier)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserCycleRepository_ListActiveByPremium(t *testing.T) {
	appDB, mock, cleanup := newUserCycleTestDB(t)
	defer cleanup()

	repo := NewUserCycleRepository(appDB, zap.NewNop())
	userID := 3
	subID := int64(42)
	states := []model.CycleState{model.CycleStateActive, model.CycleStateCompleted}
	now := time.Now().Unix()

	rows := sqlmock.NewRows([]string{
		"id", "user_id", "tier", "state", "origin_type", "origin_id",
		"cycle_no", "period_start", "period_end", "total_seconds", "used_seconds",
		"created_at", "updated_at",
	}).AddRow(
		int64(7), userID, string(model.AccountRolePremium), string(model.CycleStateActive),
		string(model.CycleOriginSubscription), subID,
		1, now-10, now+10, 100, 0, now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, tier, state, origin_type, origin_id,
		       cycle_no, period_start, period_end, total_seconds, used_seconds,
		       created_at, updated_at
		FROM user_cycle_states
		WHERE user_id = $1
		  AND tier = $2
		  AND state IN ($3,$4)
		  AND origin_type = $5
		  AND origin_id = $6
		ORDER BY period_start ASC
	`)).
		WithArgs(userID, string(model.AccountRolePremium), string(states[0]), string(states[1]), string(model.CycleOriginSubscription), subID).
		WillReturnRows(rows)

	got, err := repo.ListActiveByPremium(context.Background(), userID, states, subID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].OriginID)
	require.Equal(t, subID, *got[0].OriginID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserCycleRepository_ListAllActiveExpired(t *testing.T) {
	appDB, mock, cleanup := newUserCycleTestDB(t)
	defer cleanup()

	repo := NewUserCycleRepository(appDB, zap.NewNop())
	now := time.Now().Unix()

	rows := sqlmock.NewRows([]string{
		"id", "user_id", "tier", "state", "origin_type", "origin_id",
		"cycle_no", "period_start", "period_end", "total_seconds", "used_seconds",
		"summary_total", "summary_used", "created_at", "updated_at",
	}).AddRow(
		int64(7), int64(2), string(model.AccountRolePayg), string(model.CycleStateActive),
		string(model.CycleOriginSubscription), int64(101),
		1, now-7200, now-10, 3600, 3600, 10, 10, now-86400, now-10,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
        SELECT id, user_id, tier, state, origin_type, origin_id,
               cycle_no, period_start, period_end, total_seconds, used_seconds,
               summary_total, summary_used,
               created_at, updated_at
        FROM user_cycle_states
        WHERE state IN ($1, $2)
          AND period_end IS NOT NULL
          AND period_end <= $3
        ORDER BY period_end ASC
    `)).WithArgs(string(model.CycleStateActive), string(model.CycleStateCompleted), now).WillReturnRows(rows)

	result, err := repo.ListAllActiveExpired(context.Background(), now)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, int64(2), int64(result[0].UserID))
	require.Equal(t, model.AccountRolePayg, result[0].Tier)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserCycleRepository_FindLatestEndedPaidCycle(t *testing.T) {
	appDB, mock, cleanup := newUserCycleTestDB(t)
	defer cleanup()

	repo := NewUserCycleRepository(appDB, zap.NewNop())
	userID := 3
	now := time.Now().Unix()

	rows := sqlmock.NewRows([]string{
		"id", "user_id", "tier", "state", "origin_type", "origin_id",
		"cycle_no", "period_start", "period_end", "total_seconds", "used_seconds",
		"summary_total", "summary_used", "created_at", "updated_at",
	}).AddRow(
		int64(12), userID, string(model.AccountRolePro), string(model.CycleStateCompleted),
		string(model.CycleOriginSubscription), int64(201),
		3, now-7200, now-1800, 3600, 3600, 30, 30, now-86400, now-1800,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
        SELECT id, user_id, tier, state, origin_type, origin_id,
               cycle_no, period_start, period_end, total_seconds, used_seconds,
               summary_total, summary_used,
               created_at, updated_at
        FROM user_cycle_states
        WHERE user_id = $1
          AND tier IN ($2, $3, $4)
          AND state IN ($5, $6, $7)
          AND period_end IS NOT NULL
        ORDER BY period_end DESC, created_at DESC
        LIMIT 1
    `)).
		WithArgs(
			userID,
			string(model.AccountRolePayg), string(model.AccountRolePremium), string(model.AccountRolePro),
			string(model.CycleStateActive), string(model.CycleStateCompleted), string(model.CycleStateExpired),
		).
		WillReturnRows(rows)

	got, err := repo.FindLatestEndedPaidCycle(context.Background(), userID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, userID, got.UserID)
	require.Equal(t, model.AccountRolePro, got.Tier)
	require.Equal(t, model.CycleStateCompleted, got.State)
	require.NoError(t, mock.ExpectationsWereMet())
}
