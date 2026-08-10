package engine

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// retryableWaves is one step declaring three attempts — the policy every path
// below is checked against.
func retryableWaves(name string) [][]pipeline.Step {
	return [][]pipeline.Step{{{
		Name: name, Image: "alpine:3.19", Run: pipeline.Cmd("echo " + name),
		Retry: &pipeline.RetrySpec{Attempts: 3},
	}}}
}

// startRetryable creates a run whose single step is already 'running', ready to
// be failed by whichever path the test exercises.
func startRetryable(t *testing.T, pool *pgxpool.Pool, eng *PgEngine) string {
	t.Helper()
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)

	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
	}, retryableWaves("a"))
	require.NoError(t, err)

	_, err = pool.Exec(ctx,
		`UPDATE steps SET status='running', started_at=now() WHERE workflow_id=$1 AND name='a'`, wfID)
	require.NoError(t, err)
	return wfID
}

func attemptRows(t *testing.T, pool *pgxpool.Pool, wfID string) []struct {
	Attempt int
	Status  string
} {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT attempt, status FROM steps WHERE workflow_id=$1 AND name='a' ORDER BY attempt`, wfID)
	require.NoError(t, err)
	defer rows.Close()

	var out []struct {
		Attempt int
		Status  string
	}
	for rows.Next() {
		var r struct {
			Attempt int
			Status  string
		}
		require.NoError(t, rows.Scan(&r.Attempt, &r.Status))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

// `retry:` must mean the same thing however the step failed. It previously meant
// "unless it timed out": the timeout timer and the deadline sweep both wrote the
// step row directly and never consulted the policy, so the single failure mode
// people most often add retries for was the one that ignored them.
func TestRetryPolicy_AppliesToEveryFailurePath(t *testing.T) {
	ctx := context.Background()

	t.Run("agent reports a non-zero exit", func(t *testing.T) {
		pool := internalTestDB(t)
		eng := New(pool, nil)
		defer eng.Close()
		wfID := startRetryable(t, pool, eng)

		require.NoError(t, eng.CompleteStep(ctx,
			EncodeTaskToken(TaskToken{WorkflowID: wfID, StepName: "a", Attempt: 0}),
			StepResult{StepName: "a", Success: false, ExitCode: 1, Error: "boom"}))

		got := attemptRows(t, pool, wfID)
		require.Len(t, got, 2, "a second attempt must be parked")
		assert.Equal(t, "failed", got[0].Status)
		assert.Equal(t, "retry_wait", got[1].Status,
			"the next attempt waits on its backoff timer, not the queue")
	})

	t.Run("execution timeout timer fires", func(t *testing.T) {
		pool := internalTestDB(t)
		eng := New(pool, nil)
		defer eng.Close()
		wfID := startRetryable(t, pool, eng)

		_, err := pool.Exec(ctx,
			`INSERT INTO timers (workflow_id, step_name, timer_type, fires_at)
			 VALUES ($1, 'a', 'timeout', now()-interval '1 second')`, wfID)
		require.NoError(t, err)
		require.NoError(t, fireTimers(ctx, pool))

		got := attemptRows(t, pool, wfID)
		require.Len(t, got, 2, "a timed-out step with attempts left must retry")
		assert.Equal(t, "failed", got[0].Status)
		assert.Equal(t, "retry_wait", got[1].Status)
	})

	t.Run("deadline sweep reaps it", func(t *testing.T) {
		pool := internalTestDB(t)
		eng := New(pool, nil)
		defer eng.Close()
		wfID := startRetryable(t, pool, eng)

		_, err := pool.Exec(ctx,
			`UPDATE steps SET deadline_at = now() - interval '1 minute' WHERE workflow_id=$1 AND name='a'`, wfID)
		require.NoError(t, err)

		loop := NewLoop(eng, ExecutorRegistry{}, LoopConfig{})
		n, err := loop.failStaleRunningSteps(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, n)

		got := attemptRows(t, pool, wfID)
		require.Len(t, got, 2, "a swept step with attempts left must retry")
		assert.Equal(t, "failed", got[0].Status)
		assert.Equal(t, "retry_wait", got[1].Status)
	})
}

// The mirror: attempts are still finite. A step on its last attempt must fail
// for good down every path, or a hung step would retry forever.
func TestRetryPolicy_ExhaustedAttemptsStayFailed(t *testing.T) {
	ctx := context.Background()
	pool := internalTestDB(t)
	eng := New(pool, nil)
	defer eng.Close()
	wfID := startRetryable(t, pool, eng)

	// Jump the step to its final attempt.
	_, err := pool.Exec(ctx,
		`UPDATE steps SET attempt = max_attempts - 1 WHERE workflow_id=$1 AND name='a'`, wfID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx,
		`INSERT INTO timers (workflow_id, step_name, timer_type, fires_at)
		 VALUES ($1, 'a', 'timeout', now()-interval '1 second')`, wfID)
	require.NoError(t, err)
	require.NoError(t, fireTimers(ctx, pool))

	got := attemptRows(t, pool, wfID)
	require.Len(t, got, 1, "the last attempt must not spawn another")
	assert.Equal(t, "failed", got[0].Status)
}

// A timer that fires after the step already resolved must be a no-op, not a
// second failure written over a succeeded step.
func TestTimeoutTimer_NoOpWhenStepAlreadyResolved(t *testing.T) {
	ctx := context.Background()
	pool := internalTestDB(t)
	eng := New(pool, nil)
	defer eng.Close()
	wfID := startRetryable(t, pool, eng)

	require.NoError(t, eng.CompleteStep(ctx,
		EncodeTaskToken(TaskToken{WorkflowID: wfID, StepName: "a", Attempt: 0}),
		StepResult{StepName: "a", Success: true, ExitCode: 0}))

	_, err := pool.Exec(ctx,
		`INSERT INTO timers (workflow_id, step_name, timer_type, fires_at)
		 VALUES ($1, 'a', 'timeout', now()-interval '1 second')`, wfID)
	require.NoError(t, err)
	require.NoError(t, fireTimers(ctx, pool))

	got := attemptRows(t, pool, wfID)
	require.Len(t, got, 1)
	assert.Equal(t, "succeeded", got[0].Status, "a late timer must not overwrite a finished step")
}
