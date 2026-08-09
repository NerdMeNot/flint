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

// TestStepTransitionAllowed pins the step-status transition table so an illegal
// move (e.g. resurrecting a terminal step, or queued→succeeded skipping running)
// is rejected rather than silently corrupting state.
func TestStepTransitionAllowed(t *testing.T) {
	legal := []struct{ from, to string }{
		{stepPending, stepQueued},
		{stepPending, stepSkipped},
		{stepPending, stepCancelled},
		{stepQueued, stepRunning},
		{stepQueued, stepWaiting},
		{stepQueued, stepPending}, // pause re-queue
		{stepRunning, stepSucceeded},
		{stepRunning, stepFailed},
		{stepWaiting, stepSucceeded},
		{stepWaiting, stepFailed},
		{stepRetryWait, stepQueued},
		{stepRunning, stepRunning}, // no-op idempotent
	}
	for _, c := range legal {
		assert.Truef(t, stepTransitionAllowed(c.from, c.to), "expected %s→%s legal", c.from, c.to)
	}

	illegal := []struct{ from, to string }{
		{stepSucceeded, stepRunning}, // terminal is final
		{stepFailed, stepQueued},
		{stepCancelled, stepRunning},
		{stepPending, stepRunning},   // must be claimed via queued
		{stepQueued, stepSucceeded},  // must run first
		{stepRetryWait, stepRunning}, // must re-queue first
	}
	for _, c := range illegal {
		assert.Falsef(t, stepTransitionAllowed(c.from, c.to), "expected %s→%s illegal", c.from, c.to)
	}
}

func TestWorkflowTransitionAllowed(t *testing.T) {
	assert.True(t, workflowTransitionAllowed(wfRunning, wfPaused))
	assert.True(t, workflowTransitionAllowed(wfPaused, wfRunning))
	assert.True(t, workflowTransitionAllowed(wfRunning, wfSucceeded))
	assert.False(t, workflowTransitionAllowed(wfSucceeded, wfRunning))
	assert.False(t, workflowTransitionAllowed(wfPaused, wfSucceeded)) // resume first
}

// TestEngineEvents_EmittedOnLifecycle runs a tiny two-wave workflow to completion
// and asserts the durable history log captured the key transitions.
func TestEngineEvents_EmittedOnLifecycle(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)
	eng := New(pool, nil)
	defer eng.Close()

	waves := [][]pipeline.Step{
		{{Name: "build", Image: "alpine:3.19", Run: pipeline.Cmd("echo")}},
		{{Name: "test", Image: "alpine:3.19", Run: pipeline.Cmd("echo"), DependsOn: []string{"build"}}},
	}
	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
	}, waves)
	require.NoError(t, err)

	// Drive both steps to completion the way an executor would: claim then complete.
	q := db.New(pool)
	complete := func(name string) {
		_, err := pool.Exec(ctx, "UPDATE steps SET status='running' WHERE workflow_id=$1 AND name=$2", wfID, name)
		require.NoError(t, err)
		tok := EncodeTaskToken(TaskToken{WorkflowID: wfID, StepName: name, Attempt: 0})
		require.NoError(t, eng.CompleteStep(ctx, tok, StepResult{StepName: name, Success: true}))
	}
	complete("build")
	complete("test")

	events, err := q.ListEngineEventsByWorkflow(ctx, wfID)
	require.NoError(t, err)

	types := map[string]int{}
	for _, e := range events {
		types[e.EventType]++
	}
	// build+test both queued and succeeded; workflow finished once.
	assert.GreaterOrEqual(t, types["queued"], 2, "expected queued events for both steps")
	assert.GreaterOrEqual(t, types["succeeded"], 2, "expected succeeded events for both steps")
	assert.Equal(t, 1, types["workflow_finished"], "expected one workflow_finished event")

	// Step-scoped events carry the step name; workflow events do not.
	for _, e := range events {
		if e.EventType == "workflow_finished" {
			assert.Nil(t, e.StepName)
			assert.Equal(t, wfSucceeded, deref(e.ToStatus))
		}
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
