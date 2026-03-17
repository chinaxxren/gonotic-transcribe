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

	"github.com/chinaxxren/gonotic/internal/pkg/database"
)

func newVerificationRequestTestDB(t *testing.T) (*database.DB, sqlmock.Sqlmock, func()) {
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

func TestVerificationRequestRepository_Upsert(t *testing.T) {
	appDB, mock, cleanup := newVerificationRequestTestDB(t)
	defer cleanup()

	repo := NewVerificationRequestRepository(appDB, zap.NewNop())
	email := "test@example.com"
	code := "123456"
	expiry := time.Now().Unix() + 600
	deviceCode := ptrString("device123")
	deviceType := ptrString("mobile")

	mock.ExpectExec(regexp.QuoteMeta(`
        INSERT INTO verification_requests (
            email, verification_code, code_expiry, code_attempts,
            device_code, device_type,
            created_at, updated_at
        ) VALUES ($1, $2, $3, 0, $4, $5, $6, $7)
        ON CONFLICT (email) DO UPDATE SET
            verification_code = EXCLUDED.verification_code,
            code_expiry = EXCLUDED.code_expiry,
            code_attempts = 0,
            device_code = EXCLUDED.device_code,
            device_type = EXCLUDED.device_type,
            updated_at = EXCLUDED.updated_at
    `)).
		WithArgs(email, code, expiry, deviceCode, deviceType, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.Upsert(context.Background(), email, code, expiry, deviceCode, deviceType)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVerificationRequestRepository_GetByEmail(t *testing.T) {
	appDB, mock, cleanup := newVerificationRequestTestDB(t)
	defer cleanup()

	repo := NewVerificationRequestRepository(appDB, zap.NewNop())
	email := "test@example.com"
	now := time.Now().Unix()

	rows := sqlmock.NewRows([]string{
		"email", "verification_code", "code_expiry", "code_attempts", "device_code", "device_type", "created_at", "updated_at",
	}).AddRow(
		email, "654321", now+300, 0, "device123", "mobile", now-600, now-600,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
        SELECT email, verification_code, code_expiry, code_attempts, device_code, device_type, created_at, updated_at
        FROM verification_requests
        WHERE email = $1
        LIMIT 1
    `)).WithArgs(email).WillReturnRows(rows)

	result, err := repo.GetByEmail(context.Background(), email)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, email, result.Email)
	require.Equal(t, "654321", result.VerificationCode)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVerificationRequestRepository_GetByEmail_NotFound(t *testing.T) {
	appDB, mock, cleanup := newVerificationRequestTestDB(t)
	defer cleanup()

	repo := NewVerificationRequestRepository(appDB, zap.NewNop())
	email := "missing@example.com"

	mock.ExpectQuery(regexp.QuoteMeta(`
        SELECT email, verification_code, code_expiry, code_attempts, device_code, device_type, created_at, updated_at
        FROM verification_requests
        WHERE email = $1
        LIMIT 1
    `)).WithArgs(email).WillReturnError(sql.ErrNoRows)

	result, err := repo.GetByEmail(context.Background(), email)
	require.Error(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "Verification request not found")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVerificationRequestRepository_IncrementAttempts(t *testing.T) {
	appDB, mock, cleanup := newVerificationRequestTestDB(t)
	defer cleanup()

	repo := NewVerificationRequestRepository(appDB, zap.NewNop())
	email := "test@example.com"

	mock.ExpectExec(regexp.QuoteMeta(`
        UPDATE verification_requests
        SET code_attempts = code_attempts + 1, updated_at = $1
        WHERE email = $2
    `)).WithArgs(sqlmock.AnyArg(), email).WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.IncrementAttempts(context.Background(), email)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVerificationRequestRepository_ConsumeIfValid(t *testing.T) {
	appDB, mock, cleanup := newVerificationRequestTestDB(t)
	defer cleanup()

	repo := NewVerificationRequestRepository(appDB, zap.NewNop())
	email := "test@example.com"
	code := "123456"
	now := time.Now().Unix()
	maxAttempts := 3

	mock.ExpectExec(regexp.QuoteMeta(`
        DELETE FROM verification_requests
        WHERE email = $1
            AND verification_code = $2
            AND code_expiry > $3
            AND code_attempts < $4
    `)).WithArgs(email, code, now, maxAttempts).WillReturnResult(sqlmock.NewResult(0, 1))

	valid, err := repo.ConsumeIfValid(context.Background(), email, code, now, maxAttempts)
	require.NoError(t, err)
	require.True(t, valid)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVerificationRequestRepository_ConsumeIfValid_Invalid(t *testing.T) {
	appDB, mock, cleanup := newVerificationRequestTestDB(t)
	defer cleanup()

	repo := NewVerificationRequestRepository(appDB, zap.NewNop())
	email := "test@example.com"
	code := "wrongcode"
	now := time.Now().Unix()
	maxAttempts := 3

	mock.ExpectExec(regexp.QuoteMeta(`
        DELETE FROM verification_requests
        WHERE email = $1
            AND verification_code = $2
            AND code_expiry > $3
            AND code_attempts < $4
    `)).WithArgs(email, code, now, maxAttempts).WillReturnResult(sqlmock.NewResult(0, 0))

	valid, err := repo.ConsumeIfValid(context.Background(), email, code, now, maxAttempts)
	require.NoError(t, err)
	require.False(t, valid)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVerificationRequestRepository_DeleteByEmail(t *testing.T) {
	appDB, mock, cleanup := newVerificationRequestTestDB(t)
	defer cleanup()

	repo := NewVerificationRequestRepository(appDB, zap.NewNop())
	email := "test@example.com"

	mock.ExpectExec(regexp.QuoteMeta(`
        DELETE FROM verification_requests
        WHERE email = $1
    `)).WithArgs(email).WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.DeleteByEmail(context.Background(), email)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
