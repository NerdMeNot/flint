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

// TestResolveStepManually forces a wedged running step to succeeded and verifies
// the workflow advances, the event is attributed to the operator, and a late
// real completion for the same step is a safe no-op.
func TestResolveStepManually(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)
	eng := New(pool, nil)
	defer eng.Close()
	q := db.New(pool)

	waves := [][]pipeline.Step{
		{{Name: "stuck", Image: "alpine:3.19", Run: pipeline.Cmd("sleep")}},
		{{Name: "next", Image: "alpine:3.19", Run: pipeline.Cmd("echo"), DependsOn: []string{"stuck"}}},
	}
	wfID, err := eng.StartWorkflowSeeded(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
	}, waves, nil)
	require.NoError(t, err)

	status := func(name string) string {
		var s string
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT status FROM steps WHERE workflow_id=$1 AND name=$2 ORDER BY attempt DESC LIMIT 1",
			wfID, name).Scan(&s))
		return s
	}

	// Simulate 'stuck' claimed and running but never completing.
	_, err = pool.Exec(ctx, "UPDATE steps SET status='running' WHERE workflow_id=$1 AND name='stuck'", wfID)
	require.NoError(t, err)

	// Operator forces it succeeded.
	require.NoError(t, eng.ResolveStepManually(ctx, wfID, "stuck", "succeeded", "ops@flint.dev", "executor wedged"))
	assert.Equal(t, "succeeded", status("stuck"))
	assert.Equal(t, "queued", status("next"), "downstream released after manual resolve")

	// History event attributes the operator + reason.
	events, err := q.ListEngineEventsByStep(ctx, db.ListEngineEventsByStepParams{
		WorkflowID: wfID, StepName: strptr("stuck"),
	})
	require.NoError(t, err)
	var found bool
	for _, e := range events {
		if e.EventType == "manual_resolve" {
			found = true
			assert.Equal(t, "operator:ops@flint.dev", e.Actor)
			assert.Equal(t, "executor wedged", deref(e.Reason))
		}
	}
	assert.True(t, found, "expected a manual_resolve event")

	// A late real completion for the same step is idempotent (terminal guard).
	tok := EncodeTaskToken(TaskToken{WorkflowID: wfID, StepName: "stuck", Attempt: 0})
	require.NoError(t, eng.CompleteStep(ctx, tok, StepResult{StepName: "stuck", Success: false}))
	assert.Equal(t, "succeeded", status("stuck"), "late completion must not override manual resolution")

	// Resolving an already-terminal step errors.
	err = eng.ResolveStepManually(ctx, wfID, "stuck", "failed", "ops@flint.dev", "")
	assert.Error(t, err)
}
