package engine

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A7: a workflow stuck 'running' with every step terminal (the finish was lost
// to a worker crash) must be recovered through the REAL finish path — derived
// verdict, FinishRun, workflow_finished event, and webhook enqueue — not the old
// bare UPDATE that stranded the run 'running', skipped the webhook, and always
// recorded 'failed'. This covers the all-succeeded case: the verdict must be
// 'succeeded', which the old sweep got wrong.
func TestLoop_FinishStaleWorkflow_SucceededVerdictAndFinishesRun(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)

	eng := New(pool, nil)
	defer eng.Close()

	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID,
		Repo: "acme/test", Ref: "main", CommitSHA: "abc123",
		TriggerType: "manual", TriggeredBy: "test",
	}, wavesSingle("a"))
	require.NoError(t, err)

	// A webhook subscribed to run.completed proves the finish path enqueues it.
	_, err = pool.Exec(ctx,
		`INSERT INTO webhooks (project_id, url, events) VALUES ($1, $2, '["run.completed"]'::jsonb)`,
		projectID, "https://example.test/hook")
	require.NoError(t, err)

	// Simulate the lost finish: all steps succeeded, but workflow + run still
	// 'running' — exactly the crash window the sweep recovers.
	_, err = pool.Exec(ctx, `UPDATE steps SET status = 'succeeded' WHERE workflow_id = $1`, wfID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE workflows SET status = 'running', finished_at = NULL WHERE id = $1`, wfID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE pipeline_runs SET status = 'running', finished_at = NULL WHERE id = $1`, runID)
	require.NoError(t, err)

	loop := NewLoop(eng, ExecutorRegistry{}, LoopConfig{})
	loop.finishStaleWorkflows(ctx)

	var wfStatus, runStatus string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM workflows WHERE id = $1`, wfID).Scan(&wfStatus))
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM pipeline_runs WHERE id = $1`, runID).Scan(&runStatus))
	assert.Equal(t, "succeeded", wfStatus, "verdict is derived from steps, not hardcoded 'failed'")
	assert.Equal(t, "succeeded", runStatus, "run is finished, not stranded 'running'")

	// workflow_finished event emitted.
	var events int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM engine_events WHERE workflow_id = $1 AND event_type = 'workflow_finished'`,
		wfID).Scan(&events))
	assert.Positive(t, events, "workflow_finished event recorded")

	// Webhook enqueued via the outbox (the bare UPDATE never did this).
	var outbox int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM flint_outbox WHERE event_type = 'webhook' AND idempotency_key LIKE '%' || $1 || '%'`,
		runID).Scan(&outbox))
	assert.Positive(t, outbox, "run.completed webhook enqueued for the recovered run")
}

// A7: the verdict is a global verdict — a single failed step fails the recovered
// workflow even though the rest succeeded.
func TestLoop_FinishStaleWorkflow_FailedVerdict(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)

	eng := New(pool, nil)
	defer eng.Close()

	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID,
		Repo: "acme/test", Ref: "main", CommitSHA: "abc123",
		TriggerType: "manual", TriggeredBy: "test",
	}, wavesSingle("a"))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE steps SET status = 'failed' WHERE workflow_id = $1`, wfID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE workflows SET status = 'running', finished_at = NULL WHERE id = $1`, wfID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE pipeline_runs SET status = 'running', finished_at = NULL WHERE id = $1`, runID)
	require.NoError(t, err)

	loop := NewLoop(eng, ExecutorRegistry{}, LoopConfig{})
	loop.finishStaleWorkflows(ctx)

	var wfStatus, runStatus string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM workflows WHERE id = $1`, wfID).Scan(&wfStatus))
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM pipeline_runs WHERE id = $1`, runID).Scan(&runStatus))
	assert.Equal(t, "failed", wfStatus)
	assert.Equal(t, "failed", runStatus)
}

// A7 safety: a freshly-'running' workflow whose steps haven't been created yet
// must NOT be finished — "no non-terminal steps" is vacuously true then, and the
// has-steps guard is what keeps the sweep from finishing a workflow mid-startup.
func TestLoop_FinishStaleWorkflow_SkipsWorkflowWithNoSteps(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)

	eng := New(pool, nil)
	defer eng.Close()

	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID,
		Repo: "acme/test", Ref: "main", CommitSHA: "abc123",
		TriggerType: "manual", TriggeredBy: "test",
	}, wavesSingle("a"))
	require.NoError(t, err)

	// Delete the steps to model the pre-first-advance window, keep workflow running.
	_, err = pool.Exec(ctx, `DELETE FROM steps WHERE workflow_id = $1`, wfID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE workflows SET status = 'running', finished_at = NULL WHERE id = $1`, wfID)
	require.NoError(t, err)

	loop := NewLoop(eng, ExecutorRegistry{}, LoopConfig{})
	loop.finishStaleWorkflows(ctx)

	var wfStatus string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM workflows WHERE id = $1`, wfID).Scan(&wfStatus))
	assert.Equal(t, "running", wfStatus, "a workflow with no steps yet must not be finished by the sweep")
}
