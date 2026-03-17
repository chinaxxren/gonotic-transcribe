package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/pkg/database"
)

// SummaryLedgerRepository defines operations for summary_ledger table.
type SummaryLedgerRepository interface {
	Create(ctx context.Context, entry *model.SummaryLedger) error
	ListByUser(ctx context.Context, userID int, limit int) ([]*model.SummaryLedger, error)
	ListRecentTemplates(ctx context.Context, userID int, limit int) ([]*model.SummaryTemplateUsage, error)
}

type summaryLedgerRepository struct {
	db *sql.DB
}

// NewSummaryLedgerRepository creates a repository backed by the main DB.
func NewSummaryLedgerRepository(db *database.DB) SummaryLedgerRepository {
	return &summaryLedgerRepository{db: db.DB.DB}
}

func (r *summaryLedgerRepository) Create(ctx context.Context, entry *model.SummaryLedger) error {
	if entry == nil {
		return fmt.Errorf("summary ledger entry is nil")
	}
	query := `
		INSERT INTO summary_ledger (
			user_id, business_id, cycle_id, template_id, summary_delta, summary_key, source, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`
	entry.CreatedAt = time.Now().Unix()
	var id int64
	err := database.QueryRowContext(ctx, r.db, query,
		entry.UserID,
		entry.BusinessID,
		entry.CycleID,
		nullableTemplateID(entry.TemplateID),
		entry.SummaryDelta,
		entry.SummaryKey,
		entry.Source,
		entry.CreatedAt,
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("create summary_ledger failed: %w", err)
	}
	entry.ID = id
	return nil
}

func (r *summaryLedgerRepository) ListByUser(ctx context.Context, userID int, limit int) ([]*model.SummaryLedger, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT id, user_id, business_id, cycle_id, template_id, summary_delta, source, created_at
		FROM summary_ledger
		WHERE user_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`
	rows, err := database.QueryContext(ctx, r.db, query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list summary_ledger failed: %w", err)
	}
	defer rows.Close()

	var entries []*model.SummaryLedger
	for rows.Next() {
		var entry model.SummaryLedger
		if err := rows.Scan(
			&entry.ID,
			&entry.UserID,
			&entry.BusinessID,
			&entry.CycleID,
			&entry.TemplateID,
			&entry.SummaryDelta,
			&entry.Source,
			&entry.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan summary_ledger failed: %w", err)
		}
		entries = append(entries, &entry)
	}
	return entries, nil
}

func (r *summaryLedgerRepository) ListRecentTemplates(ctx context.Context, userID int, limit int) ([]*model.SummaryTemplateUsage, error) {
	if limit <= 0 {
		limit = 5
	}
	query := `
		SELECT template_id, SUM(summary_delta) AS used_times, MAX(created_at) as last_used
		FROM summary_ledger
		WHERE user_id = ? AND template_id IS NOT NULL
		GROUP BY template_id
		ORDER BY last_used DESC
		LIMIT ?
	`
	rows, err := database.QueryContext(ctx, r.db, query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent templates from summary_ledger failed: %w", err)
	}
	defer rows.Close()

	var result []*model.SummaryTemplateUsage
	for rows.Next() {
		var tplID sql.NullInt64
		var usedTimes int
		var lastUsed int64
		if err := rows.Scan(&tplID, &usedTimes, &lastUsed); err != nil {
			return nil, fmt.Errorf("scan recent templates failed: %w", err)
		}
		if !tplID.Valid {
			continue
		}
		result = append(result, &model.SummaryTemplateUsage{
			TemplateID: tplID.Int64,
			UserID:     userID,
			UsedTimes:  usedTimes,
			UpdatedAt:  lastUsed,
			CreatedAt:  lastUsed,
		})
	}
	return result, nil
}

func nullableTemplateID(ptr *int64) interface{} {
	if ptr == nil {
		return nil
	}
	return *ptr
}
