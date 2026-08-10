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

// startOneStep creates a running workflow with a single step and returns its id.
func startOneStep(t *testing.T, pool *pgxpool.Pool, eng *PgEngine) string {
	t.Helper()
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)

	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
	}, wavesSingle("a"))
	require.NoError(t, err)
	return wfID
}

// advanceOnce runs one advancement pass the way the loop would.
func advanceOnce(t *testing.T, pool *pgxpool.Pool, wfID string) error {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := advanceWorkflow(ctx, db.New(pool).WithTx(tx), wfID, 0); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func latestStep(t *testing.T, pool *pgxpool.Pool, wfID string) (status string, attempt int) {
	t.Helper()
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT status, attempt FROM steps WHERE workflow_id=$1 AND name='a'
		 ORDER BY attempt DESC LIMIT 1`, wfID).Scan(&status, &attempt))
	return
}

func sendMachineLost(t *testing.T, eng *PgEngine, wfID string, attempt *int) {
	t.Helper()
	payload := map[string]any{
		"stepName": "a", "success": false, "reason": "machine lost",
	}
	if attempt != nil {
		payload["attempt"] = *attempt
	}
	require.NoError(t, eng.DeliverSignal(context.Background(), wfID, "step-result", payload))
}

// The fleet reports a machine death as a step-result signal. It has to land
// whatever in-flight state the step is in — a step can be back in 'queued' after
// an undispatched claim was re-queued while its assignment was still live.
//
// This used to vanish: queued→failed was not a legal transition, the resulting
// error was swallowed, and the signal was consumed anyway — so the step sat
// queued until the deadline sweep hours later, which is exactly the wait the
// signal exists to eliminate.
func TestMachineLostSignal_FailsStepInAnyLiveState(t *testing.T) {
	for _, state := range []string{"queued", "running"} {
		t.Run("step is "+state, func(t *testing.T) {
			pool := internalTestDB(t)
			quiesceQueue(t, pool)
			eng := New(pool, nil)
			defer eng.Close()

			wfID := startOneStep(t, pool, eng)
			_, err := pool.Exec(context.Background(),
				`UPDATE steps SET status=$2 WHERE workflow_id=$1 AND name='a'`, wfID, state)
			require.NoError(t, err)

			zero := 0
			sendMachineLost(t, eng, wfID, &zero)
			require.NoError(t, advanceOnce(t, pool, wfID))

			status, _ := latestStep(t, pool, wfID)
			assert.Equal(t, "failed", status, "a machine-lost signal must resolve the step")
		})
	}
}

// The signal names the attempt it is about. One that arrives for an execution
// that has already been superseded must be ignored, not applied to whatever
// attempt happens to be current — that would fail an attempt which never ran.
func TestMachineLostSignal_IgnoresSupersededAttempt(t *testing.T) {
	pool := internalTestDB(t)
	quiesceQueue(t, pool)
	eng := New(pool, nil)
	defer eng.Close()

	wfID := startOneStep(t, pool, eng)
	// Pretend attempt 0 already failed and attempt 1 is parked awaiting backoff.
	_, err := pool.Exec(context.Background(),
		`UPDATE steps SET status='failed', finished_at=now() WHERE workflow_id=$1 AND name='a'`, wfID)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO steps (workflow_id, name, exec_type, status, wave, attempt,
		     max_attempts, step_def, timeout_seconds)
		 VALUES ($1, 'a', 'run', 'retry_wait', 0, 1, 3, '{}'::jsonb, 3600)`, wfID)
	require.NoError(t, err)

	// A late signal about the DEAD attempt 0 arrives.
	zero := 0
	sendMachineLost(t, eng, wfID, &zero)
	require.NoError(t, advanceOnce(t, pool, wfID))

	status, attempt := latestStep(t, pool, wfID)
	assert.Equal(t, 1, attempt)
	assert.Equal(t, "retry_wait", status,
		"a stale signal must not fail the parked retry that replaced its attempt")
}

// A signal written before the attempt field existed still has to work.
func TestMachineLostSignal_WithoutAttemptAppliesToLiveStep(t *testing.T) {
	pool := internalTestDB(t)
	quiesceQueue(t, pool)
	eng := New(pool, nil)
	defer eng.Close()

	wfID := startOneStep(t, pool, eng)
	_, err := pool.Exec(context.Background(),
		`UPDATE steps SET status='running' WHERE workflow_id=$1 AND name='a'`, wfID)
	require.NoError(t, err)

	sendMachineLost(t, eng, wfID, nil)
	require.NoError(t, advanceOnce(t, pool, wfID))

	status, _ := latestStep(t, pool, wfID)
	assert.Equal(t, "failed", status)
}

// The backstop for the one state nothing else sweeps: a parked retry whose
// backoff timer is gone would otherwise wait forever.
func TestSweep_RequeuesRetryStepWithNoTimer(t *testing.T) {
	ctx := context.Background()
	pool := internalTestDB(t)
	quiesceQueue(t, pool)
	eng := New(pool, nil)
	defer eng.Close()

	wfID := startOneStep(t, pool, eng)
	// A retry parked long ago, with no timer to release it.
	_, err := pool.Exec(ctx,
		`UPDATE steps SET status='retry_wait', created_at = now() - interval '1 hour'
		 WHERE workflow_id=$1 AND name='a'`, wfID)
	require.NoError(t, err)

	n, err := db.New(pool).RequeueOrphanedRetrySteps(ctx, orphanedRetryGrace.Seconds())
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	status, _ := latestStep(t, pool, wfID)
	assert.Equal(t, "queued", status, "a retry with no timer must be released, not stranded")
}

// The same backstop must leave a legitimately-waiting retry alone.
func TestSweep_LeavesRetryStepWithLiveTimerParked(t *testing.T) {
	ctx := context.Background()
	pool := internalTestDB(t)
	quiesceQueue(t, pool)
	eng := New(pool, nil)
	defer eng.Close()

	wfID := startOneStep(t, pool, eng)
	_, err := pool.Exec(ctx,
		`UPDATE steps SET status='retry_wait', created_at = now() - interval '1 hour'
		 WHERE workflow_id=$1 AND name='a'`, wfID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO timers (workflow_id, step_name, timer_type, fires_at, fired)
		 VALUES ($1, 'a', 'retry_backoff', now() + interval '10 minutes', false)`, wfID)
	require.NoError(t, err)

	n, err := db.New(pool).RequeueOrphanedRetrySteps(ctx, orphanedRetryGrace.Seconds())
	require.NoError(t, err)
	assert.EqualValues(t, 0, n, "a retry still waiting on its backoff must stay parked")

	status, _ := latestStep(t, pool, wfID)
	assert.Equal(t, "retry_wait", status)
}
