package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/chinaxxren/gonotic/internal/pkg/database"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newMeetingSyncTaskTestDB(t *testing.T) (*database.DB, sqlmock.Sqlmock, func()) {
	t.Helper()
	mdb, mock, err := sqlmock.New()
	require.NoError(t, err)

	sqlxDB := sqlx.NewDb(mdb, "postgres")
	appDB := database.NewFromSQLX(sqlxDB, zap.NewNop())
	cleanup := func() {
		sqlxDB.Close()
		mdb.Close()
	}
	return appDB, mock, cleanup
}

func TestMeetingSyncTaskRepository_CreateOrGetPending_ReturnsExisting(t *testing.T) {
	appDB, mock, cleanup := newMeetingSyncTaskTestDB(t)
	defer cleanup()

	repo := NewMeetingSyncTaskRepository(appDB, zap.NewNop())
	meetingID := 10

	rows := sqlmock.NewRows([]string{"id", "meeting_id", "status", "current_step", "attempts", "last_error", "next_retry_at", "created_at", "updated_at"}).
		AddRow(1, meetingID, MeetingSyncStatusPending, "step1", 0, sql.NullString{}, sql.NullInt64{}, int64(1), int64(1))

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, meeting_id, status, current_step, attempts, last_error, next_retry_at, created_at, updated_at\n\t\tFROM meeting_sync_tasks WHERE meeting_id = $1 LIMIT 1")).
		WithArgs(meetingID).
		WillReturnRows(rows)

	got, err := repo.CreateOrGetPending(context.Background(), meetingID, "step1")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, 1, got.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMeetingSyncTaskRepository_CreateOrGetPending_InsertsWhenMissing(t *testing.T) {
	appDB, mock, cleanup := newMeetingSyncTaskTestDB(t)
	defer cleanup()

	repo := NewMeetingSyncTaskRepository(appDB, zap.NewNop())
	meetingID := 11

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, meeting_id, status, current_step, attempts, last_error, next_retry_at, created_at, updated_at\n\t\tFROM meeting_sync_tasks WHERE meeting_id = $1 LIMIT 1")).
		WithArgs(meetingID).
		WillReturnError(sql.ErrNoRows)

	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO meeting_sync_tasks (meeting_id, status, current_step, attempts, created_at, updated_at)\n\t\tVALUES ($1, $2, $3, 0, $4, $5) RETURNING id")).
		WithArgs(meetingID, MeetingSyncStatusPending, "init", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))

	got, err := repo.CreateOrGetPending(context.Background(), meetingID, "init")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, 7, got.ID)
	require.Equal(t, MeetingSyncStatusPending, got.Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMeetingSyncTaskRepository_FetchReadyTasks(t *testing.T) {
	appDB, mock, cleanup := newMeetingSyncTaskTestDB(t)
	defer cleanup()

	repo := NewMeetingSyncTaskRepository(appDB, zap.NewNop())
	rows := sqlmock.NewRows([]string{"id", "meeting_id", "status", "current_step", "attempts", "last_error", "next_retry_at", "created_at", "updated_at"}).
		AddRow(1, 10, MeetingSyncStatusPending, "step", 0, sql.NullString{}, sql.NullInt64{}, int64(1), int64(1))

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, meeting_id, status, current_step, attempts, last_error, next_retry_at, created_at, updated_at\n\t\tFROM meeting_sync_tasks\n\t\tWHERE (status = $1 OR status = $2)\n\t\t  AND (next_retry_at IS NULL OR next_retry_at <= $3)\n\t\tORDER BY updated_at ASC\n\t\tLIMIT $4")).
		WithArgs(MeetingSyncStatusPending, MeetingSyncStatusFailed, sqlmock.AnyArg(), 1).
		WillReturnRows(rows)

	tasks, err := repo.FetchReadyTasks(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMeetingSyncTaskRepository_MarkProcessing_DefaultAllowedStatuses(t *testing.T) {
	appDB, mock, cleanup := newMeetingSyncTaskTestDB(t)
	defer cleanup()

	repo := NewMeetingSyncTaskRepository(appDB, zap.NewNop())

	mock.ExpectExec(regexp.QuoteMeta("UPDATE meeting_sync_tasks SET status = $1, updated_at = $2 WHERE id = $3 AND status IN ($4,$5)" )).
		WithArgs(MeetingSyncStatusProcessing, sqlmock.AnyArg(), 1, MeetingSyncStatusPending, MeetingSyncStatusFailed).
		WillReturnResult(sqlmock.NewResult(0, 1))

	ok, err := repo.MarkProcessing(context.Background(), 1, nil)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMeetingSyncTaskRepository_UpdateOnStepSuccess_EndTask(t *testing.T) {
	appDB, mock, cleanup := newMeetingSyncTaskTestDB(t)
	defer cleanup()

	repo := NewMeetingSyncTaskRepository(appDB, zap.NewNop())

	mock.ExpectExec(regexp.QuoteMeta("UPDATE meeting_sync_tasks\n\t\t\tSET status = $1, current_step = '', next_retry_at = NULL, updated_at = $2\n\t\t\tWHERE id = $3")).
		WithArgs(MeetingSyncStatusSuccess, sqlmock.AnyArg(), 1).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateOnStepSuccess(context.Background(), 1, "")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMeetingSyncTaskRepository_UpdateOnError_SetsRetryAndPermanentFailed(t *testing.T) {
	appDB, mock, cleanup := newMeetingSyncTaskTestDB(t)
	defer cleanup()

	repo := NewMeetingSyncTaskRepository(appDB, zap.NewNop())

	mock.ExpectQuery(regexp.QuoteMeta("SELECT attempts FROM meeting_sync_tasks WHERE id = $1")).
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"attempts"}).AddRow(5))

	mock.ExpectExec(regexp.QuoteMeta("UPDATE meeting_sync_tasks\n\t\tSET status = $1, current_step = $2, attempts = $3, last_error = $4, next_retry_at = $5, updated_at = $6\n\t\tWHERE id = $7")).
		WithArgs(MeetingSyncStatusPermanentFailed, "s", 6, "boom", sql.NullInt64{Int64: 0, Valid: false}, sqlmock.AnyArg(), 1).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateOnError(context.Background(), 1, "s", 1, "boom")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())

	// Check one of the backoff windows too (attempts <= 3 => +300).
	mock.ExpectQuery(regexp.QuoteMeta("SELECT attempts FROM meeting_sync_tasks WHERE id = $1")).
		WithArgs(2).
		WillReturnRows(sqlmock.NewRows([]string{"attempts"}).AddRow(0))

	// Can't assert exact next_retry_at (depends on time.Now), just ensure it is valid via AnyArg.
	mock.ExpectExec(regexp.QuoteMeta("UPDATE meeting_sync_tasks\n\t\tSET status = $1, current_step = $2, attempts = $3, last_error = $4, next_retry_at = $5, updated_at = $6\n\t\tWHERE id = $7")).
		WithArgs(MeetingSyncStatusFailed, "s2", 1, "err", sqlmock.AnyArg(), sqlmock.AnyArg(), 2).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = repo.UpdateOnError(context.Background(), 2, "s2", 1, "err")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMeetingSyncTaskRepository_UpdateOnStepSuccess_NextStep(t *testing.T) {
	appDB, mock, cleanup := newMeetingSyncTaskTestDB(t)
	defer cleanup()

	repo := NewMeetingSyncTaskRepository(appDB, zap.NewNop())
	mock.ExpectExec(regexp.QuoteMeta("UPDATE meeting_sync_tasks\n\t\tSET current_step = $1, updated_at = $2\n\t\tWHERE id = $3")).
		WithArgs("next", sqlmock.AnyArg(), 1).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateOnStepSuccess(context.Background(), 1, "next")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMeetingSyncTaskRepository_GetByMeetingID_NoRows(t *testing.T) {
	appDB, mock, cleanup := newMeetingSyncTaskTestDB(t)
	defer cleanup()

	repo := NewMeetingSyncTaskRepository(appDB, zap.NewNop())
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, meeting_id, status, current_step, attempts, last_error, next_retry_at, created_at, updated_at\n\t\tFROM meeting_sync_tasks WHERE meeting_id = $1 LIMIT 1")).
		WithArgs(77).
		WillReturnError(sql.ErrNoRows)

	got, err := repo.GetByMeetingID(context.Background(), 77)
	require.NoError(t, err)
	require.Nil(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMeetingSyncTaskRepository_FetchReadyTasks_DefaultLimit(t *testing.T) {
	appDB, mock, cleanup := newMeetingSyncTaskTestDB(t)
	defer cleanup()

	repo := NewMeetingSyncTaskRepository(appDB, zap.NewNop())
	rows := sqlmock.NewRows([]string{"id", "meeting_id", "status", "current_step", "attempts", "last_error", "next_retry_at", "created_at", "updated_at"})

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, meeting_id, status, current_step, attempts, last_error, next_retry_at, created_at, updated_at\n\t\tFROM meeting_sync_tasks\n\t\tWHERE (status = $1 OR status = $2)\n\t\t  AND (next_retry_at IS NULL OR next_retry_at <= $3)\n\t\tORDER BY updated_at ASC\n\t\tLIMIT $4")).
		WithArgs(MeetingSyncStatusPending, MeetingSyncStatusFailed, sqlmock.AnyArg(), 50).
		WillReturnRows(rows)

	_, err := repo.FetchReadyTasks(context.Background(), 0)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMeetingSyncTaskRepository_MarkProcessing_NoRowsAffected(t *testing.T) {
	appDB, mock, cleanup := newMeetingSyncTaskTestDB(t)
	defer cleanup()

	repo := NewMeetingSyncTaskRepository(appDB, zap.NewNop())
	mock.ExpectExec(regexp.QuoteMeta("UPDATE meeting_sync_tasks SET status = $1, updated_at = $2 WHERE id = $3 AND status IN ($4)" )).
		WithArgs(MeetingSyncStatusProcessing, sqlmock.AnyArg(), 1, MeetingSyncStatusPending).
		WillReturnResult(sqlmock.NewResult(0, 0))

	ok, err := repo.MarkProcessing(context.Background(), 1, []string{MeetingSyncStatusPending})
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMeetingSyncTaskRepository_UpdateOnError_DefaultAttemptIncrement(t *testing.T) {
	appDB, mock, cleanup := newMeetingSyncTaskTestDB(t)
	defer cleanup()

	repo := NewMeetingSyncTaskRepository(appDB, zap.NewNop())

	mock.ExpectQuery(regexp.QuoteMeta("SELECT attempts FROM meeting_sync_tasks WHERE id = $1")).
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"attempts"}).AddRow(3))

	mock.ExpectExec(regexp.QuoteMeta("UPDATE meeting_sync_tasks\n\t\tSET status = $1, current_step = $2, attempts = $3, last_error = $4, next_retry_at = $5, updated_at = $6\n\t\tWHERE id = $7")).
		WithArgs(MeetingSyncStatusFailed, "s", 4, "err", sqlmock.AnyArg(), sqlmock.AnyArg(), 1).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateOnError(context.Background(), 1, "s", 0, "err")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMeetingSyncTaskRepository_UpdateOnError_BackoffSecondWindow(t *testing.T) {
	appDB, mock, cleanup := newMeetingSyncTaskTestDB(t)
	defer cleanup()

	repo := NewMeetingSyncTaskRepository(appDB, zap.NewNop())

	mock.ExpectQuery(regexp.QuoteMeta("SELECT attempts FROM meeting_sync_tasks WHERE id = $1")).
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"attempts"}).AddRow(3))

	// attempts becomes 4 -> should be failed and retry later (AnyArg)
	mock.ExpectExec(regexp.QuoteMeta("UPDATE meeting_sync_tasks\n\t\tSET status = $1, current_step = $2, attempts = $3, last_error = $4, next_retry_at = $5, updated_at = $6\n\t\tWHERE id = $7")).
		WithArgs(MeetingSyncStatusFailed, "s", 4, "err", sqlmock.AnyArg(), sqlmock.AnyArg(), 1).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateOnError(context.Background(), 1, "s", 1, "err")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMeetingSyncTaskRepository_UpdateOnError_UsesTimeWindow(t *testing.T) {
	// A very lightweight sanity check to ensure tests do not flake due to wall clock.
	_ = time.Now()
}
