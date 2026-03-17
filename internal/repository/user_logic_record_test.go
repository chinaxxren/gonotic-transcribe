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

func newUserLogicRecordTestDB(t *testing.T) (*database.DB, sqlmock.Sqlmock, func()) {
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

func TestUserLogicRecordRepository_GetByUserID_NoRows(t *testing.T) {
	appDB, mock, cleanup := newUserLogicRecordTestDB(t)
	defer cleanup()

	repo := NewUserLogicRecordRepository(appDB)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT user_id, next_free_grant_at,\n            created_at,\n            updated_at\n        FROM user_logic_record\n        WHERE user_id = $1\n        LIMIT 1" )).
		WithArgs(1).
		WillReturnError(sql.ErrNoRows)

	got, err := repo.GetByUserID(context.Background(), 1)
	require.NoError(t, err)
	require.Nil(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserLogicRecordRepository_UpsertNextFreeGrant_Nullable(t *testing.T) {
	appDB, mock, cleanup := newUserLogicRecordTestDB(t)
	defer cleanup()

	repo := NewUserLogicRecordRepository(appDB)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO user_logic_record (\n            user_id,\n            next_free_grant_at,\n            created_at,\n            updated_at\n        ) VALUES ($1, $2, $3, $4)\n        ON CONFLICT (user_id) DO UPDATE SET\n            next_free_grant_at = EXCLUDED.next_free_grant_at,\n            updated_at = EXCLUDED.updated_at" )).
		WithArgs(1, nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpsertNextFreeGrant(context.Background(), 1, nil)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserLogicRecordRepository_ListDueFreeGrantUsers_DefaultLimit(t *testing.T) {
	appDB, mock, cleanup := newUserLogicRecordTestDB(t)
	defer cleanup()

	repo := NewUserLogicRecordRepository(appDB)
	cutoff := time.Now().Unix()
	rows := sqlmock.NewRows([]string{"user_id", "next_free_grant_at", "created_at", "updated_at"}).
		AddRow(1, cutoff, int64(1), int64(2))

	mock.ExpectQuery(regexp.QuoteMeta("SELECT user_id, next_free_grant_at,\n            created_at,\n            updated_at\n        FROM user_logic_record\n        WHERE next_free_grant_at IS NOT NULL AND next_free_grant_at <= $1\n        ORDER BY next_free_grant_at ASC\n        LIMIT $2" )).
		WithArgs(cutoff, 100).
		WillReturnRows(rows)

	got, err := repo.ListDueFreeGrantUsers(context.Background(), cutoff, 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].NextFreeGrantAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserLogicRecordRepository_DisableFreeGrant(t *testing.T) {
	appDB, mock, cleanup := newUserLogicRecordTestDB(t)
	defer cleanup()

	repo := NewUserLogicRecordRepository(appDB)
	mock.ExpectExec(regexp.QuoteMeta("UPDATE user_logic_record \n\t\tSET next_free_grant_at = NULL, updated_at = $1\n\t\tWHERE user_id = $2" )).
		WithArgs(sqlmock.AnyArg(), 1).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.DisableFreeGrant(context.Background(), 1)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
