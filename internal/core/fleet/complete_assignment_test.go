package fleet_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A8a: a redelivered completion must be idempotent. The first CompleteAssignment
// finalizes the assignment, bumps steps_completed, and moves the machine
// busy→idle; a second (duplicate) call must be a no-op — FinishAssignment affects
// 0 rows, so steps_completed and the machine's state must not drift.
func TestCompleteAssignment_DuplicateDoesNotDriftStats(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	joinToken := h.seedPool(t, "complete-idem")
	machineID, _ := h.register(t, joinToken)

	// Machine is busy running one assignment.
	assignmentID := h.insertAssignment(t, machineID, "running")
	_, err := h.pool.Exec(ctx, `UPDATE machines SET status = 'busy' WHERE id = $1`, machineID)
	require.NoError(t, err)

	// First completion: finalizes, counts, and releases the machine to idle.
	require.NoError(t, h.fleet.CompleteAssignment(ctx, assignmentID, machineID, "succeeded", nil))
	m, err := h.q.GetMachine(ctx, machineID)
	require.NoError(t, err)
	assert.Equal(t, int32(1), m.StepsCompleted, "first completion counts once")
	assert.Equal(t, "idle", m.Status, "machine released to idle")

	// Duplicate completion (redelivered result): no-op.
	require.NoError(t, h.fleet.CompleteAssignment(ctx, assignmentID, machineID, "succeeded", nil))
	m, err = h.q.GetMachine(ctx, machineID)
	require.NoError(t, err)
	assert.Equal(t, int32(1), m.StepsCompleted, "duplicate completion must not double-count")
	assert.Equal(t, "idle", m.Status)
}
