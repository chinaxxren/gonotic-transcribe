package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chinaxxren/gonotic/internal/pkg/database"
)

func newSummaryTemplateTestDB(t *testing.T) (*database.DB, sqlmock.Sqlmock, func()) {
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

func TestSummaryTemplateRepository_ListByIDs(t *testing.T) {
	appDB, mock, cleanup := newSummaryTemplateTestDB(t)
	defer cleanup()

	repo := NewSummaryTemplateRepository(appDB)
	ids := []int64{11, 22, 33}

	rows := sqlmock.NewRows([]string{
		"id", "group_id", "name", "intro", "description", "storage", "location", "template_type", "owner_id", "is_visible", "display_order", "created_at", "updated_at",
	}).
		AddRow(11, 1, "tpl-11", "intro", "desc-11", "local", "path-11", "public", 0, 1, 10, 1, 2).
		AddRow(22, 2, "tpl-22", "intro2", "desc-22", "oss", "key-22", "user", 100, 0, 20, 3, 4)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, group_id, name, intro, description, storage, location, template_type, owner_id, is_visible, display_order, created_at, updated_at
		FROM summary_templates
		WHERE id IN ($1,$2,$3)
		ORDER BY display_order ASC, id ASC
	`)).
		WithArgs(int64(11), int64(22), int64(33)).
		WillReturnRows(rows)

	result, err := repo.ListByIDs(context.Background(), ids)
	require.NoError(t, err)
	require.Len(t, result, 2)
	require.Equal(t, int64(11), result[0].ID)
	require.Equal(t, int64(22), result[1].ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSummaryTemplateRepository_CountByGroupIDs(t *testing.T) {
	appDB, mock, cleanup := newSummaryTemplateTestDB(t)
	defer cleanup()

	repo := NewSummaryTemplateRepository(appDB)
	groupIDs := []int64{1, 2}

	rows := sqlmock.NewRows([]string{"group_id", "cnt"}).
		AddRow(1, 3).
		AddRow(2, 5)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT group_id, COUNT(*) as cnt
		FROM summary_templates
		WHERE group_id IN ($1,$2) AND template_type <> 'user'
		GROUP BY group_id
	`)).
		WithArgs(int64(1), int64(2)).
		WillReturnRows(rows)

	result, err := repo.CountByGroupIDs(context.Background(), groupIDs)
	require.NoError(t, err)
	require.Equal(t, map[int64]int{
		1: 3,
		2: 5,
	}, result)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSummaryTemplateRepository_CountByGroupIDs_EmptyInput(t *testing.T) {
	appDB, _, cleanup := newSummaryTemplateTestDB(t)
	defer cleanup()

	repo := NewSummaryTemplateRepository(appDB)
	result, err := repo.CountByGroupIDs(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, result)
}

func TestSummaryTemplateRepository_ListByTemplateType_EmptyInput(t *testing.T) {
	appDB, _, cleanup := newSummaryTemplateTestDB(t)
	defer cleanup()

	repo := NewSummaryTemplateRepository(appDB)
	result, err := repo.ListByTemplateType(context.Background(), " ", 0)
	require.NoError(t, err)
	require.Empty(t, result)
}

func TestSummaryTemplateRepository_ListByTemplateType_WithLimit(t *testing.T) {
	appDB, mock, cleanup := newSummaryTemplateTestDB(t)
	defer cleanup()

	repo := NewSummaryTemplateRepository(appDB)

	rows := sqlmock.NewRows([]string{
		"id", "group_id", "name", "intro", "description", "storage", "location", "template_type", "owner_id",
		"is_visible", "display_order", "created_at", "updated_at",
	}).AddRow(
		int64(1), int64(2), "name", "intro", "desc", "local", "loc", "public", int64(0), 1, 10, int64(1), int64(2),
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, group_id, name, intro, description, storage, location, template_type, owner_id,
		       is_visible, display_order, created_at, updated_at
		FROM summary_templates
		WHERE template_type = $1
		ORDER BY display_order ASC, id ASC

		LIMIT 3
	`)).WithArgs("public").WillReturnRows(rows)

	result, err := repo.ListByTemplateType(context.Background(), "public", 3)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, int64(1), result[0].ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSummaryTemplateRepository_CountByTemplateType(t *testing.T) {
	appDB, mock, cleanup := newSummaryTemplateTestDB(t)
	defer cleanup()

	repo := NewSummaryTemplateRepository(appDB)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COUNT(*)
		FROM summary_templates
		WHERE template_type = $1
	`)).WithArgs("public").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	cnt, err := repo.CountByTemplateType(context.Background(), "public")
	require.NoError(t, err)
	require.Equal(t, 5, cnt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSummaryTemplateRepository_CountByTemplateType_EmptyInput(t *testing.T) {
	appDB, _, cleanup := newSummaryTemplateTestDB(t)
	defer cleanup()

	repo := NewSummaryTemplateRepository(appDB)
	cnt, err := repo.CountByTemplateType(context.Background(), " ")
	require.NoError(t, err)
	require.Equal(t, 0, cnt)
}

func TestSummaryTemplateRepository_UtilHelpers(t *testing.T) {
	var ptr *int64
	require.Nil(t, nullableInt64Value(ptr))
	v := int64(7)
	ptr = &v
	require.Equal(t, int64(7), nullableInt64Value(ptr))

	require.Nil(t, nullableString(""))
	require.Nil(t, nullableString(" \t\n"))
	require.Equal(t, "x", nullableString("x"))

	require.True(t, errorsIsNoRows(sql.ErrNoRows))
	require.False(t, errorsIsNoRows(sql.ErrTxDone))

	require.Equal(t, 1, boolToTinyInt(true))
	require.Equal(t, 0, boolToTinyInt(false))

	// sanity: ensure Visible scan target matches model expectation
	_ = model.SummaryTemplate{}
}
