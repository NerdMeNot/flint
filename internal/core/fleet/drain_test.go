package fleet_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/fleet"
)

// drainFixture creates a machine in the given pool, parked in `status`.
func drainFixture(t *testing.T, h *harness, poolName, status string) string {
	t.Helper()
	machineID, _ := registerRaceFixture(t, h, poolName)
	_, err := h.pool.Exec(context.Background(),
		`UPDATE machines SET status=$2, idle_since=now() WHERE id=$1`, machineID, status)
	require.NoError(t, err)
	return machineID
}

func machineStatus(t *testing.T, h *harness, machineID string) string {
	t.Helper()
	var s string
	require.NoError(t, h.pool.QueryRow(context.Background(),
		`SELECT status FROM machines WHERE id=$1`, machineID).Scan(&s))
	return s
}

// Draining means "finish what you have and stop". Once nothing is left, the
// machine has done what was asked and must be terminated.
//
// Nothing used to complete a drain: scale-down only considers idle machines, so
// a drained machine sat in 'draining' — still billing — until its agent stopped
// heartbeating, was marked 'lost', and was reaped by reconciliation. A graceful
// operation reaching its end state through the failure path, having paid for the
// wait.
func TestTerminateDrained_TerminatesAnElasticMachineWithNoWork(t *testing.T) {
	h, _ := elasticHarness(t, "drain-pool")
	ctx := context.Background()
	machineID := drainFixture(t, h, "drain-pool-elastic", "draining")

	n, err := h.fleet.TerminateDrained(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, "terminated", machineStatus(t, h, machineID))
}

// A drain must not cut work short — that is the whole difference between drain
// and terminate.
func TestTerminateDrained_LeavesAMachineStillHoldingWork(t *testing.T) {
	h, _ := elasticHarness(t, "drain-busy-pool")
	ctx := context.Background()
	machineID := drainFixture(t, h, "drain-busy-pool-elastic", "draining")

	poolID := poolIDByName(t, h, "drain-busy-pool-elastic")
	_, err := h.pool.Exec(ctx, `
		INSERT INTO step_assignments (step_id, workflow_id, run_id, step_name, attempt,
			pool_id, machine_id, status, cpu_millis, memory_mb, payload)
		VALUES (gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 's', 0,
			$1, $2, 'running', 100, 128, '{}')`, poolID, machineID)
	require.NoError(t, err)

	n, err := h.fleet.TerminateDrained(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, "draining", machineStatus(t, h, machineID),
		"a draining machine must keep its in-flight work")
}

// Flint doesn't own a static machine's power button, so 'draining' is where it
// rests until an operator acts.
func TestTerminateDrained_NeverTerminatesStaticCapacity(t *testing.T) {
	h, _ := elasticHarness(t, "drain-static-pool")
	ctx := context.Background()
	machineID := drainFixture(t, h, "drain-static-pool-elastic", "draining")
	_, err := h.pool.Exec(ctx, `UPDATE machines SET provider='static' WHERE id=$1`, machineID)
	require.NoError(t, err)

	n, err := h.fleet.TerminateDrained(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, "draining", machineStatus(t, h, machineID))
}

// The drain an agent sends when its spot instance is about to be reclaimed is
// the one that matters most. It used to be validated against the machine row the
// RPC authenticated with — and the scheduler moves machines idle → busy every
// tick, so that row is routinely stale. The transition failed, the error was
// discarded, and the agent was told to carry on: it kept taking new work until
// the instance was reclaimed mid-step.
func TestHeartbeat_DrainAppliesDespiteStaleAuthenticatedRow(t *testing.T) {
	h, _ := elasticHarness(t, "drain-race")
	ctx := context.Background()
	machineID := drainFixture(t, h, "drain-race-elastic", "idle")

	authed, err := h.q.GetMachine(ctx, machineID)
	require.NoError(t, err)
	require.Equal(t, "idle", authed.Status)

	// The scheduler binds work to it between authentication and the handler.
	_, err = h.pool.Exec(ctx, `UPDATE machines SET status='busy' WHERE id=$1`, machineID)
	require.NoError(t, err)

	res, err := h.fleet.Heartbeat(ctx, authed, fleet.HeartbeatInput{
		DrainRequested: true, DrainReason: "spot interruption notice",
	})
	require.NoError(t, err)

	assert.Equal(t, "draining", machineStatus(t, h, machineID))
	assert.Equal(t, "drain", res.Action, "the agent must be told to stop taking work")
}

// A drain request for a machine already past draining is not an error — there is
// simply nothing left to do.
func TestHeartbeat_DrainIsANoOpOnAMachineAlreadyLeaving(t *testing.T) {
	h, _ := elasticHarness(t, "drain-noop")
	ctx := context.Background()
	machineID := drainFixture(t, h, "drain-noop-elastic", "idle")

	authed, err := h.q.GetMachine(ctx, machineID)
	require.NoError(t, err)

	_, err = h.pool.Exec(ctx, `UPDATE machines SET status='terminating' WHERE id=$1`, machineID)
	require.NoError(t, err)

	res, err := h.fleet.Heartbeat(ctx, authed, fleet.HeartbeatInput{DrainRequested: true})
	require.NoError(t, err, "a drain that no longer applies must not fail the heartbeat")

	assert.Equal(t, "terminating", machineStatus(t, h, machineID))
	assert.Equal(t, "shutdown", res.Action, "a terminating machine is told to shut down, not drain")
}

// The reappearance path shares the same lock-and-re-read helper.
func TestHeartbeat_LostMachineReappears(t *testing.T) {
	h, _ := elasticHarness(t, "revive")
	ctx := context.Background()
	machineID := drainFixture(t, h, "revive-elastic", "lost")

	authed, err := h.q.GetMachine(ctx, machineID)
	require.NoError(t, err)

	res, err := h.fleet.Heartbeat(ctx, authed, fleet.HeartbeatInput{})
	require.NoError(t, err)

	assert.Equal(t, "idle", machineStatus(t, h, machineID), "a machine that heartbeats again is back")
	assert.Equal(t, "continue", res.Action)
}
