package engine

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// strandedEvent inserts an outbox event already claimed and long since stranded
// in 'processing' — what a worker that died mid-delivery leaves behind.
func strandedEvent(t *testing.T, pool *pgxpool.Pool, attempts, maxAttempts int) string {
	t.Helper()
	var id string
	require.NoError(t, pool.QueryRow(context.Background(), `
		INSERT INTO flint_outbox (event_type, payload, idempotency_key, status,
			attempts, max_attempts, claimed_at)
		VALUES ('webhook', '{}'::jsonb, $1, 'processing', $2, $3, now() - interval '10 minutes')
		RETURNING id`, uuid.NewString(), attempts, maxAttempts).Scan(&id))
	return id
}

func outboxStatus(t *testing.T, pool *pgxpool.Pool, id string) string {
	t.Helper()
	var s string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT status FROM flint_outbox WHERE id=$1`, id).Scan(&s))
	return s
}

// An event stranded with attempts left goes back to pending for another try.
func TestRecoverStaleOutbox_RetriesWhenBudgetRemains(t *testing.T) {
	pool := internalTestDB(t)
	id := strandedEvent(t, pool, 1, 5)

	_, err := db.New(pool).RecoverStaleOutboxEvents(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "pending", outboxStatus(t, pool, id))
}

// An event whose delivery reliably strands the worker must eventually stop.
//
// attempts is incremented at claim, but only FailOutboxEvent converted an
// exhausted budget into 'failed' — and a delivery that kills the worker never
// reaches it. Recovery returned the event to 'pending' unconditionally, so the
// one payload capable of hanging a worker was retried every five minutes
// forever: exactly the case the attempt budget exists to stop.
func TestRecoverStaleOutbox_GivesUpWhenBudgetIsExhausted(t *testing.T) {
	pool := internalTestDB(t)
	id := strandedEvent(t, pool, 5, 5)

	_, err := db.New(pool).RecoverStaleOutboxEvents(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "failed", outboxStatus(t, pool, id),
		"a delivery that always strands must exhaust its budget, not loop forever")

	// And it must not be picked up again.
	claimed, err := db.New(pool).ClaimOutboxBatch(context.Background(), 10)
	require.NoError(t, err)
	for _, c := range claimed {
		assert.NotEqual(t, id, c.ID, "a failed event must never be claimed again")
	}
}

// Terminally-failed events were the one status nothing pruned — resolved events
// were cleaned, failures accumulated forever.
func TestCleanFailedOutbox_PrunesOldFailuresOnly(t *testing.T) {
	ctx := context.Background()
	pool := internalTestDB(t)
	q := db.New(pool)

	var oldFailed, recentFailed string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO flint_outbox (event_type, payload, idempotency_key, status, created_at)
		VALUES ('webhook', '{}'::jsonb, $1, 'failed', now() - interval '60 days')
		RETURNING id`, uuid.NewString()).Scan(&oldFailed))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO flint_outbox (event_type, payload, idempotency_key, status, created_at)
		VALUES ('webhook', '{}'::jsonb, $1, 'failed', now())
		RETURNING id`, uuid.NewString()).Scan(&recentFailed))

	require.NoError(t, q.CleanFailedOutbox(ctx))

	var n int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM flint_outbox WHERE id=$1`, oldFailed).Scan(&n))
	assert.Equal(t, 0, n, "an old failure must be pruned")

	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM flint_outbox WHERE id=$1`, recentFailed).Scan(&n))
	assert.Equal(t, 1, n, "a recent failure stays visible to an operator")
}
