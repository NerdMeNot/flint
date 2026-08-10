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

// queueStepAt inserts a queued step at a given wave, eligible `agoSeconds` ago.
// Steps are addressed by name so the assertions read as a queue order.
func queueStepAt(t *testing.T, pool *pgxpool.Pool, wfID, name string, wave, agoSeconds int) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO steps (workflow_id, name, exec_type, status, wave, attempt,
		     max_attempts, step_def, timeout_seconds, queued_at)
		 VALUES ($1, $2, 'run', 'queued', $3, 0, 1, '{}'::jsonb, 3600,
		         now() - make_interval(secs := $4))`,
		wfID, name, wave, agoSeconds)
	require.NoError(t, err)
}

// quiesceQueue clears any work other tests in this package left queued. The
// suite shares one database, and claim order is a global property — a stray
// queued step from an earlier test would otherwise be claimed ahead of the
// fixtures under test.
func quiesceQueue(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE steps SET status = 'cancelled' WHERE status = 'queued'`)
	require.NoError(t, err)
}

// startEmptyWorkflow creates a running workflow with no steps of its own, so a
// test can queue exactly the steps it wants to reason about.
func startEmptyWorkflow(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	runID := uuid.NewString()
	seedRun(t, pool, runID)

	var wfID string
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO workflows (run_id, status, dag_waves, input, step_outputs)
		 VALUES ($1, 'running', '[]'::jsonb, '{}'::jsonb, '{}'::jsonb) RETURNING id`,
		runID).Scan(&wfID))
	return wfID
}

// Claiming is ordered by when a step became eligible, not by how deep it sits in
// its own workflow. Wave numbers are per-workflow depths and mean nothing across
// workflows, so ordering by wave first let every newly-started run jump ahead of
// work that had already been waiting — starving long pipelines under load.
func TestClaimOrder_OldestEligibleFirstAcrossWorkflows(t *testing.T) {
	ctx := context.Background()
	pool := internalTestDB(t)
	quiesceQueue(t, pool)

	deepAndOld := startEmptyWorkflow(t, pool)
	shallowAndNew := startEmptyWorkflow(t, pool)

	// A deep step that has been waiting ten minutes.
	queueStepAt(t, pool, deepAndOld, "deep-old", 5, 600)
	// A brand-new run's first step, eligible one second ago.
	queueStepAt(t, pool, shallowAndNew, "shallow-new", 0, 1)

	claimed, err := db.New(pool).ClaimQueuedSteps(ctx, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	assert.Equal(t, "deep-old", claimed[0].Name,
		"the step that has waited longest must be claimed first, whatever its wave")
}

// Within one instant, wave still breaks the tie so a workflow's own steps go
// shallowest-first.
//
// Asserted on WHICH steps a bounded batch claims, not on the order they come
// back in: the ORDER BY sits in the subquery that selects ids, and an UPDATE's
// RETURNING makes no promise about row order. Claim priority is about selection.
func TestClaimOrder_WaveBreaksTiesWithinTheSameInstant(t *testing.T) {
	ctx := context.Background()
	pool := internalTestDB(t)
	quiesceQueue(t, pool)
	wfID := startEmptyWorkflow(t, pool)

	queueStepAt(t, pool, wfID, "deepest", 3, 60)
	queueStepAt(t, pool, wfID, "middle", 2, 60)
	queueStepAt(t, pool, wfID, "shallowest", 1, 60)
	// Collapse all three onto one instant so only wave can separate them.
	_, err := pool.Exec(ctx,
		`UPDATE steps SET queued_at = now() - interval '60 seconds' WHERE workflow_id=$1`, wfID)
	require.NoError(t, err)

	claimed, err := db.New(pool).ClaimQueuedSteps(ctx, 2)
	require.NoError(t, err)
	require.Len(t, claimed, 2)

	got := []string{claimed[0].Name, claimed[1].Name}
	assert.ElementsMatch(t, []string{"shallowest", "middle"}, got,
		"equal eligibility → the two shallowest are the ones claimed")
}

// A saturation check: with work arriving continuously, the long-waiting step
// must still get served rather than being pushed back forever.
func TestClaimOrder_SustainedArrivalsDoNotStarveWaitingWork(t *testing.T) {
	ctx := context.Background()
	pool := internalTestDB(t)
	quiesceQueue(t, pool)
	q := db.New(pool)

	old := startEmptyWorkflow(t, pool)
	queueStepAt(t, pool, old, "waiting-since-forever", 7, 3600)

	// Three rounds of fresh wave-0 arrivals, each claimed one at a time.
	served := false
	for round := 0; round < 3 && !served; round++ {
		fresh := startEmptyWorkflow(t, pool)
		queueStepAt(t, pool, fresh, uuid.NewString(), 0, 0)

		claimed, err := q.ClaimQueuedSteps(ctx, 1)
		require.NoError(t, err)
		require.Len(t, claimed, 1)
		if claimed[0].Name == "waiting-since-forever" {
			served = true
		}
	}
	assert.True(t, served, "the oldest queued step must be served ahead of newer arrivals")
}
