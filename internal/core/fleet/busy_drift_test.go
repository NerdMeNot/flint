package fleet_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// busyFixture parks a machine in 'busy' with its last status change `agoSecs`
// in the past, so the drift grace can be exercised.
func busyFixture(t *testing.T, h *harness, poolName string, agoSecs int) string {
	t.Helper()
	machineID, _ := registerRaceFixture(t, h, poolName)
	_, err := h.pool.Exec(context.Background(),
		`UPDATE machines SET status='busy', updated_at = now() - make_interval(secs := $2)
		 WHERE id=$1`, machineID, agoSecs)
	require.NoError(t, err)
	return machineID
}

// busy → idle happens in exactly one place — CompleteAssignment, when the last
// assignment finishes — and nothing checked the result. A machine that missed it
// was stranded: scale-down only considers idle machines, and a healthy agent
// keeps renewing its lease so the heartbeat sweep never sees it either. It stayed
// busy and kept billing, with no path back.
func TestReconcileBusyDrift_ReturnsAStrandedMachineToIdle(t *testing.T) {
	h, _ := elasticHarness(t, "busy-drift")
	ctx := context.Background()
	machineID := busyFixture(t, h, "busy-drift-elastic", 600)

	n, err := h.fleet.ReconcileBusyDrift(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, "idle", machineStatus(t, h, machineID),
		"a machine marked busy with no work must become schedulable again")
}

// The correction must never touch a machine that is genuinely working.
func TestReconcileBusyDrift_LeavesAMachineHoldingWorkAlone(t *testing.T) {
	h, _ := elasticHarness(t, "busy-drift-live")
	ctx := context.Background()
	machineID := busyFixture(t, h, "busy-drift-live-elastic", 600)

	poolID := poolIDByName(t, h, "busy-drift-live-elastic")
	_, err := h.pool.Exec(ctx, `
		INSERT INTO step_assignments (step_id, workflow_id, run_id, step_name, attempt,
			pool_id, machine_id, status, cpu_millis, memory_mb, payload)
		VALUES (gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 's', 0,
			$1, $2, 'running', 100, 128, '{}')`, poolID, machineID)
	require.NoError(t, err)

	n, err := h.fleet.ReconcileBusyDrift(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, "busy", machineStatus(t, h, machineID))
}

// The grace window keeps the sweep clear of the ordinary gap between one
// assignment finishing and the next being bound.
func TestReconcileBusyDrift_RespectsTheGraceWindow(t *testing.T) {
	h, _ := elasticHarness(t, "busy-drift-grace")
	ctx := context.Background()
	machineID := busyFixture(t, h, "busy-drift-grace-elastic", 1)

	n, err := h.fleet.ReconcileBusyDrift(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "a machine that just went busy must be left alone")
	assert.Equal(t, "busy", machineStatus(t, h, machineID))
}

// Terminating is only for capacity that is idle, drained, or still coming up.
// A busy machine has to be drained first — stating that here turns a future
// "terminate now" caller into a clear error rather than a puzzling
// illegal-transition one.
func TestTerminate_RefusesABusyMachine(t *testing.T) {
	h, _ := elasticHarness(t, "terminate-busy")
	ctx := context.Background()
	machineID := busyFixture(t, h, "terminate-busy-elastic", 600)

	// Give it live work so the drift sweep won't reclassify it first.
	poolID := poolIDByName(t, h, "terminate-busy-elastic")
	_, err := h.pool.Exec(ctx, `
		INSERT INTO step_assignments (step_id, workflow_id, run_id, step_name, attempt,
			pool_id, machine_id, status, cpu_millis, memory_mb, payload)
		VALUES (gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 's', 0,
			$1, $2, 'running', 100, 128, '{}')`, poolID, machineID)
	require.NoError(t, err)

	// TerminateDrained must not pick it up — it is busy, not draining.
	n, err := h.fleet.TerminateDrained(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, "busy", machineStatus(t, h, machineID))
}
