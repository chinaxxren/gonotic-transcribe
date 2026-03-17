// Package database provides PostgreSQL database connection management and utilities.
// It uses sqlx for enhanced database operations and connection pooling.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq" // PostgreSQL driver
	"go.uber.org/zap"
)

// DB wraps sqlx.DB with additional functionality.
type DB struct {
	*sqlx.DB
	logger *zap.Logger
}

// NewFromSQLX wraps an existing sqlx.DB with the database.DB helper.
// Primarily intended for tests where the caller controls the underlying connection (e.g. sqlmock).
func NewFromSQLX(db *sqlx.DB, logger *zap.Logger) *DB {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &DB{
		DB:     db,
		logger: logger,
	}
}

// Config contains database configuration.
type Config struct {
	URL          string        // Database connection URL
	MaxConns     int           // Maximum number of open connections
	MaxIdle      int           // Maximum number of idle connections
	ConnTimeout  time.Duration // Connection timeout
	ConnLifetime time.Duration // Maximum connection lifetime
}

// New creates a new database connection with the given configuration.
// It establishes a connection pool and verifies connectivity.
//
// Parameters:
//   - cfg: Database configuration
//   - logger: Logger instance for database operations
//
// Returns:
//   - *DB: Database connection wrapper
//   - error: Error if connection fails
//
// The function configures connection pooling with the following settings:
//   - MaxOpenConns: Maximum number of open connections to the database
//   - MaxIdleConns: Maximum number of idle connections in the pool
//   - ConnMaxLifetime: Maximum amount of time a connection may be reused
//   - ConnMaxIdleTime: Maximum amount of time a connection may be idle
func New(cfg Config, logger *zap.Logger) (*DB, error) {
	url := strings.TrimSpace(cfg.URL)
	if url == "" {
		return nil, fmt.Errorf("database URL is empty")
	}

	driver := "postgres"
	dsn := url
	if !strings.HasPrefix(url, "postgres://") && !strings.HasPrefix(url, "postgresql://") {
		return nil, fmt.Errorf("DATABASE_URL must be a PostgreSQL URL (postgres:// or postgresql://)")
	}

	logger.Debug("Connecting to database", zap.String("driver", driver))

	// Open database connection
	db, err := sqlx.Connect(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(cfg.MaxConns)
	db.SetMaxIdleConns(cfg.MaxIdle)
	db.SetConnMaxLifetime(cfg.ConnLifetime)
	db.SetConnMaxIdleTime(cfg.ConnTimeout)

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	logger.Info("✅ Database connection established",
		zap.Int("max_conns", cfg.MaxConns),
		zap.Int("max_idle", cfg.MaxIdle),
		zap.Duration("conn_lifetime", cfg.ConnLifetime))

	return &DB{
		DB:     db,
		logger: logger,
	}, nil
}

// Close closes the database connection and releases all resources.
// It should be called when the application shuts down.
//
// Returns:
//   - error: Error if closing fails
func (db *DB) Close() error {
	db.logger.Info("Closing database connection")
	return db.DB.Close()
}

// HealthCheck performs a health check on the database connection.
// It verifies that the database is reachable and responsive.
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//
// Returns:
//   - error: Error if health check fails
func (db *DB) HealthCheck(ctx context.Context) error {
	// Ping database with timeout
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("database health check failed: %w", err)
	}

	// Check if we can execute a simple query
	var result int
	err := db.GetContext(ctx, &result, "SELECT 1")
	if err != nil {
		return fmt.Errorf("database query check failed: %w", err)
	}

	return nil
}

// Stats returns database connection pool statistics.
// Useful for monitoring and debugging connection pool behavior.
//
// Returns:
//   - sql.DBStats: Connection pool statistics
func (db *DB) Stats() sql.DBStats {
	return db.DB.Stats()
}

// WithTransaction executes a function within a database transaction.
// If the function returns an error, the transaction is rolled back.
// Otherwise, the transaction is committed.
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - fn: Function to execute within the transaction
//
// Returns:
//   - error: Error if transaction fails or function returns error
//
// Example:
//
//	err := db.WithTransaction(ctx, func(tx *sqlx.Tx) error {
//	    _, err := tx.ExecContext(ctx, "INSERT INTO users ...")
//	    return err
//	})
func (db *DB) WithTransaction(ctx context.Context, fn func(*sqlx.Tx) error) error {
	// Begin transaction
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Execute function
	err = fn(tx)
	if err != nil {
		// Rollback on error
		if rbErr := tx.Rollback(); rbErr != nil && rbErr != sql.ErrTxDone {
			db.logger.Error("Failed to rollback transaction",
				zap.Error(rbErr),
				zap.Error(err))
		}
		return err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// LogStats logs current database connection pool statistics.
// Useful for monitoring connection pool health.
func (db *DB) LogStats() {
	stats := db.Stats()
	db.logger.Info("Database connection pool stats",
		zap.Int("open_connections", stats.OpenConnections),
		zap.Int("in_use", stats.InUse),
		zap.Int("idle", stats.Idle),
		zap.Int64("wait_count", stats.WaitCount),
		zap.Duration("wait_duration", stats.WaitDuration),
		zap.Int64("max_idle_closed", stats.MaxIdleClosed),
		zap.Int64("max_lifetime_closed", stats.MaxLifetimeClosed))
}
