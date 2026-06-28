package engine

import (
	"context"
	"testing"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRetryBackoff_NotBypassed is the regression guard for the retry-backoff bug:
// a failed step with attempts remaining must park the retry in 'retry_wait' (NOT
// queue it immediately) so the backoff timer's delay is honoured, and only the
// fired retry_backoff timer promotes it to 'queued'.
func TestRetryBackoff_NotBypassed(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)
	q := db.New(pool)

	eng := New(pool, nil)
	defer eng.Close()

	// One step, two attempts, with a long backoff so it can't fire on its own.
	waves := [][]pipeline.Step{
		{{Name: "a", Image: "alpine:3.19", Run: pipeline.Cmd("echo a"),
			Retry: &pipeline.RetrySpec{Attempts: 2, Delay: "60s"}}},
	}
	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
	}, waves)
	require.NoError(t, err)

	// Dispatch fails → attempt 0 failed, attempt 1 parked in retry_wait + backoff timer.
	loop := NewLoop(eng, ExecutorRegistry{"run": erroringExecutor{}}, LoopConfig{ClaimBatchSize: 1000})
	loop.claimAndDispatchSimple(ctx)

	state, err := eng.QueryWorkflow(ctx, wfID)
	require.NoError(t, err)
	a := stepByName(state, "a")
	assert.Equal(t, "retry_wait", a.Status, "retry attempt must be parked, not queued")
	assert.Equal(t, 1, a.Attempt, "latest attempt should be the retry")
	assert.Equal(t, "running", state.Status, "workflow keeps running during backoff")

	// A retry_backoff timer must exist and the step must NOT be queued yet.
	var queuedCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM steps WHERE workflow_id=$1 AND name='a' AND status='queued'`, wfID).Scan(&queuedCount))
	assert.Equal(t, 0, queuedCount, "step must not be queued while backing off")

	var timerCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM timers WHERE workflow_id=$1 AND step_name='a' AND timer_type='retry_backoff' AND fired=false`, wfID).Scan(&timerCount))
	assert.Equal(t, 1, timerCount, "a retry_backoff timer should be pending")

	// Make the backoff timer due and fire it → the parked attempt becomes queued.
	_, err = pool.Exec(ctx,
		`UPDATE timers SET fires_at = now() - interval '1 second' WHERE workflow_id=$1 AND timer_type='retry_backoff'`, wfID)
	require.NoError(t, err)
	require.NoError(t, fireTimers(ctx, pool))

	state, err = eng.QueryWorkflow(ctx, wfID)
	require.NoError(t, err)
	assert.Equal(t, "queued", stepByName(state, "a").Status, "fired backoff timer should re-queue the retry")

	_ = q
}

// TestRequeueUndispatchedSteps recovers a step claimed (running) but never
// dispatched — the worker crashed between claim and Job creation.
func TestRequeueUndispatchedSteps(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)
	q := db.New(pool)

	eng := New(pool, nil)
	defer eng.Close()

	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
	}, wavesSingle("a"))
	require.NoError(t, err)

	// Simulate a claim that never dispatched: running, started a while ago, a
	// deadline already ticking, but dispatched_at IS NULL.
	_, err = pool.Exec(ctx,
		`UPDATE steps SET status='running', started_at=now()-interval '5 minutes',
		 deadline_at=now()+interval '1 hour', dispatched_at=NULL WHERE workflow_id=$1 AND name='a'`, wfID)
	require.NoError(t, err)

	n, err := q.RequeueUndispatchedSteps(ctx, 60) // 60s grace
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, int64(1))

	var status string
	var deadline *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, deadline_at::text FROM steps WHERE workflow_id=$1 AND name='a'`, wfID).Scan(&status, &deadline))
	assert.Equal(t, "queued", status, "undispatched step should be re-queued")
	assert.Nil(t, deadline, "deadline must be cleared on re-queue")
}

// TestSetStepQueued_ClearsDeadline guards the throttle/requeue stale-deadline bug:
// re-queuing a step must wipe the deadline it carried from an earlier claim.
func TestSetStepQueued_ClearsDeadline(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)
	q := db.New(pool)

	eng := New(pool, nil)
	defer eng.Close()

	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
	}, wavesSingle("a"))
	require.NoError(t, err)

	var stepID string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id FROM steps WHERE workflow_id=$1 AND name='a'`, wfID).Scan(&stepID))
	_, err = pool.Exec(ctx,
		`UPDATE steps SET status='running', started_at=now(), deadline_at=now()+interval '2 hours' WHERE id=$1`, stepID)
	require.NoError(t, err)

	require.NoError(t, q.SetStepQueued(ctx, stepID))

	var deadline, started, dispatched *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT deadline_at::text, started_at::text, dispatched_at::text FROM steps WHERE id=$1`, stepID).
		Scan(&deadline, &started, &dispatched))
	assert.Nil(t, deadline, "deadline_at must be cleared")
	assert.Nil(t, started, "started_at must be cleared")
	assert.Nil(t, dispatched, "dispatched_at must be cleared")
}

// TestRecoverStaleOutboxEvents returns events stranded in 'processing' (worker
// crashed mid-delivery) back to 'pending'.
func TestRecoverStaleOutboxEvents(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	q := db.New(pool)

	id := uuid.NewString()
	key := "test-" + id
	_, err := pool.Exec(ctx,
		`INSERT INTO flint_outbox (id, event_type, payload, status, attempts, idempotency_key, claimed_at)
		 VALUES ($1, 'webhook', '{}'::jsonb, 'processing', 1, $2, now()-interval '10 minutes')`, id, key)
	require.NoError(t, err)

	n, err := q.RecoverStaleOutboxEvents(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, int64(1))

	var status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM flint_outbox WHERE id=$1`, id).Scan(&status))
	assert.Equal(t, "pending", status)
}

// TestTimeoutTimer_FiresAtomically verifies the atomic timer path: a due timeout
// timer fails its running step and is then marked fired.
func TestTimeoutTimer_FiresAtomically(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)

	eng := New(pool, nil)
	defer eng.Close()

	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
	}, wavesSingle("a"))
	require.NoError(t, err)

	_, err = pool.Exec(ctx,
		`UPDATE steps SET status='running', started_at=now() WHERE workflow_id=$1 AND name='a'`, wfID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO timers (workflow_id, step_name, timer_type, fires_at)
		 VALUES ($1, 'a', 'timeout', now()-interval '1 second')`, wfID)
	require.NoError(t, err)

	require.NoError(t, fireTimers(ctx, pool))

	state, err := eng.QueryWorkflow(ctx, wfID)
	require.NoError(t, err)
	assert.Equal(t, "failed", stepByName(state, "a").Status, "timed-out step should be failed")

	var fired bool
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT fired FROM timers WHERE workflow_id=$1 AND timer_type='timeout'`, wfID).Scan(&fired))
	assert.True(t, fired, "timer should be marked fired after handling")
}

// TestWaitStep_SignalResolves exercises the generic external-signal wait: the step
// parks in 'waiting', an external signal resolves it (capturing the payload into
// its outputs), and the downstream step proceeds.
func TestWaitStep_SignalResolves(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)

	eng := New(pool, nil)
	defer eng.Close()

	waves := [][]pipeline.Step{
		{{Name: "approval", Wait: &pipeline.WaitSpec{Signal: "deploy-ok", Timeout: "1h"}}},
		{{Name: "after", Image: "alpine:3.19", Run: pipeline.Cmd("echo after"), DependsOn: []string{"approval"}}},
	}
	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
	}, waves)
	require.NoError(t, err)

	// Claim parks the wait step in 'waiting' (no executor needed).
	loop := NewLoop(eng, ExecutorRegistry{}, LoopConfig{ClaimBatchSize: 1000})
	loop.claimAndDispatchSimple(ctx)

	state, err := eng.QueryWorkflow(ctx, wfID)
	require.NoError(t, err)
	assert.Equal(t, "waiting", stepByName(state, "approval").Status, "wait step should park in waiting")

	// A wait_timeout timer should have been created.
	var timerCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM timers WHERE workflow_id=$1 AND step_name='approval' AND timer_type='wait_timeout'`, wfID).Scan(&timerCount))
	assert.Equal(t, 1, timerCount)

	// Deliver the external signal with a payload, then resolve.
	require.NoError(t, eng.DeliverSignal(ctx, wfID, "deploy-ok", map[string]any{"version": "1.2.3"}))
	loop.processSignalWaits(ctx)

	state, err = eng.QueryWorkflow(ctx, wfID)
	require.NoError(t, err)
	assert.Equal(t, "succeeded", stepByName(state, "approval").Status, "signaled wait step should succeed")
	assert.Equal(t, "queued", stepByName(state, "after").Status, "downstream step should proceed")

	// The signal payload should be captured into the step's outputs.
	var version *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT result->'outputs'->>'version' FROM steps WHERE workflow_id=$1 AND name='approval'`, wfID).Scan(&version))
	require.NotNil(t, version)
	assert.Equal(t, "1.2.3", *version, "wait signal payload should land in step outputs")
}

// TestWaitStep_Timeout verifies a wait step that never receives its signal fails
// when its wait_timeout fires.
func TestWaitStep_Timeout(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)

	eng := New(pool, nil)
	defer eng.Close()

	waves := [][]pipeline.Step{
		{{Name: "approval", Wait: &pipeline.WaitSpec{Signal: "never", Timeout: "1h"}}},
	}
	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
	}, waves)
	require.NoError(t, err)

	loop := NewLoop(eng, ExecutorRegistry{}, LoopConfig{ClaimBatchSize: 1000})
	loop.claimAndDispatchSimple(ctx)

	// Force the wait_timeout due and fire it.
	_, err = pool.Exec(ctx,
		`UPDATE timers SET fires_at=now()-interval '1 second' WHERE workflow_id=$1 AND timer_type='wait_timeout'`, wfID)
	require.NoError(t, err)
	require.NoError(t, fireTimers(ctx, pool))

	state, err := eng.QueryWorkflow(ctx, wfID)
	require.NoError(t, err)
	assert.Equal(t, "failed", stepByName(state, "approval").Status, "timed-out wait step should fail")
	assert.Equal(t, "failed", state.Status, "workflow should terminate")
}

// stepByName returns the named step from a workflow state snapshot.
func stepByName(state *WorkflowState, name string) StepState {
	for _, s := range state.Steps {
		if s.Name == name {
			return s
		}
	}
	return StepState{}
}
