package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/pkg/database"
	"github.com/chinaxxren/gonotic/internal/pkg/errors"
	"go.uber.org/zap"
)

type VerificationRequestRepository interface {
	Upsert(ctx context.Context, email, code string, expiry int64, deviceCode, deviceType *string) error
	GetByEmail(ctx context.Context, email string) (*model.VerificationRequest, error)
	IncrementAttempts(ctx context.Context, email string) error
	ConsumeIfValid(ctx context.Context, email, code string, nowUnix int64, maxAttempts int) (bool, error)
	DeleteByEmail(ctx context.Context, email string) error
}

type verificationRequestRepository struct {
	db     *database.DB
	logger *zap.Logger
}

func NewVerificationRequestRepository(db *database.DB, logger *zap.Logger) VerificationRequestRepository {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &verificationRequestRepository{db: db, logger: logger}
}

func (r *verificationRequestRepository) Upsert(ctx context.Context, email, code string, expiry int64, deviceCode, deviceType *string) error {
	query := `
		INSERT INTO verification_requests (
			email, verification_code, code_expiry, code_attempts,
			device_code, device_type,
			created_at, updated_at
		) VALUES (?, ?, ?, 0, ?, ?, ?, ?)
		ON CONFLICT (email) DO UPDATE SET
			verification_code = EXCLUDED.verification_code,
			code_expiry = EXCLUDED.code_expiry,
			code_attempts = 0,
			device_code = EXCLUDED.device_code,
			device_type = EXCLUDED.device_type,
			updated_at = EXCLUDED.updated_at
	`

	now := time.Now().Unix()
	if _, err := database.ExecContext(ctx, r.db.DB.DB, query, email, code, expiry, deviceCode, deviceType, now, now); err != nil {
		return fmt.Errorf("upsert verification_requests failed: %w", err)
	}
	return nil
}

func (r *verificationRequestRepository) GetByEmail(ctx context.Context, email string) (*model.VerificationRequest, error) {
	query := `
		SELECT email, verification_code, code_expiry, code_attempts,
			device_code, device_type,
			created_at, updated_at
		FROM verification_requests
		WHERE email = ?
		LIMIT 1
	`

	row := database.QueryRowContext(ctx, r.db.DB.DB, query, email)
	var req model.VerificationRequest
	var deviceCode sql.NullString
	var deviceType sql.NullString
	if err := row.Scan(
		&req.Email,
		&req.VerificationCode,
		&req.CodeExpiry,
		&req.CodeAttempts,
		&deviceCode,
		&deviceType,
		&req.CreatedAt,
		&req.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New(errors.ErrResourceNotFound, "Verification request not found")
		}
		return nil, fmt.Errorf("query verification_requests failed: %w", err)
	}
	if deviceCode.Valid {
		value := deviceCode.String
		req.DeviceCode = &value
	}
	if deviceType.Valid {
		value := deviceType.String
		req.DeviceType = &value
	}
	return &req, nil
}

func (r *verificationRequestRepository) IncrementAttempts(ctx context.Context, email string) error {
	query := `
		UPDATE verification_requests
		SET code_attempts = code_attempts + 1, updated_at = ?
		WHERE email = ?
	`

	now := time.Now().Unix()
	res, err := database.ExecContext(ctx, r.db.DB.DB, query, now, email)
	if err != nil {
		return fmt.Errorf("increment verification_requests attempts failed: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return errors.New(errors.ErrResourceNotFound, "Verification request not found")
	}
	return nil
}

func (r *verificationRequestRepository) ConsumeIfValid(ctx context.Context, email, code string, nowUnix int64, maxAttempts int) (bool, error) {
	query := `
		DELETE FROM verification_requests
		WHERE email = ?
			AND verification_code = ?
			AND code_expiry > ?
			AND code_attempts < ?
	`

	res, err := database.ExecContext(ctx, r.db.DB.DB, query, email, code, nowUnix, maxAttempts)
	if err != nil {
		return false, fmt.Errorf("consume verification_requests failed: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("consume verification_requests rows affected failed: %w", err)
	}
	return rows > 0, nil
}

func (r *verificationRequestRepository) DeleteByEmail(ctx context.Context, email string) error {
	query := `DELETE FROM verification_requests WHERE email = ?`
	if _, err := database.ExecContext(ctx, r.db.DB.DB, query, email); err != nil {
		return fmt.Errorf("delete verification_requests failed: %w", err)
	}
	return nil
}
