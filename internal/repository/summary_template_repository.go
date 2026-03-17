package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/pkg/database"
)

// SummaryGroupRepository defines CRUD operations for summary_groups.
type SummaryGroupRepository interface {
	Create(ctx context.Context, group *model.SummaryGroup) error
	Update(ctx context.Context, group *model.SummaryGroup) error
	Delete(ctx context.Context, id int64) error
	ListAll(ctx context.Context) ([]*model.SummaryGroup, error)
	GetByID(ctx context.Context, id int64) (*model.SummaryGroup, error)
}

// SummaryTemplateRepository defines CRUD operations for summary_templates.
type SummaryTemplateRepository interface {
	Create(ctx context.Context, tpl *model.SummaryTemplate) error
	Update(ctx context.Context, tpl *model.SummaryTemplate) error
	Delete(ctx context.Context, id int64) error
	GetByID(ctx context.Context, id int64) (*model.SummaryTemplate, error)
	ListByGroupIDs(ctx context.Context, groupIDs []int64) ([]*model.SummaryTemplate, error)
	ListByTemplateType(ctx context.Context, templateType string, limit int) ([]*model.SummaryTemplate, error)
	ListByIDs(ctx context.Context, ids []int64) ([]*model.SummaryTemplate, error)
	CountByGroupIDs(ctx context.Context, groupIDs []int64) (map[int64]int, error)
	CountByTemplateType(ctx context.Context, templateType string) (int, error)
}

type summaryGroupRepository struct {
	db *sql.DB
}

type summaryTemplateRepository struct {
	db *sql.DB
}

// NewSummaryGroupRepository creates a repository instance.
func NewSummaryGroupRepository(db *database.DB) SummaryGroupRepository {
	return &summaryGroupRepository{db: db.DB.DB}
}

// NewSummaryTemplateRepository creates a repository instance.
func NewSummaryTemplateRepository(db *database.DB) SummaryTemplateRepository {
	return &summaryTemplateRepository{db: db.DB.DB}
}

func (r *summaryGroupRepository) Create(ctx context.Context, group *model.SummaryGroup) error {
	now := time.Now().Unix()
	if group.CreatedAt == 0 {
		group.CreatedAt = now
	}
	if group.UpdatedAt == 0 {
		group.UpdatedAt = now
	}
	query := `
		INSERT INTO summary_groups (name, image_url, group_type, is_visible, display_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`
	var id int64
	err := database.QueryRowContext(ctx, r.db, query,
		group.Name,
		nullableString(group.ImageURL),
		group.GroupType,
		boolToTinyInt(group.Visible),
		group.DisplayOrder,
		group.CreatedAt,
		group.UpdatedAt,
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("create summary group failed: %w", err)
	}
	group.ID = id
	return nil
}

func (r *summaryGroupRepository) Update(ctx context.Context, group *model.SummaryGroup) error {
	group.UpdatedAt = time.Now().Unix()
	query := `
		UPDATE summary_groups
		SET name = ?, image_url = ?, group_type = ?, is_visible = ?, display_order = ?, updated_at = ?
		WHERE id = ?
	`
	if _, err := database.ExecContext(ctx, r.db, query,
		group.Name,
		nullableString(group.ImageURL),
		group.GroupType,
		boolToTinyInt(group.Visible),
		group.DisplayOrder,
		group.UpdatedAt,
		group.ID,
	); err != nil {
		return fmt.Errorf("update summary group failed: %w", err)
	}
	return nil
}

func (r *summaryGroupRepository) Delete(ctx context.Context, id int64) error {
	if _, err := database.ExecContext(ctx, r.db, `DELETE FROM summary_groups WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete summary group failed: %w", err)
	}
	return nil
}

func (r *summaryGroupRepository) ListAll(ctx context.Context) ([]*model.SummaryGroup, error) {
	rows, err := database.QueryContext(ctx, r.db, `
		SELECT id, name, image_url, group_type, is_visible, display_order, created_at, updated_at
		FROM summary_groups
		ORDER BY display_order ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list summary groups failed: %w", err)
	}
	defer rows.Close()

	var groups []*model.SummaryGroup
	for rows.Next() {
		var g model.SummaryGroup
		if err := rows.Scan(
			&g.ID,
			&g.Name,
			&g.ImageURL,
			&g.GroupType,
			&g.Visible,
			&g.DisplayOrder,
			&g.CreatedAt,
			&g.UpdatedAt,
		); err != nil {
			return nil, err
		}
		groups = append(groups, &g)
	}
	return groups, nil
}

func (r *summaryGroupRepository) GetByID(ctx context.Context, id int64) (*model.SummaryGroup, error) {
	row := database.QueryRowContext(ctx, r.db, `
		SELECT id, name, image_url, group_type, is_visible, display_order, created_at, updated_at
		FROM summary_groups WHERE id = ? LIMIT 1
	`, id)
	var g model.SummaryGroup
	if err := row.Scan(
		&g.ID,
		&g.Name,
		&g.ImageURL,
		&g.GroupType,
		&g.Visible,
		&g.DisplayOrder,
		&g.CreatedAt,
		&g.UpdatedAt,
	); err != nil {
		if errorsIsNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get summary group failed: %w", err)
	}
	return &g, nil
}

func (r *summaryTemplateRepository) Create(ctx context.Context, tpl *model.SummaryTemplate) error {
	now := time.Now().Unix()
	if tpl.CreatedAt == 0 {
		tpl.CreatedAt = now
	}
	if tpl.UpdatedAt == 0 {
		tpl.UpdatedAt = now
	}
	query := `
		INSERT INTO summary_templates (
			group_id, name, intro, description, storage, location, template_type, owner_id,
			is_visible, display_order, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`
	var id int64
	err := database.QueryRowContext(ctx, r.db, query,
		tpl.GroupID,
		tpl.Name,
		nullableString(tpl.Intro),
		nullableString(tpl.Description),
		tpl.Storage,
		tpl.Location,
		tpl.TemplateType,
		tpl.OwnerID,
		boolToTinyInt(tpl.Visible),
		tpl.DisplayOrder,
		tpl.CreatedAt,
		tpl.UpdatedAt,
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("create summary template failed: %w", err)
	}
	tpl.ID = id
	return nil
}

func (r *summaryTemplateRepository) Update(ctx context.Context, tpl *model.SummaryTemplate) error {
	tpl.UpdatedAt = time.Now().Unix()
	query := `
		UPDATE summary_templates
		SET group_id = ?, name = ?, intro = ?, description = ?, storage = ?, location = ?,
			template_type = ?, owner_id = ?, is_visible = ?, display_order = ?, updated_at = ?
		WHERE id = ?
	`
	if _, err := database.ExecContext(ctx, r.db, query,
		tpl.GroupID,
		nullableString(tpl.Name),
		nullableString(tpl.Intro),
		nullableString(tpl.Description),
		tpl.Storage,
		tpl.Location,
		tpl.TemplateType,
		tpl.OwnerID,
		boolToTinyInt(tpl.Visible),
		tpl.DisplayOrder,
		tpl.UpdatedAt,
		tpl.ID,
	); err != nil {
		return fmt.Errorf("update summary template failed: %w", err)
	}
	return nil
}

func (r *summaryTemplateRepository) Delete(ctx context.Context, id int64) error {
	if _, err := database.ExecContext(ctx, r.db, `DELETE FROM summary_templates WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete summary template failed: %w", err)
	}
	return nil
}

func (r *summaryTemplateRepository) GetByID(ctx context.Context, id int64) (*model.SummaryTemplate, error) {
	row := database.QueryRowContext(ctx, r.db, `
		SELECT id, group_id, name, intro, description, storage, location, template_type, owner_id,
		       is_visible, display_order, created_at, updated_at
		FROM summary_templates WHERE id = ? LIMIT 1
	`, id)
	var tpl model.SummaryTemplate
	if err := row.Scan(
		&tpl.ID,
		&tpl.GroupID,
		&tpl.Name,
		&tpl.Intro,
		&tpl.Description,
		&tpl.Storage,
		&tpl.Location,
		&tpl.TemplateType,
		&tpl.OwnerID,
		&tpl.Visible,
		&tpl.DisplayOrder,
		&tpl.CreatedAt,
		&tpl.UpdatedAt,
	); err != nil {
		if errorsIsNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get summary template failed: %w", err)
	}
	return &tpl, nil
}

func (r *summaryTemplateRepository) ListByGroupIDs(ctx context.Context, groupIDs []int64) ([]*model.SummaryTemplate, error) {
	if len(groupIDs) == 0 {
		return []*model.SummaryTemplate{}, nil
	}
	placeholders := make([]string, len(groupIDs))
	args := make([]interface{}, len(groupIDs))
	for i, id := range groupIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf(`
		SELECT id, group_id, name, intro, description, storage, location, template_type, owner_id,
		       is_visible, display_order, created_at, updated_at
		FROM summary_templates
		WHERE group_id IN (%s) AND template_type <> 'user'
		ORDER BY display_order ASC, id ASC
	`, strings.Join(placeholders, ","))
	rows, err := database.QueryContext(ctx, r.db, sqlx.Rebind(sqlx.DOLLAR, query), args...)
	if err != nil {
		return nil, fmt.Errorf("list summary templates failed: %w", err)
	}
	defer rows.Close()

	var templates []*model.SummaryTemplate
	for rows.Next() {
		var tpl model.SummaryTemplate
		if err := rows.Scan(
			&tpl.ID,
			&tpl.GroupID,
			&tpl.Name,
			&tpl.Intro,
			&tpl.Description,
			&tpl.Storage,
			&tpl.Location,
			&tpl.TemplateType,
			&tpl.OwnerID,
			&tpl.Visible,
			&tpl.DisplayOrder,
			&tpl.CreatedAt,
			&tpl.UpdatedAt,
		); err != nil {
			return nil, err
		}
		templates = append(templates, &tpl)
	}
	return templates, nil
}

func (r *summaryTemplateRepository) ListByTemplateType(ctx context.Context, templateType string, limit int) ([]*model.SummaryTemplate, error) {
	templateType = strings.TrimSpace(templateType)
	if templateType == "" {
		return []*model.SummaryTemplate{}, nil
	}

	query := `
		SELECT id, group_id, name, intro, description, storage, location, template_type, owner_id,
		       is_visible, display_order, created_at, updated_at
		FROM summary_templates
		WHERE template_type = ?
		ORDER BY display_order ASC, id ASC
	`
	args := []interface{}{templateType}
	if limit > 0 {
		query = fmt.Sprintf("%s\n\t\tLIMIT %d\n\t", query, limit)
	}

	rows, err := database.QueryContext(ctx, r.db, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list templates by type failed: %w", err)
	}
	defer rows.Close()

	var templates []*model.SummaryTemplate
	for rows.Next() {
		var tpl model.SummaryTemplate
		if err := rows.Scan(
			&tpl.ID,
			&tpl.GroupID,
			&tpl.Name,
			&tpl.Intro,
			&tpl.Description,
			&tpl.Storage,
			&tpl.Location,
			&tpl.TemplateType,
			&tpl.OwnerID,
			&tpl.Visible,
			&tpl.DisplayOrder,
			&tpl.CreatedAt,
			&tpl.UpdatedAt,
		); err != nil {
			return nil, err
		}
		templates = append(templates, &tpl)
	}
	return templates, nil
}

func (r *summaryTemplateRepository) CountByGroupIDs(ctx context.Context, groupIDs []int64) (map[int64]int, error) {
	counts := make(map[int64]int)
	if len(groupIDs) == 0 {
		return counts, nil
	}

	placeholders := make([]string, len(groupIDs))
	args := make([]interface{}, len(groupIDs))
	for i, id := range groupIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT group_id, COUNT(*) as cnt
		FROM summary_templates
		WHERE group_id IN (%s) AND template_type <> 'user'
		GROUP BY group_id
	`, strings.Join(placeholders, ","))

	rows, err := database.QueryContext(ctx, r.db, sqlx.Rebind(sqlx.DOLLAR, query), args...)
	if err != nil {
		return nil, fmt.Errorf("count summary templates failed: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var groupID int64
		var cnt int
		if err := rows.Scan(&groupID, &cnt); err != nil {
			return nil, err
		}
		counts[groupID] = cnt
	}
	return counts, nil
}

func (r *summaryTemplateRepository) CountByTemplateType(ctx context.Context, templateType string) (int, error) {
	templateType = strings.TrimSpace(templateType)
	if templateType == "" {
		return 0, nil
	}
	row := database.QueryRowContext(ctx, r.db, `
		SELECT COUNT(*)
		FROM summary_templates
		WHERE template_type = ?
	`, templateType)
	var cnt int
	if err := row.Scan(&cnt); err != nil {
		return 0, fmt.Errorf("count templates by type failed: %w", err)
	}
	return cnt, nil
}

func (r *summaryTemplateRepository) ListByIDs(ctx context.Context, ids []int64) ([]*model.SummaryTemplate, error) {
	if len(ids) == 0 {
		return []*model.SummaryTemplate{}, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf(`
		SELECT id, group_id, name, intro, description, storage, location, template_type, owner_id,
		       is_visible, display_order, created_at, updated_at
		FROM summary_templates
		WHERE id IN (%s)
		ORDER BY display_order ASC, id ASC
	`, strings.Join(placeholders, ","))

	rows, err := database.QueryContext(ctx, r.db, sqlx.Rebind(sqlx.DOLLAR, query), args...)
	if err != nil {
		return nil, fmt.Errorf("list templates by ids failed: %w", err)
	}
	defer rows.Close()

	var templates []*model.SummaryTemplate
	for rows.Next() {
		var tpl model.SummaryTemplate
		if err := rows.Scan(
			&tpl.ID,
			&tpl.GroupID,
			&tpl.Name,
			&tpl.Intro,
			&tpl.Description,
			&tpl.Storage,
			&tpl.Location,
			&tpl.TemplateType,
			&tpl.OwnerID,
			&tpl.Visible,
			&tpl.DisplayOrder,
			&tpl.CreatedAt,
			&tpl.UpdatedAt,
		); err != nil {
			return nil, err
		}
		templates = append(templates, &tpl)
	}
	return templates, nil
}

func nullableInt64Value(ptr *int64) interface{} {
	if ptr == nil {
		return nil
	}
	return *ptr
}

func nullableString(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func errorsIsNoRows(err error) bool {
	return err == sql.ErrNoRows
}

func boolToTinyInt(val bool) int {
	if val {
		return 1
	}
	return 0
}
