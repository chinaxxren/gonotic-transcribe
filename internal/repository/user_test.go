package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/pkg/database"
	pkgerrors "github.com/chinaxxren/gonotic/internal/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newUserRepoTestDB(t *testing.T) (*database.DB, sqlmock.Sqlmock, func()) {
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

func TestBoolToSmallInt(t *testing.T) {
	require.Equal(t, int16(1), boolToSmallInt(true))
	require.Equal(t, int16(0), boolToSmallInt(false))
}

func TestUserRepository_UpdateDeviceInfo_NoRows(t *testing.T) {
	appDB, mock, cleanup := newUserRepoTestDB(t)
	defer cleanup()

	repo := NewUserRepository(appDB, zap.NewNop())

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users
		SET device_code = $1, device_type = $2, updated_at = $3
		WHERE id = $4
	`)).WithArgs("dc", "ios", sqlmock.AnyArg(), 1).WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.UpdateDeviceInfo(context.Background(), 1, "dc", "ios")
	require.Error(t, err)
	appErr := pkgerrors.GetAppError(err)
	require.NotNil(t, appErr)
	require.Equal(t, pkgerrors.ErrUserNotFound, appErr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepository_GetByDeviceCode_NoRows(t *testing.T) {
	appDB, mock, cleanup := newUserRepoTestDB(t)
	defer cleanup()

	repo := NewUserRepository(appDB, zap.NewNop())
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, email, role, last_active_at, active_session_id,
		       device_code, device_type, created_at, updated_at, is_active,
		       apple_sub,
		       verification_code, code_expiry, code_attempts
		FROM users
		WHERE device_code = $1
	`)).WithArgs("dc").WillReturnError(sql.ErrNoRows)

	_, err := repo.GetByDeviceCode(context.Background(), "dc")
	require.Error(t, err)
	appErr := pkgerrors.GetAppError(err)
	require.NotNil(t, appErr)
	require.Equal(t, pkgerrors.ErrUserNotFound, appErr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepository_GetByAppleSub_NoRows(t *testing.T) {
	appDB, mock, cleanup := newUserRepoTestDB(t)
	defer cleanup()

	repo := NewUserRepository(appDB, zap.NewNop())
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, email, role, last_active_at, active_session_id,
		       device_code, device_type, created_at, updated_at, is_active,
		       apple_sub,
		       verification_code, code_expiry, code_attempts
		FROM users
		WHERE apple_sub = $1
	`)).WithArgs("sub").WillReturnError(sql.ErrNoRows)

	_, err := repo.GetByAppleSub(context.Background(), "sub")
	require.Error(t, err)
	appErr := pkgerrors.GetAppError(err)
	require.NotNil(t, appErr)
	require.Equal(t, pkgerrors.ErrUserNotFound, appErr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepository_GetByID_NoRows(t *testing.T) {
	appDB, mock, cleanup := newUserRepoTestDB(t)
	defer cleanup()

	repo := NewUserRepository(appDB, zap.NewNop())
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, email, role, last_active_at, active_session_id,
		       device_code, device_type, created_at, updated_at, is_active,
		       apple_sub,
		       verification_code, code_expiry, code_attempts
		FROM users
		WHERE id = $1
	`)).WithArgs(1).WillReturnError(sql.ErrNoRows)

	_, err := repo.GetByID(context.Background(), 1)
	require.Error(t, err)
	appErr := pkgerrors.GetAppError(err)
	require.NotNil(t, appErr)
	require.Equal(t, pkgerrors.ErrUserNotFound, appErr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepository_GetByEmail_NoRows(t *testing.T) {
	appDB, mock, cleanup := newUserRepoTestDB(t)
	defer cleanup()

	repo := NewUserRepository(appDB, zap.NewNop())
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, email, role, last_active_at, active_session_id,
		       device_code, device_type, created_at, updated_at, is_active,
		       apple_sub,
		       verification_code, code_expiry, code_attempts
		FROM users
		WHERE email = $1
	`)).WithArgs("a@b.com").WillReturnError(sql.ErrNoRows)

	_, err := repo.GetByEmail(context.Background(), "a@b.com")
	require.Error(t, err)
	appErr := pkgerrors.GetAppError(err)
	require.NotNil(t, appErr)
	require.Equal(t, pkgerrors.ErrUserNotFound, appErr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepository_Create_SetsID(t *testing.T) {
	appDB, mock, cleanup := newUserRepoTestDB(t)
	defer cleanup()

	repo := NewUserRepository(appDB, zap.NewNop())
	now := time.Now().Unix()
	user := &model.User{Email: "a@b.com", Role: string(model.RoleFree), CreatedAt: now, UpdatedAt: now, IsActive: true, AppleSub: nil}

	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO users (
			email, role, created_at, updated_at, is_active, code_attempts, apple_sub
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`)).WithArgs(user.Email, user.Role, user.CreatedAt, user.UpdatedAt, int16(1), 0, user.AppleSub).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(10)))

	err := repo.Create(context.Background(), user)
	require.NoError(t, err)
	require.Equal(t, 10, user.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepository_Update_NoRows(t *testing.T) {
	appDB, mock, cleanup := newUserRepoTestDB(t)
	defer cleanup()

	repo := NewUserRepository(appDB, zap.NewNop())
	user := &model.User{ID: 1, Email: "a@b.com", Role: string(model.RoleFree)}

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users
		SET email = $1, role = $2, last_active_at = $3, active_session_id = $4,
		    device_code = $5, device_type = $6, updated_at = $7, is_active = $8,
		    verification_code = $9, code_expiry = $10, code_attempts = $11
		WHERE id = $12
	`)).WithArgs(user.Email, user.Role, user.LastActiveAt, user.ActiveSessionID, user.DeviceCode, user.DeviceType, sqlmock.AnyArg(), int16(0), user.VerificationCode, user.CodeExpiry, user.CodeAttempts, user.ID).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.Update(context.Background(), user)
	require.Error(t, err)
	appErr := pkgerrors.GetAppError(err)
	require.NotNil(t, appErr)
	require.Equal(t, pkgerrors.ErrUserNotFound, appErr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepository_BindAppleSub_Conflict(t *testing.T) {
	appDB, mock, cleanup := newUserRepoTestDB(t)
	defer cleanup()

	repo := NewUserRepository(appDB, zap.NewNop())

	pqErr := &pq.Error{Code: "23505"}
	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users
		SET apple_sub = $1, updated_at = $2
		WHERE id = $3
	`)).WithArgs("sub", sqlmock.AnyArg(), 1).WillReturnError(pqErr)

	err := repo.BindAppleSub(context.Background(), 1, "sub")
	require.Error(t, err)
	appErr := pkgerrors.GetAppError(err)
	require.NotNil(t, appErr)
	require.Equal(t, pkgerrors.ErrResourceConflict, appErr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepository_UpdateVerificationCode_NoRows(t *testing.T) {
	appDB, mock, cleanup := newUserRepoTestDB(t)
	defer cleanup()

	repo := NewUserRepository(appDB, zap.NewNop())

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users
		SET verification_code = $1, code_expiry = $2, code_attempts = 0, updated_at = $3
		WHERE email = $4
	`)).WithArgs("123", sqlmock.AnyArg(), sqlmock.AnyArg(), "a@b.com").WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.UpdateVerificationCode(context.Background(), "a@b.com", "123", time.Now().Add(time.Hour))
	require.Error(t, err)
	appErr := pkgerrors.GetAppError(err)
	require.NotNil(t, appErr)
	require.Equal(t, pkgerrors.ErrUserNotFound, appErr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepository_ClearVerificationCode_NoRows(t *testing.T) {
	appDB, mock, cleanup := newUserRepoTestDB(t)
	defer cleanup()

	repo := NewUserRepository(appDB, zap.NewNop())

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users
		SET verification_code = NULL,
		    code_expiry = NULL,
		    code_attempts = 0,
		    updated_at = $1
		WHERE id = $2
	`)).WithArgs(sqlmock.AnyArg(), 1).WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.ClearVerificationCode(context.Background(), 1)
	require.Error(t, err)
	appErr := pkgerrors.GetAppError(err)
	require.NotNil(t, appErr)
	require.Equal(t, pkgerrors.ErrUserNotFound, appErr.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepository_HasPaidBefore(t *testing.T) {
	appDB, mock, cleanup := newUserRepoTestDB(t)
	defer cleanup()

	repo := NewUserRepository(appDB, zap.NewNop())
	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COUNT(*) > 0 as has_paid
		FROM transactions 
		WHERE user_id = $1 
		  AND status IN ('VALIDATED', 'FULFILLED')
		  AND product_type != 'FREE'
	`)).WithArgs(1).WillReturnRows(sqlmock.NewRows([]string{"has_paid"}).AddRow(true))

	has, err := repo.HasPaidBefore(context.Background(), 1)
	require.NoError(t, err)
	require.True(t, has)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepository_UpdateLastActive(t *testing.T) {
	appDB, mock, cleanup := newUserRepoTestDB(t)
	defer cleanup()

	repo := NewUserRepository(appDB, zap.NewNop())

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users
		SET last_active_at = $1, updated_at = $2
		WHERE id = $3
	`)).WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 1).WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateLastActive(context.Background(), 1)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepository_IncrementCodeAttempts(t *testing.T) {
	appDB, mock, cleanup := newUserRepoTestDB(t)
	defer cleanup()

	repo := NewUserRepository(appDB, zap.NewNop())

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users
		SET code_attempts = code_attempts + 1, updated_at = $1
		WHERE id = $2
	`)).WithArgs(sqlmock.AnyArg(), 1).WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.IncrementCodeAttempts(context.Background(), 1)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepository_UpdateRole_SuccessAndNoRows(t *testing.T) {
	appDB, mock, cleanup := newUserRepoTestDB(t)
	defer cleanup()

	repo := NewUserRepository(appDB, zap.NewNop())

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users
		SET role = $1, updated_at = $2
		WHERE id = $3
	`)).WithArgs(model.RoleVip.String(), sqlmock.AnyArg(), 1).WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateRole(context.Background(), 1, model.RoleVip)
	require.NoError(t, err)

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users
		SET role = $1, updated_at = $2
		WHERE id = $3
	`)).WithArgs(model.RoleVip.String(), sqlmock.AnyArg(), 2).WillReturnResult(sqlmock.NewResult(0, 0))

	err = repo.UpdateRole(context.Background(), 2, model.RoleVip)
	require.Error(t, err)
	appErr := pkgerrors.GetAppError(err)
	require.NotNil(t, appErr)
	require.Equal(t, pkgerrors.ErrUserNotFound, appErr.Code)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepository_UpdateActiveSession(t *testing.T) {
	appDB, mock, cleanup := newUserRepoTestDB(t)
	defer cleanup()

	repo := NewUserRepository(appDB, zap.NewNop())

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users
		SET active_session_id = $1, updated_at = $2
		WHERE id = $3
	`)).WithArgs("sess", sqlmock.AnyArg(), 1).WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateActiveSession(context.Background(), 1, "sess")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepository_BindAppleSub_SuccessAndNoRows(t *testing.T) {
	appDB, mock, cleanup := newUserRepoTestDB(t)
	defer cleanup()

	repo := NewUserRepository(appDB, zap.NewNop())

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users
		SET apple_sub = $1, updated_at = $2
		WHERE id = $3
	`)).WithArgs("sub", sqlmock.AnyArg(), 1).WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.BindAppleSub(context.Background(), 1, "sub")
	require.NoError(t, err)

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users
		SET apple_sub = $1, updated_at = $2
		WHERE id = $3
	`)).WithArgs("sub", sqlmock.AnyArg(), 2).WillReturnResult(sqlmock.NewResult(0, 0))

	err = repo.BindAppleSub(context.Background(), 2, "sub")
	require.Error(t, err)
	appErr := pkgerrors.GetAppError(err)
	require.NotNil(t, appErr)
	require.Equal(t, pkgerrors.ErrUserNotFound, appErr.Code)

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepository_UpgradeUserRole_InvalidTargetRole(t *testing.T) {
	appDB, mock, cleanup := newUserRepoTestDB(t)
	defer cleanup()

	repo := NewUserRepository(appDB, zap.NewNop())

	rows := sqlmock.NewRows([]string{
		"id", "email", "role", "last_active_at", "active_session_id",
		"device_code", "device_type", "created_at", "updated_at", "is_active",
		"apple_sub",
		"verification_code", "code_expiry", "code_attempts",
	}).AddRow(
		1, "a@b.com", string(model.RoleFree), nil, nil,
		nil, nil, int64(1), int64(1), true,
		nil,
		nil, nil, 0,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, email, role, last_active_at, active_session_id,
		       device_code, device_type, created_at, updated_at, is_active,
		       apple_sub,
		       verification_code, code_expiry, code_attempts
		FROM users
		WHERE id = $1
	`)).WithArgs(1).WillReturnRows(rows)

	err := repo.UpgradeUserRole(context.Background(), 1, model.UserRole("invalid"))
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepository_UpgradeUserRole_SuccessAndNoRows(t *testing.T) {
	appDB, mock, cleanup := newUserRepoTestDB(t)
	defer cleanup()

	repo := NewUserRepository(appDB, zap.NewNop())

	rows := sqlmock.NewRows([]string{
		"id", "email", "role", "last_active_at", "active_session_id",
		"device_code", "device_type", "created_at", "updated_at", "is_active",
		"apple_sub",
		"verification_code", "code_expiry", "code_attempts",
	}).AddRow(
		1, "a@b.com", string(model.RoleFree), nil, nil,
		nil, nil, int64(1), int64(1), true,
		nil,
		nil, nil, 0,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, email, role, last_active_at, active_session_id,
		       device_code, device_type, created_at, updated_at, is_active,
		       apple_sub,
		       verification_code, code_expiry, code_attempts
		FROM users
		WHERE id = $1
	`)).WithArgs(1).WillReturnRows(rows)

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users
		SET role = $1, updated_at = $2
		WHERE id = $3
	`)).WithArgs(model.RoleVip.String(), sqlmock.AnyArg(), 1).WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpgradeUserRole(context.Background(), 1, model.RoleVip)
	require.NoError(t, err)

	// no rows affected
	rows2 := sqlmock.NewRows([]string{
		"id", "email", "role", "last_active_at", "active_session_id",
		"device_code", "device_type", "created_at", "updated_at", "is_active",
		"apple_sub",
		"verification_code", "code_expiry", "code_attempts",
	}).AddRow(
		2, "b@b.com", string(model.RoleFree), nil, nil,
		nil, nil, int64(1), int64(1), true,
		nil,
		nil, nil, 0,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, email, role, last_active_at, active_session_id,
		       device_code, device_type, created_at, updated_at, is_active,
		       apple_sub,
		       verification_code, code_expiry, code_attempts
		FROM users
		WHERE id = $1
	`)).WithArgs(2).WillReturnRows(rows2)

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users
		SET role = $1, updated_at = $2
		WHERE id = $3
	`)).WithArgs(model.RoleVip.String(), sqlmock.AnyArg(), 2).WillReturnResult(sqlmock.NewResult(0, 0))

	err = repo.UpgradeUserRole(context.Background(), 2, model.RoleVip)
	require.Error(t, err)
	appErr := pkgerrors.GetAppError(err)
	require.NotNil(t, appErr)
	require.Equal(t, pkgerrors.ErrUserNotFound, appErr.Code)

	require.NoError(t, mock.ExpectationsWereMet())
}
