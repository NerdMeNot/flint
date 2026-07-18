package fleet_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/pkg/compute"
)

// A3: an instance Flint created but hasn't finished recording (still 'requested',
// its provider_ref not yet committed) must NOT be destroyed as a zombie —
// reconcile correlates on the durable MachineID tag, not the provider_ref.
func TestReconcile_DoesNotDestroyMachineBeingRecorded(t *testing.T) {
	h, fake := elasticHarness(t, "reconcile-live-pool")
	fake.OnCreate = nil // we drive Create directly; no auto-registration
	ctx := context.Background()
	poolID := poolIDByName(t, h, "reconcile-live-pool")

	// Mid-provision: a machine row exists in 'requested' with no provider_ref.
	machineID := "22222222-2222-2222-2222-222222222222"
	_, err := h.pool.Exec(ctx, `
		INSERT INTO machines (id, pool_id, provider, status, cpu_millis, memory_mb, disk_gb, arch)
		VALUES ($1, $2, 'fake-cloud', 'requested', 2000, 4096, 0, 'amd64')`, machineID, poolID)
	require.NoError(t, err)

	// The provider has a live instance for it (distinct instance id), tagged with
	// the MachineID — exactly what List surfaces.
	_, err = fake.Create(ctx, compute.Offer{InstanceType: "t3.small", Arch: "amd64"},
		compute.Bootstrap{MachineID: machineID})
	require.NoError(t, err)

	// Two reconcile passes (the second-sighting grace would reap a true zombie).
	require.NoError(t, h.fleet.Reconcile(ctx))
	require.NoError(t, h.fleet.Reconcile(ctx))

	refs, err := fake.List(ctx)
	require.NoError(t, err)
	live := 0
	for _, r := range refs {
		if r.MachineID == machineID && r.State != compute.RefTerminated {
			live++
		}
	}
	assert.Equal(t, 1, live, "a live instance still being recorded must not be reaped as a zombie")
}

// A2: machine transitions are optimistic — UpdateMachineStatus moves the row only
// if it's still in the expected `from` status, so a transition computed against a
// stale read can't clobber a concurrent one (last-writer-wins would corrupt
// busy/idle accounting under multi-replica load).
func TestUpdateMachineStatus_OptimisticGuard(t *testing.T) {
	h, _ := elasticHarness(t, "optimistic-pool")
	ctx := context.Background()
	poolID := poolIDByName(t, h, "optimistic-pool")

	h.insertPendingAssignment(t, poolID)
	_, err := h.fleet.Provision(ctx)
	require.NoError(t, err)
	waitForCond(t, 5*time.Second, func() bool {
		return countMachines(t, h, poolID, "idle") == 1
	}, "machine should register and go idle")

	var mid string
	require.NoError(t, h.pool.QueryRow(ctx,
		`SELECT id FROM machines WHERE pool_id = $1 AND status = 'idle'`, poolID).Scan(&mid))

	// First idle→busy from the true state wins.
	n, err := h.q.UpdateMachineStatus(ctx, db.UpdateMachineStatusParams{
		ID: mid, FromStatus: "idle", Status: "busy",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	// A second transition still keyed on `idle` (a stale read) affects no rows —
	// the machine is busy now.
	n, err = h.q.UpdateMachineStatus(ctx, db.UpdateMachineStatusParams{
		ID: mid, FromStatus: "idle", Status: "busy",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(0), n, "a stale from-status must not clobber a concurrent transition")
}

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

// #2b: two workers must not each scale a pool down to minWarm against independent
// idle counts and collectively breach the warm floor. The per-pool scale-down
// lock serializes them; a worker that can't take it skips the pool this tick. We
// hold the lock on a separate connection and assert ScaleDown terminates nothing
// until it's released. (The scheduler's guard, #2a, is a blocking transaction
// lock exercised by the existing scheduling tests.)
func TestScaleDown_SkipsPoolLockedByAnotherWorker(t *testing.T) {
	h, _ := elasticHarness(t, "scaledown-lock-pool")
	ctx := context.Background()
	poolID := poolIDByName(t, h, "scaledown-lock-pool")

	// Boot an idle machine, then age it well past the pool's idle TTL.
	h.insertPendingAssignment(t, poolID)
	_, err := h.fleet.Provision(ctx)
	require.NoError(t, err)
	waitForCond(t, 5*time.Second, func() bool {
		return countMachines(t, h, poolID, "idle") == 1
	}, "machine should register and go idle")
	_, err = h.pool.Exec(ctx,
		`UPDATE step_assignments SET status = 'succeeded', finished_at = now() WHERE pool_id = $1`, poolID)
	require.NoError(t, err)
	_, err = h.pool.Exec(ctx,
		`UPDATE machines SET status = 'idle', idle_since = now() - interval '1 hour' WHERE pool_id = $1`, poolID)
	require.NoError(t, err)

	// Another worker is already scaling this pool down: it holds the lock.
	conn, err := h.pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	var held bool
	require.NoError(t, conn.QueryRow(ctx,
		`SELECT pg_try_advisory_lock(hashtext('flint-scaledown:' || $1))`, poolID).Scan(&held))
	require.True(t, held)

	reaped, err := h.fleet.ScaleDown(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, reaped, "must not scale down a pool another worker is scaling down")
	assert.Equal(t, 1, countMachines(t, h, poolID, "idle"), "the idle machine survives while locked")

	// Release; scale-down proceeds.
	_, err = conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtext('flint-scaledown:' || $1))`, poolID)
	require.NoError(t, err)
	reaped, err = h.fleet.ScaleDown(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, reaped, "scale-down resumes once the lock is free")
}

// A4: a machine can be status='idle' yet still hold a live assignment (accounting
// drift). Scale-down must not terminate it and kill the running work.
func TestScaleDown_SkipsMachineWithLiveWork(t *testing.T) {
	h, _ := elasticHarness(t, "idle-with-work-pool")
	ctx := context.Background()
	poolID := poolIDByName(t, h, "idle-with-work-pool")

	h.insertPendingAssignment(t, poolID)
	_, err := h.fleet.Provision(ctx)
	require.NoError(t, err)
	waitForCond(t, 5*time.Second, func() bool {
		return countMachines(t, h, poolID, "idle") == 1
	}, "machine should register and go idle")

	var mid string
	require.NoError(t, h.pool.QueryRow(ctx,
		`SELECT id FROM machines WHERE pool_id = $1 AND status = 'idle'`, poolID).Scan(&mid))

	// Drift: a running assignment on the machine, but its status is idle past TTL.
	_, err = h.pool.Exec(ctx, `
		INSERT INTO step_assignments (step_id, workflow_id, run_id, step_name, attempt,
			pool_id, machine_id, status, cpu_millis, memory_mb, payload)
		VALUES (gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 'live-step', 0,
			$1, $2, 'running', 1000, 1024, '{}')`, poolID, mid)
	require.NoError(t, err)
	_, err = h.pool.Exec(ctx,
		`UPDATE machines SET status = 'idle', idle_since = now() - interval '1 hour' WHERE id = $1`, mid)
	require.NoError(t, err)

	// minWarm=0, so only the active-work guard protects it.
	reaped, err := h.fleet.ScaleDown(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, reaped, "a machine holding live work must not be scaled down")
	assert.Equal(t, 1, countMachines(t, h, poolID, "idle"), "the machine survives")
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
