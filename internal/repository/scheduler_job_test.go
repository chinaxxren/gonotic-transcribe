package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/pkg/database"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newSchedulerJobTestDB(t *testing.T) (*database.DB, sqlmock.Sqlmock, func()) {
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

func TestSchedulerJobRepository_CreateJob(t *testing.T) {
	appDB, mock, cleanup := newSchedulerJobTestDB(t)
	defer cleanup()

	repo := NewSchedulerJobRepository(appDB)
	job := &model.SchedulerJob{JobType: "test", Status: model.SchedulerJobStatusPending}

	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO scheduler_jobs (job_type, status, started_at, finished_at, attempts, error_message, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`)).
		WithArgs(job.JobType, job.Status, job.StartedAt, job.FinishedAt, job.Attempts, job.ErrorMsg, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(9)))

	err := repo.CreateJob(context.Background(), job)
	require.NoError(t, err)
	require.Equal(t, int64(9), job.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSchedulerJobRepository_UpdateJobStatus(t *testing.T) {
	appDB, mock, cleanup := newSchedulerJobTestDB(t)
	defer cleanup()

	repo := NewSchedulerJobRepository(appDB)
	errMsg := "boom"

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE scheduler_jobs
		SET status = $1, error_message = $2, finished_at = $3, updated_at = $4
		WHERE id = $5
	`)).
		WithArgs(model.SchedulerJobStatusFailed, &errMsg, sqlmock.AnyArg(), sqlmock.AnyArg(), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateJobStatus(context.Background(), 1, model.SchedulerJobStatusFailed, &errMsg)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSchedulerJobRepository_CreateJobItems(t *testing.T) {
	appDB, mock, cleanup := newSchedulerJobTestDB(t)
	defer cleanup()

	repo := NewSchedulerJobRepository(appDB)

	items := []*model.SchedulerJobItem{{JobID: 1, ItemKey: "a", Status: "PENDING"}, {JobID: 1, ItemKey: "b", Status: "PENDING"}}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`
			INSERT INTO scheduler_job_items (job_id, item_key, status, retry_count, started_at, finished_at, error_message, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			RETURNING id
		`)).
		WithArgs(int64(1), "a", "PENDING", 0, (*int64)(nil), (*int64)(nil), (*string)(nil), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)))
	mock.ExpectQuery(regexp.QuoteMeta(`
			INSERT INTO scheduler_job_items (job_id, item_key, status, retry_count, started_at, finished_at, error_message, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			RETURNING id
		`)).
		WithArgs(int64(1), "b", "PENDING", 0, (*int64)(nil), (*int64)(nil), (*string)(nil), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(12)))
	mock.ExpectCommit()

	err := repo.CreateJobItems(context.Background(), items)
	require.NoError(t, err)
	require.Equal(t, int64(11), items[0].ID)
	require.Equal(t, int64(12), items[1].ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSchedulerJobRepository_FetchPendingItems(t *testing.T) {
	appDB, mock, cleanup := newSchedulerJobTestDB(t)
	defer cleanup()

	repo := NewSchedulerJobRepository(appDB)

	now := time.Now().Unix()
	rows := sqlmock.NewRows([]string{"id", "job_id", "item_key", "status", "retry_count", "started_at", "finished_at", "error_message", "created_at", "updated_at"}).
		AddRow(int64(1), int64(2), "k", "PENDING", 0, nil, nil, nil, now, now)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT i.id, i.job_id, i.item_key, i.status, i.retry_count, i.started_at, i.finished_at, i.error_message,
			i.created_at, i.updated_at
		FROM scheduler_job_items i
		JOIN scheduler_jobs j ON i.job_id = j.id
		WHERE j.job_type = $1 AND i.status IN ('PENDING','FAILED')
		ORDER BY i.created_at ASC
		LIMIT $2
	`)).
		WithArgs("type", 1).
		WillReturnRows(rows)

	items, err := repo.FetchPendingItems(context.Background(), "type", 1)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, int64(2), items[0].JobID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSchedulerJobRepository_UpdateJobItem(t *testing.T) {
	appDB, mock, cleanup := newSchedulerJobTestDB(t)
	defer cleanup()

	repo := NewSchedulerJobRepository(appDB)
	started := time.Now().Unix()
	finished := started + 1
	errMsg := "err"
	item := &model.SchedulerJobItem{ID: 7, Status: "FAILED", RetryCount: 1, StartedAt: &started, FinishedAt: &finished, ErrorMsg: &errMsg}

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE scheduler_job_items
		SET status = $1, retry_count = $2, started_at = $3, finished_at = $4, error_message = $5, updated_at = $6
		WHERE id = $7
	`)).
		WithArgs(item.Status, item.RetryCount, item.StartedAt, item.FinishedAt, item.ErrorMsg, sqlmock.AnyArg(), item.ID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateJobItem(context.Background(), item)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSchedulerJobRepository_CreateJobItems_EmptyNoop(t *testing.T) {
	appDB, mock, cleanup := newSchedulerJobTestDB(t)
	defer cleanup()

	repo := NewSchedulerJobRepository(appDB)
	err := repo.CreateJobItems(context.Background(), nil)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
