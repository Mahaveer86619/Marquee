package store

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"time"

	"marquee/internal/store/db"
)

// loggedDB wraps a connection or transaction and logs each query by the name
// sqlc gives it (the "-- name: X" comment), with its duration and error.
// Argument values are not logged.
type loggedDB struct {
	inner db.DBTX
	log   *slog.Logger
}

func (l loggedDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	start := time.Now()
	res, err := l.inner.ExecContext(ctx, query, args...)
	l.record(ctx, query, start, err)
	return res, err
}

func (l loggedDB) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	start := time.Now()
	stmt, err := l.inner.PrepareContext(ctx, query)
	l.record(ctx, query, start, err)
	return stmt, err
}

func (l loggedDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	start := time.Now()
	rows, err := l.inner.QueryContext(ctx, query, args...)
	l.record(ctx, query, start, err)
	return rows, err
}

func (l loggedDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	start := time.Now()
	row := l.inner.QueryRowContext(ctx, query, args...)
	l.record(ctx, query, start, row.Err())
	return row
}

func (l loggedDB) record(ctx context.Context, query string, start time.Time, err error) {
	attrs := []any{"query", queryName(query), "ms", time.Since(start).Milliseconds()}
	if err != nil && err != sql.ErrNoRows {
		attrs = append(attrs, "err", err)
	}
	l.log.DebugContext(ctx, "sql", attrs...)
}

// queryName returns the sqlc query name, or the first words of the SQL.
func queryName(query string) string {
	const marker = "-- name: "
	if i := strings.Index(query, marker); i >= 0 {
		rest := query[i+len(marker):]
		if j := strings.IndexAny(rest, " \n"); j > 0 {
			return rest[:j]
		}
		return rest
	}
	fields := strings.Fields(query)
	if len(fields) > 3 {
		fields = fields[:3]
	}
	return strings.Join(fields, " ")
}
