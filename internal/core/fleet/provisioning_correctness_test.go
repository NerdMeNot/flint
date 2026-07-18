package fleet_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A1: two dispatch workers must not both read the same deficit and both boot it.
// The per-pool advisory lock serializes provisioning; a worker that can't take
// the lock skips the pool this tick. We simulate the "other worker" by holding
// the pool's provisioning lock on a separate connection and asserting Provision
// boots nothing until it's released.
func TestProvision_SkipsPoolLockedByAnotherWorker(t *testing.T) {
	h, _ := elasticHarness(t, "locked-pool")
	ctx := context.Background()
	poolID := poolIDByName(t, h, "locked-pool")

	h.insertPendingAssignment(t, poolID) // real, placeable demand

	// Another worker is already provisioning this pool: it holds the lock.
	conn, err := h.pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	var held bool
	require.NoError(t, conn.QueryRow(ctx,
		`SELECT pg_try_advisory_lock(hashtext('flint-prov:' || $1))`, poolID).Scan(&held))
	require.True(t, held, "test must hold the pool lock to simulate a concurrent worker")

	// Provisioning must skip the pool while another worker owns it.
	booted, err := h.fleet.Provision(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, booted, "must not double-provision a pool another worker is provisioning")
	assert.Equal(t, 0, countMachines(t, h, poolID, "requested")+
		countMachines(t, h, poolID, "provisioning"), "no machine reserved while locked")

	// Release the lock; the next tick provisions normally.
	_, err = conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtext('flint-prov:' || $1))`, poolID)
	require.NoError(t, err)

	booted, err = h.fleet.Provision(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, booted, "provisioning resumes once the pool lock is free")
}

// A2: demand pinned by hard run-affinity to an existing holder machine must not
// inflate the provisioning deficit — a new machine can't take it (it can only
// run on the holder), so booting for it just burns idle capacity.
func TestProvision_SkipsAffinityPinnedDemand(t *testing.T) {
	h, _ := elasticHarness(t, "affinity-pool")
	ctx := context.Background()
	poolID := poolIDByName(t, h, "affinity-pool")

	// Boot a machine for run R and bind R's work to it, so the machine becomes
	// R's workspace holder.
	runID := h.insertPendingForRun(t, poolID, "11111111-1111-1111-1111-111111111111")
	booted, err := h.fleet.Provision(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, booted)
	waitForCond(t, 5*time.Second, func() bool {
		return countMachines(t, h, poolID, "idle") == 1
	}, "booted machine should register and go idle")
	_, err = h.fleet.SchedulePending(ctx)
	require.NoError(t, err)
	waitForCond(t, 3*time.Second, func() bool {
		var n int
		_ = h.pool.QueryRow(ctx,
			`SELECT count(*) FROM step_assignments WHERE run_id = $1 AND status = 'assigned'`, runID).Scan(&n)
		return n == 1
	}, "run should bind to its holder machine")

	// More work arrives for the SAME run: it's affinity-pinned to the holder,
	// which is now busy. Provisioning a new machine cannot serve it.
	h.insertPendingForRun(t, poolID, runID)
	booted, err = h.fleet.Provision(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, booted, "affinity-pinned demand must not trigger a boot")

	// Sanity: work for a fresh run (no holder) still provisions.
	h.insertPendingAssignment(t, poolID)
	booted, err = h.fleet.Provision(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, booted, 1, "unpinned demand still provisions")
}

// insertPendingForRun plants pending demand for a specific run id, so tests can
// bind a run to a holder and then add more work for the same run.
func (h *harness) insertPendingForRun(t *testing.T, poolID, runID string) string {
	t.Helper()
	_, err := h.pool.Exec(context.Background(), `
		INSERT INTO step_assignments (step_id, workflow_id, run_id, step_name, attempt,
			pool_id, status, cpu_millis, memory_mb, payload)
		VALUES (gen_random_uuid(), gen_random_uuid(), $1, 'pinned-step', 0,
			$2, 'pending', 1000, 1024, '{}')`, runID, poolID)
	require.NoError(t, err)
	return runID
}
