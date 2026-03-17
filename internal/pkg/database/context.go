package database

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
)

// contextKeyTx 用于在 context 中存储事务指针，避免键冲突。
type contextKeyTx struct{}

// ContextWithTx 将 sqlx.Tx 附加到上下文，方便仓储层复用同一事务。
func ContextWithTx(ctx context.Context, tx *sqlx.Tx) context.Context {
	return context.WithValue(ctx, contextKeyTx{}, tx)
}

// GetTx 从 context 中取出 sqlx.Tx，如果上层没有注入则返回 false。
func GetTx(ctx context.Context) (*sqlx.Tx, bool) {
	value := ctx.Value(contextKeyTx{})
	if value == nil {
		return nil, false
	}
	tx, ok := value.(*sqlx.Tx)
	return tx, ok && tx != nil
}

// ExecContext 根据上下文中是否存在事务自动选择 tx 或 db 执行。
func ExecContext(ctx context.Context, db *sql.DB, query string, args ...interface{}) (sql.Result, error) {
	query = sqlx.Rebind(sqlx.DOLLAR, query)
	if tx, ok := GetTx(ctx); ok {
		return tx.ExecContext(ctx, query, args...)
	}
	return db.ExecContext(ctx, query, args...)
}

// QueryContext 根据上下文选择 tx 或 db 执行查询。
func QueryContext(ctx context.Context, db *sql.DB, query string, args ...interface{}) (*sql.Rows, error) {
	query = sqlx.Rebind(sqlx.DOLLAR, query)
	if tx, ok := GetTx(ctx); ok {
		return tx.QueryContext(ctx, query, args...)
	}
	return db.QueryContext(ctx, query, args...)
}

// QueryRowContext 根据上下文选择 tx 或 db 执行单行查询。
func QueryRowContext(ctx context.Context, db *sql.DB, query string, args ...interface{}) *sql.Row {
	query = sqlx.Rebind(sqlx.DOLLAR, query)
	if tx, ok := GetTx(ctx); ok {
		return tx.QueryRowContext(ctx, query, args...)
	}
	return db.QueryRowContext(ctx, query, args...)
}
