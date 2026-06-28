package engine

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// conflictRetries is how many times retryOnConflict re-runs a transaction body
// that aborted with a deadlock or serialization failure before giving up.
const conflictRetries = 3

// retryOnConflict runs fn, retrying when Postgres aborts the transaction with a
// deadlock (40P01) or serialization failure (40001). These are transient by
// definition — the database chose a victim to break a cycle — so re-running the
// whole tx body resolves them. It makes cross-aggregate operations (e.g. a
// CompleteStep on a child racing a CancelWorkflow on its parent, which lock the
// workflow tree in opposite orders) self-heal instead of surfacing an error.
//
// fn must be a complete, self-contained transaction (begin → commit) with no
// side effects that escape a rolled-back attempt; CompleteStep/CancelWorkflow
// satisfy this (their NOTIFY/observer calls happen only after a successful commit).
func retryOnConflict(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 0; attempt < conflictRetries; attempt++ {
		if err = fn(); err == nil || !isSerializationConflict(err) {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return err
}

// isSerializationConflict reports whether err is a Postgres deadlock (40P01) or
// serialization failure (40001) — the two classes safe to resolve by retry.
func isSerializationConflict(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "40P01" || pgErr.Code == "40001"
	}
	return false
}
