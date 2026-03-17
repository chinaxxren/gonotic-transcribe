package repository

import (
	"context"
	"fmt"
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

func newTestDB(t *testing.T) (*database.DB, sqlmock.Sqlmock, func()) {
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

func TestNotificationInboxRepository_ClaimPendingEmpty(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	repo := NewNotificationInboxRepository(db)
	ctx := context.Background()

	selectRegex := regexp.MustCompile(`SELECT id, provider`)
	mock.ExpectBegin()
	mock.ExpectQuery(selectRegex.String()).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "provider", "original_transaction_id", "event_type", "sequence",
			"payload", "state", "retry_count", "available_at", "error_message",
			"created_at", "updated_at",
		}))
	mock.ExpectRollback()

	items, err := repo.ClaimPending(ctx, 5)
	require.NoError(t, err)
	require.Nil(t, items)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNotificationInboxRepository_Enqueue(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	repo := NewNotificationInboxRepository(db)
	ctx := context.Background()
	item := &model.NotificationInbox{
		Provider:              "apple",
		OriginalTransactionID: "orig-123",
		EventType:             "DID_RENEW",
		Sequence:              time.Now().Unix(),
		Payload:               []byte("payload"),
		State:                 model.NotificationStatePending,
		RetryCount:            0,
		AvailableAt:           time.Now().Unix(),
	}

	mock.ExpectQuery("INSERT INTO notification_inbox").
		WithArgs(
			item.Provider,
			item.OriginalTransactionID,
			item.EventType,
			item.Sequence,
			item.Payload,
			item.State,
			item.RetryCount,
			item.AvailableAt,
			item.ErrorMessage,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))

	err := repo.Enqueue(ctx, item)
	require.NoError(t, err)
	require.Equal(t, int64(42), item.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNotificationInboxRepository_ClaimPending(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	repo := NewNotificationInboxRepository(db)
	ctx := context.Background()
	now := time.Now().Unix()

	rows := sqlmock.NewRows([]string{
		"id", "provider", "original_transaction_id", "event_type", "sequence",
		"payload", "state", "retry_count", "available_at", "error_message",
		"created_at", "updated_at",
	}).
		AddRow(int64(1), "apple", "orig-1", "DID_RENEW", int64(1001), []byte("p1"), model.NotificationStatePending, 0, now, nil, now, now).
		AddRow(int64(2), "apple", "orig-2", "DID_RENEW", int64(1002), []byte("p2"), model.NotificationStateRetry, 1, now, nil, now, now)

	selectRegex := regexp.MustCompile(`SELECT id, provider, original_transaction_id, event_type, sequence,`)

	mock.ExpectBegin()
	mock.ExpectQuery(selectRegex.String()).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(rows)

	mock.ExpectExec("UPDATE notification_inbox SET state = 'PROCESSING'").
		WithArgs(sqlmock.AnyArg(), int64(1), int64(2)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	items, err := repo.ClaimPending(ctx, 2)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, int64(1), items[0].ID)
	require.Equal(t, model.NotificationStatePending, items[0].State)
	require.Equal(t, model.NotificationStateRetry, items[1].State)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNotificationInboxRepository_MarkRetryAndDead(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	repo := NewNotificationInboxRepository(db)
	ctx := context.Background()

	mock.ExpectExec("UPDATE notification_inbox SET state = 'RETRY_PENDING'").
		WithArgs(sqlmock.AnyArg(), "boom", sqlmock.AnyArg(), int64(10)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec("UPDATE notification_inbox SET state = 'DEAD'").
		WithArgs("fatal", sqlmock.AnyArg(), int64(10)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.MarkRetry(ctx, 10, 30, "boom"))
	require.NoError(t, repo.MarkDead(ctx, 10, "fatal"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNotificationInboxRepository_MarkRetryError(t *testing.T) {
	db, mock, cleanup := newTestDB(t)
	defer cleanup()

	repo := NewNotificationInboxRepository(db)
	ctx := context.Background()

	mock.ExpectExec("UPDATE notification_inbox SET state = 'RETRY_PENDING'").
		WithArgs(sqlmock.AnyArg(), "oops", sqlmock.AnyArg(), int64(20)).
		WillReturnError(fmt.Errorf("update failed"))

	err := repo.MarkRetry(ctx, 20, 15, "oops")
	require.Error(t, err)
	require.Contains(t, err.Error(), "update failed")
	require.NoError(t, mock.ExpectationsWereMet())
}
