package mysql

import (
	"context"
	"database/sql"
)

// Executor 让仓储代码同时接受 *sql.DB 与 *sql.Tx。
type Executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

var (
	_ Executor = (*sql.DB)(nil)
	_ Executor = (*sql.Tx)(nil)
)

// rowExists 判断幂等 UPDATE 未命中时记录是否仍然存在。
func rowExists(ctx context.Context, ex Executor, query string, args ...any) (bool, error) {
	var count int
	if err := ex.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}
