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

func newSummaryLedgerTestDB(t *testing.T) (*database.DB, sqlmock.Sqlmock, func()) {
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

func TestSummaryLedgerRepository_Create(t *testing.T) {
	appDB, mock, cleanup := newSummaryLedgerTestDB(t)
	defer cleanup()

	repo := NewSummaryLedgerRepository(appDB)
	entry := &model.SummaryLedger{
		UserID:       101,
		BusinessID:   202,
		CycleID:      303,
		TemplateID:   ptrInt64(404),
		SummaryDelta: 2,
		Source:       "unit-test",
	}

	mock.ExpectQuery("INSERT INTO summary_ledger").
		WithArgs(entry.UserID, entry.BusinessID, entry.CycleID, *entry.TemplateID, entry.SummaryDelta, entry.SummaryKey, entry.Source, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))

	err := repo.Create(context.Background(), entry)
	require.NoError(t, err)
	require.Equal(t, int64(1), entry.ID)
	require.NotZero(t, entry.CreatedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSummaryLedgerRepository_ListRecentTemplates(t *testing.T) {
	appDB, mock, cleanup := newSummaryLedgerTestDB(t)
	defer cleanup()

	repo := NewSummaryLedgerRepository(appDB)
	userID := 999
	limit := 3
	now := time.Now().Unix()

	rows := sqlmock.NewRows([]string{"template_id", "used_times", "last_used"}).
		AddRow(10, 4, now).    // accumulated delta = 4
		AddRow(20, 2, now-10). // smaller last_used
		AddRow(nil, 1, now-20) // should be skipped

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT template_id, SUM(summary_delta) AS used_times, MAX(created_at) as last_used
		FROM summary_ledger
		WHERE user_id = $1 AND template_id IS NOT NULL
		GROUP BY template_id
		ORDER BY last_used DESC
		LIMIT $2
	`)).
		WithArgs(userID, limit).
		WillReturnRows(rows)

	result, err := repo.ListRecentTemplates(context.Background(), userID, limit)
	require.NoError(t, err)
	require.Len(t, result, 2, "nil template row should be skipped")

	require.Equal(t, int64(10), result[0].TemplateID)
	require.Equal(t, 4, result[0].UsedTimes)
	require.Equal(t, now, result[0].UpdatedAt)

	require.Equal(t, int64(20), result[1].TemplateID)
	require.Equal(t, 2, result[1].UsedTimes)
	require.Equal(t, now-10, result[1].UpdatedAt)

	require.NoError(t, mock.ExpectationsWereMet())
}

func ptrInt64(v int64) *int64 {
	return &v
}
