// Package database provides database migration functionality.
package database

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

// Migration represents a database migration.
type Migration struct {
	Version     int    // Migration version number
	Description string // Migration description
	Up          string // SQL for applying migration
	Down        string // SQL for reverting migration
}

func splitSQLStatements(sqlStr string) []string {
	// NOTE: A naive strings.Split(";") breaks PostgreSQL statements that contain
	// semicolons inside dollar-quoted blocks, e.g. DO $$ ... ; ... $$.
	// This splitter only splits on semicolons that are not inside:
	// - single-quoted strings
	// - double-quoted identifiers
	// - dollar-quoted blocks (e.g. $$...$$ or $tag$...$tag$)
	statements := make([]string, 0, 8)
	var buf strings.Builder

	inSingle := false
	inDouble := false
	dollarTag := ""

	flush := func() {
		stmt := strings.TrimSpace(buf.String())
		buf.Reset()
		if stmt != "" {
			statements = append(statements, stmt)
		}
	}

	// Helper: attempt to read a dollar-quote tag starting at i (sqlStr[i] == '$').
	// Returns (tag, ok, newIndexAfterTag).
	readDollarTag := func(i int) (string, bool, int) {
		j := i + 1
		for j < len(sqlStr) {
			c := sqlStr[j]
			if c == '$' {
				return sqlStr[i : j+1], true, j + 1
			}
			if !(c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
				return "", false, i
			}
			j++
		}
		return "", false, i
	}

	for i := 0; i < len(sqlStr); i++ {
		c := sqlStr[i]

		// Inside dollar-quoted block: look for closing tag.
		if dollarTag != "" {
			if c == '$' && strings.HasPrefix(sqlStr[i:], dollarTag) {
				buf.WriteString(dollarTag)
				i += len(dollarTag) - 1
				dollarTag = ""
				continue
			}
			buf.WriteByte(c)
			continue
		}

		// Handle single quotes (with '' escaping)
		if inSingle {
			buf.WriteByte(c)
			if c == '\'' {
				if i+1 < len(sqlStr) && sqlStr[i+1] == '\'' {
					buf.WriteByte(sqlStr[i+1])
					i++
					continue
				}
				inSingle = false
			}
			continue
		}

		// Handle double quotes ("" escaping)
		if inDouble {
			buf.WriteByte(c)
			if c == '"' {
				if i+1 < len(sqlStr) && sqlStr[i+1] == '"' {
					buf.WriteByte(sqlStr[i+1])
					i++
					continue
				}
				inDouble = false
			}
			continue
		}

		// Not in any quoted context.
		switch c {
		case '\'':
			inSingle = true
			buf.WriteByte(c)
			continue
		case '"':
			inDouble = true
			buf.WriteByte(c)
			continue
		case '$':
			if tag, ok, nextIdx := readDollarTag(i); ok {
				dollarTag = tag
				buf.WriteString(tag)
				i = nextIdx - 1
				continue
			}
			buf.WriteByte(c)
			continue
		case ';':
			flush()
			continue
		default:
			buf.WriteByte(c)
		}
	}

	flush()
	return statements
}

// Migrator handles database migrations.
type Migrator struct {
	db     *DB
	logger *zap.Logger
	table  string
}

// NewMigrator creates a new migrator instance.
//
// Parameters:
//   - db: Database connection
//   - logger: Logger instance
//
// Returns:
//   - *Migrator: Migrator instance
func NewMigrator(db *DB, logger *zap.Logger) *Migrator {
	table := "schema_migrations"
	if strings.ToLower(strings.TrimSpace(db.DriverName())) == "postgres" {
		// Avoid conflicts with a schema_migrations table imported from MySQL by pgloader.
		table = "schema_migrations_pg"
	}
	return &Migrator{
		db:     db,
		logger: logger,
		table:  table,
	}
}

// createMigrationsTable creates the migrations tracking table if it doesn't exist.
//
// Returns:
//   - error: Error if table creation fails
func (m *Migrator) createMigrationsTable(ctx context.Context) error {
	driver := strings.ToLower(strings.TrimSpace(m.db.DriverName()))
	var query string
	if driver == "postgres" {
		query = fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %s (
				version INT PRIMARY KEY,
				description VARCHAR(255) NOT NULL,
				applied_at BIGINT NOT NULL
			);
			CREATE INDEX IF NOT EXISTS %s ON %s (applied_at);
		`, m.table, "idx_"+m.table+"_applied_at", m.table)
	} else {
		query = fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %s (
				version INT PRIMARY KEY,
				description VARCHAR(255) NOT NULL,
				applied_at BIGINT NOT NULL,
				INDEX idx_applied_at (applied_at)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
		`, m.table)
	}

	statements := splitSQLStatements(query)
	for _, stmt := range statements {
		if _, err := m.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to create migrations table: %w", err)
		}
	}
	return nil
}

func (m *Migrator) rebind(query string) string {
	return m.db.Rebind(query)
}

func rebindTx(tx *sqlx.Tx, query string) string {
	return tx.Rebind(query)
}

// getAppliedMigrations returns a set of applied migration versions.
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//
// Returns:
//   - map[int]bool: Set of applied migration versions
//   - error: Error if query fails
func (m *Migrator) getAppliedMigrations(ctx context.Context) (map[int]bool, error) {
	var versions []int
	query := fmt.Sprintf("SELECT version FROM %s ORDER BY version", m.table)
	err := m.db.SelectContext(ctx, &versions, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get applied migrations: %w", err)
	}

	applied := make(map[int]bool)
	for _, v := range versions {
		applied[v] = true
	}

	return applied, nil
}

// recordMigration records a migration as applied.
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - migration: Migration to record
//
// Returns:
//   - error: Error if recording fails
func (m *Migrator) recordMigration(ctx context.Context, migration Migration) error {
	query := m.rebind(fmt.Sprintf(`INSERT INTO %s (version, description, applied_at) VALUES (?, ?, ?)`, m.table))
	_, err := m.db.ExecContext(ctx, query, migration.Version, migration.Description, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}
	return nil
}

// removeMigration removes a migration record.
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - version: Migration version to remove
//
// Returns:
//   - error: Error if removal fails
func (m *Migrator) removeMigration(ctx context.Context, version int) error {
	query := m.rebind(fmt.Sprintf(`DELETE FROM %s WHERE version = ?`, m.table))
	_, err := m.db.ExecContext(ctx, query, version)
	if err != nil {
		return fmt.Errorf("failed to remove migration record: %w", err)
	}
	return nil
}

// Up applies all pending migrations.
// Migrations are applied in order by version number.
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - migrations: List of migrations to apply
//
// Returns:
//   - error: Error if any migration fails
//
// If a migration fails, the function stops and returns an error.
// Previously applied migrations are not rolled back automatically.
func (m *Migrator) Up(ctx context.Context, migrations []Migration) error {
	// Create migrations table if it doesn't exist
	if err := m.createMigrationsTable(ctx); err != nil {
		return err
	}

	// Get applied migrations
	applied, err := m.getAppliedMigrations(ctx)
	if err != nil {
		return err
	}

	// Sort migrations by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	// Apply pending migrations
	for _, migration := range migrations {
		if applied[migration.Version] {
			m.logger.Debug("Skipping already applied migration",
				zap.Int("version", migration.Version),
				zap.String("description", migration.Description))
			continue
		}

		m.logger.Info("Applying migration",
			zap.Int("version", migration.Version),
			zap.String("description", migration.Description))

		// Execute migration in a transaction
		err := m.db.WithTransaction(ctx, func(tx *sqlx.Tx) error {
			// Execute migration SQL statements sequentially
			statements := splitSQLStatements(migration.Up)
			for _, stmt := range statements {
				if _, err := tx.ExecContext(ctx, stmt); err != nil {
					return fmt.Errorf("failed to execute migration SQL (%s): %w", stmt, err)
				}
			}

			// Record migration
			query := rebindTx(tx, fmt.Sprintf(`INSERT INTO %s (version, description, applied_at) VALUES (?, ?, ?)`, m.table))
			if _, err := tx.ExecContext(ctx, query, migration.Version, migration.Description, time.Now().Unix()); err != nil {
				return fmt.Errorf("failed to record migration: %w", err)
			}

			return nil
		})

		if err != nil {
			return fmt.Errorf("migration %d failed: %w", migration.Version, err)
		}

		m.logger.Info("✅ Migration applied successfully",
			zap.Int("version", migration.Version))
	}

	m.logger.Info("✅ All migrations applied successfully")
	return nil
}

// Down reverts the last applied migration.
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - migrations: List of all migrations
//
// Returns:
//   - error: Error if rollback fails
func (m *Migrator) Down(ctx context.Context, migrations []Migration) error {
	// Get applied migrations
	applied, err := m.getAppliedMigrations(ctx)
	if err != nil {
		return err
	}

	if len(applied) == 0 {
		m.logger.Info("No migrations to revert")
		return nil
	}

	// Find the last applied migration
	var lastMigration *Migration
	maxVersion := 0
	for _, migration := range migrations {
		if applied[migration.Version] && migration.Version > maxVersion {
			maxVersion = migration.Version
			lastMigration = &migration
		}
	}

	if lastMigration == nil {
		return fmt.Errorf("no migration found to revert")
	}

	m.logger.Info("Reverting migration",
		zap.Int("version", lastMigration.Version),
		zap.String("description", lastMigration.Description))

	// Execute rollback in a transaction
	err = m.db.WithTransaction(ctx, func(tx *sqlx.Tx) error {
		// Execute rollback SQL statements sequentially
		statements := splitSQLStatements(lastMigration.Down)
		for _, stmt := range statements {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("failed to execute rollback SQL (%s): %w", stmt, err)
			}
		}

		// Remove migration record
		query := rebindTx(tx, fmt.Sprintf(`DELETE FROM %s WHERE version = ?`, m.table))
		if _, err := tx.ExecContext(ctx, query, lastMigration.Version); err != nil {
			return fmt.Errorf("failed to remove migration record: %w", err)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	m.logger.Info("✅ Migration reverted successfully",
		zap.Int("version", lastMigration.Version))
	return nil
}

// Status returns the current migration status.
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - migrations: List of all migrations
//
// Returns:
//   - []string: List of status messages
//   - error: Error if status check fails
func (m *Migrator) Status(ctx context.Context, migrations []Migration) ([]string, error) {
	// Create migrations table if it doesn't exist
	if err := m.createMigrationsTable(ctx); err != nil {
		return nil, err
	}

	// Get applied migrations
	applied, err := m.getAppliedMigrations(ctx)
	if err != nil {
		return nil, err
	}

	// Sort migrations by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	// Build status messages
	var status []string
	for _, migration := range migrations {
		state := "pending"
		if applied[migration.Version] {
			state = "applied"
		}
		status = append(status, fmt.Sprintf("[%s] %d: %s", state, migration.Version, migration.Description))
	}

	return status, nil
}
