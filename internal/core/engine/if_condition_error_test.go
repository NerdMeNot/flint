package engine

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// TestIfConditionError_FailsLoudly is the regression guard for the silent-skip
// bug: an if: expression that errors at evaluation time (unknown identifier,
// type error) must FAIL the step with a diagnostic — not skip it silently as
// if the expression had evaluated false.
func TestIfConditionError_FailsLoudly(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)

	eng := New(pool, nil)
	defer eng.Close()

	waves := [][]pipeline.Step{
		{{Name: "broken", Image: "alpine:3.19", Run: pipeline.Cmd("echo hi"),
			If: "${{ nonexistent_context == 'main' }}"}},
		{{Name: "downstream", Image: "alpine:3.19", Run: pipeline.Cmd("echo bye"),
			DependsOn: []string{"broken"}}},
	}
	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
	}, waves)
	require.NoError(t, err)

	state, err := eng.QueryWorkflow(ctx, wfID)
	require.NoError(t, err)

	broken := stepByName(state, "broken")
	assert.Equal(t, "failed", broken.Status, "an erroring if: must fail the step, not skip it")
	assert.Equal(t, "skipped", stepByName(state, "downstream").Status)
	assert.Equal(t, "failed", state.Status, "the workflow must surface the failure")

	// The failure must carry a diagnostic naming the expression.
	var reason string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT coalesce(reason, '') FROM engine_events
		 WHERE workflow_id=$1 AND step_name='broken' AND to_status='failed'
		 ORDER BY created_at DESC LIMIT 1`, wfID).Scan(&reason))
	assert.Contains(t, reason, "failed to evaluate", "the event should explain the eval error")
	assert.Contains(t, reason, "nonexistent_context", "the event should name the expression")
}

// TestIfConditionFalse_StillSkips confirms the legitimate skip path is intact:
// an expression that cleanly evaluates false skips the step.
func TestIfConditionFalse_StillSkips(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)

	eng := New(pool, nil)
	defer eng.Close()

	waves := [][]pipeline.Step{
		{{Name: "gated", Image: "alpine:3.19", Run: pipeline.Cmd("echo hi"),
			If: `${{ env.DEPLOY == "yes" }}`}},
	}
	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
		Env: map[string]string{"DEPLOY": "no"},
	}, waves)
	require.NoError(t, err)

	state, err := eng.QueryWorkflow(ctx, wfID)
	require.NoError(t, err)
	assert.Equal(t, "skipped", stepByName(state, "gated").Status)
	assert.Equal(t, "succeeded", state.Status, "a skipped step is not a failure")
}
