package engine

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// TestPauseResume verifies that a paused workflow queues no new work — claiming
// skips its steps and advancement does not queue downstream — and that resume
// continues the run.
func TestPauseResume(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)
	eng := New(pool, nil)
	defer eng.Close()
	q := db.New(pool)

	waves := [][]pipeline.Step{
		{{Name: "build", Image: "alpine:3.19", Run: pipeline.Cmd("echo")}},
		{{Name: "test", Image: "alpine:3.19", Run: pipeline.Cmd("echo"), DependsOn: []string{"build"}}},
	}
	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
	}, waves)
	require.NoError(t, err)

	status := func(name string) string {
		var s string
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT status FROM steps WHERE workflow_id=$1 AND name=$2 ORDER BY attempt DESC LIMIT 1",
			wfID, name).Scan(&s))
		return s
	}
	wfStatus := func() string {
		ws, err := q.GetWorkflowStatus(ctx, wfID)
		require.NoError(t, err)
		return ws.Status
	}

	// Pause the running workflow.
	require.NoError(t, eng.PauseWorkflow(ctx, wfID))
	assert.Equal(t, "paused", wfStatus())

	// A claim attempt must skip the paused workflow's queued 'build' step.
	claimed, err := q.ClaimQueuedSteps(ctx, 10)
	require.NoError(t, err)
	for _, cs := range claimed {
		assert.NotEqual(t, wfID, cs.WorkflowID, "paused workflow's steps must not be claimed")
	}
	assert.Equal(t, "queued", status("build"), "build stays queued while paused")

	// Completing build while paused records the result but does NOT queue test.
	_, err = pool.Exec(ctx, "UPDATE steps SET status='running' WHERE workflow_id=$1 AND name='build'", wfID)
	require.NoError(t, err)
	tok := EncodeTaskToken(TaskToken{WorkflowID: wfID, StepName: "build", Attempt: 0})
	require.NoError(t, eng.CompleteStep(ctx, tok, StepResult{StepName: "build", Success: true}))
	assert.Equal(t, "succeeded", status("build"))
	assert.Equal(t, "pending", status("test"), "test must NOT be queued while paused")

	// Resume → test becomes eligible (build succeeded) and is queued.
	require.NoError(t, eng.ResumeWorkflow(ctx, wfID))
	assert.Equal(t, "running", wfStatus())
	assert.Equal(t, "queued", status("test"), "test queued after resume")

	// Pausing a non-running... resuming a non-paused workflow is a no-op.
	require.NoError(t, eng.ResumeWorkflow(ctx, wfID)) // already running
	assert.Equal(t, "running", wfStatus())
}
