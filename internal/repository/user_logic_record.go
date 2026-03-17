package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/pkg/database"
)

// UserLogicRecordRepository manages user_logic_record table operations.
type UserLogicRecordRepository interface {
	// GetByUserID returns the logic record for a user, or nil if absent.
	GetByUserID(ctx context.Context, userID int) (*model.UserLogicRecord, error)
	// UpsertNextFreeGrant inserts or updates the next_free_grant_at field for a user.
	UpsertNextFreeGrant(ctx context.Context, userID int, nextGrant *int64) error
	// ListDueFreeGrantUsers returns users whose next_free_grant_at is due (<= cutoff).
	ListDueFreeGrantUsers(ctx context.Context, cutoff int64, limit int) ([]*model.UserLogicRecord, error)
	// DisableFreeGrant disables free grant for a user by setting next_free_grant_at to NULL
	DisableFreeGrant(ctx context.Context, userID int) error
}

type userLogicRecordRepository struct {
	db *database.DB
}

// NewUserLogicRecordRepository creates a repository instance.
func NewUserLogicRecordRepository(db *database.DB) UserLogicRecordRepository {
	return &userLogicRecordRepository{db: db}
}

func (r *userLogicRecordRepository) GetByUserID(ctx context.Context, userID int) (*model.UserLogicRecord, error) {
	query := `
        SELECT user_id, next_free_grant_at,
            created_at,
            updated_at
        FROM user_logic_record
        WHERE user_id = ?
        LIMIT 1
    `

	row := database.QueryRowContext(ctx, r.db.DB.DB, query, userID)
	var rec model.UserLogicRecord
	var nextGrant sql.NullInt64
	if err := row.Scan(&rec.UserID, &nextGrant, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query user_logic_record failed: %w", err)
	}
	if nextGrant.Valid {
		value := nextGrant.Int64
		rec.NextFreeGrantAt = &value
	}
	return &rec, nil
}

func (r *userLogicRecordRepository) UpsertNextFreeGrant(ctx context.Context, userID int, nextGrant *int64) error {
	query := `
        INSERT INTO user_logic_record (
            user_id,
            next_free_grant_at,
            created_at,
            updated_at
        ) VALUES (?, ?, ?, ?)
        ON CONFLICT (user_id) DO UPDATE SET
            next_free_grant_at = EXCLUDED.next_free_grant_at,
            updated_at = EXCLUDED.updated_at
    `

	var next interface{}
	if nextGrant != nil {
		next = *nextGrant
	} else {
		next = nil
	}

	now := time.Now().Unix()

	if _, err := database.ExecContext(ctx, r.db.DB.DB, query, userID, next, now, now); err != nil {
		return fmt.Errorf("upsert user_logic_record failed: %w", err)
	}
	return nil
}

func (r *userLogicRecordRepository) ListDueFreeGrantUsers(ctx context.Context, cutoff int64, limit int) ([]*model.UserLogicRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `
        SELECT user_id, next_free_grant_at,
            created_at,
            updated_at
        FROM user_logic_record
        WHERE next_free_grant_at IS NOT NULL AND next_free_grant_at <= ?
        ORDER BY next_free_grant_at ASC
        LIMIT ?
    `

	rows, err := database.QueryContext(ctx, r.db.DB.DB, query, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("list due user_logic_record failed: %w", err)
	}
	defer rows.Close()

	var records []*model.UserLogicRecord
	for rows.Next() {
		var rec model.UserLogicRecord
		var nextGrant sql.NullInt64
		if err := rows.Scan(&rec.UserID, &nextGrant, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan user_logic_record failed: %w", err)
		}
		if nextGrant.Valid {
			value := nextGrant.Int64
			rec.NextFreeGrantAt = &value
		}
		records = append(records, &rec)
	}
	return records, nil
}

// DisableFreeGrant disables free grant for a user by setting next_free_grant_at to NULL.
// This is used when a user has paid before and should no longer receive free grants.
func (r *userLogicRecordRepository) DisableFreeGrant(ctx context.Context, userID int) error {
	query := `
		UPDATE user_logic_record 
		SET next_free_grant_at = NULL, updated_at = ?
		WHERE user_id = ?
	`

	_, err := database.ExecContext(ctx, r.db.DB.DB, query, time.Now().Unix(), userID)
	if err != nil {
		return fmt.Errorf("disable free grant failed for user %d: %w", userID, err)
	}

	return nil
}
