package fleet_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A5: terminal step_assignments past the retention window are pruned (they don't
// cascade from run deletion — no FK on the hot dispatch path), while recent and
// still-live assignments survive.
func TestRetention_PrunesOldTerminalAssignments(t *testing.T) {
	h, _ := elasticHarness(t, "retention-pool")
	ctx := context.Background()
	poolID := poolIDByName(t, h, "retention-pool")

	insert := func(status, finishedExpr string) {
		_, err := h.pool.Exec(ctx, `
			INSERT INTO step_assignments (step_id, workflow_id, run_id, step_name, attempt,
				pool_id, status, cpu_millis, memory_mb, payload, finished_at)
			VALUES (gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 'r', 0,
				$1, $2, 100, 128, '{}', `+finishedExpr+`)`, poolID, status)
		require.NoError(t, err)
	}
	insert("succeeded", "now() - interval '100 days'") // old terminal → pruned
	insert("failed", "now()")                          // recent terminal → kept
	insert("running", "NULL")                          // live → kept

	require.NoError(t, h.q.CleanupOldStepAssignments(ctx, 90))

	count := func(status string) int {
		var n int
		require.NoError(t, h.pool.QueryRow(ctx,
			`SELECT count(*) FROM step_assignments WHERE pool_id = $1 AND status = $2`, poolID, status).Scan(&n))
		return n
	}
	assert.Equal(t, 0, count("succeeded"), "old terminal assignment must be pruned")
	assert.Equal(t, 1, count("failed"), "recent terminal assignment survives")
	assert.Equal(t, 1, count("running"), "live assignment survives")
}
