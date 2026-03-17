package repository

import (
	"context"
	"database/sql"
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

func newSubscriptionTestDB(t *testing.T) (*database.DB, sqlmock.Sqlmock, func()) {
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

func ptrString(v string) *string {
	return &v
}

func TestSubscriptionRepository_CreateAndGet(t *testing.T) {
	appDB, mock, cleanup := newSubscriptionTestDB(t)
	defer cleanup()

	repo := NewSubscriptionRepository(appDB)
	now := time.Now().Unix()
	next := now + 3600
	expires := now + 7200

	sub := &model.Subscription{
		UserID:             1,
		Provider:           "apple",
		ProductType:        model.SubscriptionProductYearPro,
		OriginalTxnID:      "orig-1",
		LatestTxnID:        ptrString("latest-1"),
		Status:             model.SubscriptionStatusActive,
		RenewalState:       model.SubscriptionRenewalOn,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now + 1800,
		NextGrantAt:        &next,
		ExpiresAt:          &expires,
		PeriodsGranted:     2,
		PeriodsConsumed:    1,
	}

	mock.ExpectQuery(regexp.QuoteMeta(`
        INSERT INTO subscriptions (
            user_id, provider, product_type, original_txn_id, latest_txn_id,
            status, renewal_state, current_period_start, current_period_end,
            next_grant_at, expires_at, periods_granted, periods_consumed,
            created_at, updated_at
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
        RETURNING id
    `)).
		WithArgs(
			sub.UserID,
			sub.Provider,
			sub.ProductType,
			sub.OriginalTxnID,
			*sub.LatestTxnID,
			sub.Status,
			sub.RenewalState,
			sub.CurrentPeriodStart,
			sub.CurrentPeriodEnd,
			*sub.NextGrantAt,
			*sub.ExpiresAt,
			sub.PeriodsGranted,
			sub.PeriodsConsumed,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(101)))

	mock.ExpectQuery(regexp.QuoteMeta(`
        SELECT id, user_id, provider, product_type, original_txn_id, latest_txn_id,
            status, renewal_state, current_period_start, current_period_end,
            next_grant_at, expires_at, periods_granted, periods_consumed, created_at, updated_at
        FROM subscriptions
        WHERE id = $1
        LIMIT 1
    `)).
		WithArgs(int64(101)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "provider", "product_type", "original_txn_id", "latest_txn_id",
			"status", "renewal_state", "current_period_start", "current_period_end",
			"next_grant_at", "expires_at", "periods_granted", "periods_consumed", "created_at", "updated_at",
		}).AddRow(
			int64(101), sub.UserID, sub.Provider, sub.ProductType, sub.OriginalTxnID, *sub.LatestTxnID,
			sub.Status, sub.RenewalState, sub.CurrentPeriodStart, sub.CurrentPeriodEnd,
			*sub.NextGrantAt, *sub.ExpiresAt, sub.PeriodsGranted, sub.PeriodsConsumed, now, now,
		))

	err := repo.Create(context.Background(), sub)
	require.NoError(t, err)
	require.Equal(t, int64(101), sub.ID)

	fetched, err := repo.GetByID(context.Background(), sub.ID)
	require.NoError(t, err)
	require.Equal(t, sub.ID, fetched.ID)
	require.Equal(t, *sub.LatestTxnID, *fetched.LatestTxnID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionRepository_UpdateMutations(t *testing.T) {
	appDB, mock, cleanup := newSubscriptionTestDB(t)
	defer cleanup()

	repo := NewSubscriptionRepository(appDB)
	id := int64(9)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE subscriptions
        SET product_type = $1, updated_at = $2
        WHERE id = $3`)).
		WithArgs(model.SubscriptionProductYearSub, sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE subscriptions
        SET status = $1, updated_at = $2
        WHERE id = $3`)).
		WithArgs(model.SubscriptionStatusExpired, sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE subscriptions
        SET renewal_state = $1, updated_at = $2
        WHERE id = $3`)).
		WithArgs(model.SubscriptionRenewalOff, sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE subscriptions
        SET latest_txn_id = $1, updated_at = $2
        WHERE id = $3`)).
		WithArgs("latest-2", sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE subscriptions
        SET next_grant_at = $1, periods_granted = $2, updated_at = $3
        WHERE id = $4`)).
		WithArgs(int64(1234), 5, sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE subscriptions
        SET current_period_start = $1,
            current_period_end = $2,
            next_grant_at = $3,
            periods_granted = $4,
            periods_consumed = $5,
            updated_at = $6
        WHERE id = $7`)).
		WithArgs(int64(1), int64(2), nil, 3, 4, sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE subscriptions
        SET next_grant_at = NULL, updated_at = $1
        WHERE id = $2`)).
		WithArgs(sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE subscriptions
        SET expires_at = $1, updated_at = $2
        WHERE id = $3`)).
		WithArgs(int64(999), sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE subscriptions
        SET periods_consumed = periods_consumed + $1, updated_at = $2
        WHERE id = $3`)).
		WithArgs(1, sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.UpdateProductType(context.Background(), id, model.SubscriptionProductYearSub))
	require.NoError(t, repo.UpdateStatus(context.Background(), id, model.SubscriptionStatusExpired))
	require.NoError(t, repo.UpdateRenewalState(context.Background(), id, model.SubscriptionRenewalOff))
	require.NoError(t, repo.UpdateLatestTxn(context.Background(), id, "latest-2"))
	require.NoError(t, repo.UpdateNextGrant(context.Background(), id, 1234, 5))
	require.NoError(t, repo.UpdatePeriodWindow(context.Background(), id, 1, 2, nil, 3, 4))
	require.NoError(t, repo.ClearNextGrantAt(context.Background(), id))
	require.NoError(t, repo.UpdateExpiresAt(context.Background(), id, 999))
	require.NoError(t, repo.IncrementConsumed(context.Background(), id, 1))

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionRepository_ListDueForGrant(t *testing.T) {
	appDB, mock, cleanup := newSubscriptionTestDB(t)
	defer cleanup()

	repo := NewSubscriptionRepository(appDB)
	cutoff := time.Now().Unix()
	limit := 2

	rows := sqlmock.NewRows([]string{
		"id", "user_id", "provider", "product_type", "original_txn_id", "latest_txn_id",
		"status", "renewal_state", "current_period_start", "current_period_end",
		"next_grant_at", "expires_at", "periods_granted", "periods_consumed", "created_at", "updated_at",
	}).AddRow(
		int64(1), int64(10), "apple", model.SubscriptionProductYearPro, "orig-1", "latest-1",
		model.SubscriptionStatusActive, model.SubscriptionRenewalOn, cutoff-100, cutoff+100,
		cutoff, nil, 3, 1, cutoff-200, cutoff-100,
	).AddRow(
		int64(2), int64(11), "apple", model.SubscriptionProductYearPro, "orig-2", nil,
		model.SubscriptionStatusActive, model.SubscriptionRenewalOn, cutoff-200, cutoff+200,
		cutoff-10, cutoff+500, 4, 2, cutoff-300, cutoff-50,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
        SELECT id, user_id, provider, product_type, original_txn_id, latest_txn_id,
            status, renewal_state, current_period_start, current_period_end,
            next_grant_at, expires_at, periods_granted, periods_consumed, created_at, updated_at
        FROM subscriptions
        WHERE next_grant_at IS NOT NULL
            AND next_grant_at <= $1
            AND status = 'ACTIVE'
        ORDER BY next_grant_at ASC
        LIMIT $2
    `)).WithArgs(cutoff, limit).WillReturnRows(rows)

	result, err := repo.ListDueForGrant(context.Background(), cutoff, limit)
	require.NoError(t, err)
	require.Len(t, result, 2)
	require.Equal(t, int64(1), result[0].ID)
	require.Nil(t, result[0].ExpiresAt)
	require.NotNil(t, result[1].ExpiresAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionRepository_GetByOriginalTxn_NoRows(t *testing.T) {
	appDB, mock, cleanup := newSubscriptionTestDB(t)
	defer cleanup()

	repo := NewSubscriptionRepository(appDB)
	provider := "apple"
	orig := "missing"

	mock.ExpectQuery(regexp.QuoteMeta(`
        SELECT id, user_id, provider, product_type, original_txn_id, latest_txn_id,
            status, renewal_state, current_period_start, current_period_end,
            next_grant_at, expires_at, periods_granted, periods_consumed, created_at, updated_at
        FROM subscriptions
        WHERE provider = $1 AND original_txn_id = $2
        LIMIT 1
    `)).WithArgs(provider, orig).WillReturnError(sql.ErrNoRows)

	got, err := repo.GetByOriginalTxn(context.Background(), provider, orig)
	require.NoError(t, err)
	require.Nil(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}
