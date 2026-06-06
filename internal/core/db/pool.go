package db

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Pool is the database connection pool interface. Extends the sqlc-generated
// DBTX with Ping (health checks) and Begin (transactions).
//
// *pgxpool.Pool satisfies this interface. Tests can mock it to avoid needing
// a real Postgres instance.
type Pool interface {
	Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
	Ping(ctx context.Context) error
	Begin(ctx context.Context) (pgx.Tx, error)
}
