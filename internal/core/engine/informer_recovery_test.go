package engine

import (
	"context"
	"testing"

	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hangingExecutor dispatches successfully but never reports completion —
// simulates an agent that crashed after the Job was created.
type hangingExecutor struct{}

func (hangingExecutor) Kind() string { return "hanging" }
func (hangingExecutor) Dispatch(context.Context, claimedStep) (string, error) {
	return "job-hanging", nil
}

// TestInformerSignal_PromptConsumption is the regression guard for the
// crash-recovery gap: when an agent dies without calling /internal/complete,
// the K8s informer delivers a step-result signal — the loop must consume it on
// the next tick (processStepResultSignals), not hours later at the step's
// timeout sweep.
func TestInformerSignal_PromptConsumption(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)

	eng := New(pool, nil)
	defer eng.Close()

	// a → b: a's failure must skip b and fail the workflow.
	waves := [][]pipeline.Step{
		{{Name: "a", Image: "alpine:3.19", Run: pipeline.Cmd("echo a")}},
		{{Name: "b", Image: "alpine:3.19", Run: pipeline.Cmd("echo b"), DependsOn: []string{"a"}}},
	}
	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
	}, waves)
	require.NoError(t, err)

	// Claim + dispatch a; the executor "creates the Job" but the agent never
	// reports back.
	loop := NewLoop(eng, ExecutorRegistry{"run": hangingExecutor{}}, LoopConfig{ClaimBatchSize: 1000})
	loop.claimAndDispatchSimple(ctx)

	state, err := eng.QueryWorkflow(ctx, wfID)
	require.NoError(t, err)
	require.Equal(t, "running", stepByName(state, "a").Status)

	// The informer notices the Job failed and delivers the exact payload shape
	// the informer handlers send (success MUST be a real bool — a string here
	// was silently dropped by the typed unmarshal).
	require.NoError(t, eng.DeliverSignal(ctx, wfID, "step-result", map[string]any{
		"stepName": "a",
		"runID":    runID,
		"success":  false,
		"reason":   "job failed (detected by informer)",
	}))

	// One tick phase — not the 2h sweep — must resolve the crashed step.
	loop.processStepResultSignals(ctx)

	state, err = eng.QueryWorkflow(ctx, wfID)
	require.NoError(t, err)
	assert.Equal(t, "failed", stepByName(state, "a").Status, "informer signal should fail the crashed step")
	assert.Equal(t, "skipped", stepByName(state, "b").Status, "downstream step should be skipped")
	assert.Equal(t, "failed", state.Status, "workflow should reach a terminal state")

	// The signal must be consumed — no unconsumed step-result signals left.
	var unconsumed int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM signals WHERE workflow_id=$1 AND consumed=false`, wfID).Scan(&unconsumed))
	assert.Equal(t, 0, unconsumed)
}

// TestInformerSignal_SuccessAdvancesDownstream covers the success path: a
// completed Job whose agent crashed before reporting still advances the DAG.
func TestInformerSignal_SuccessAdvancesDownstream(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)

	eng := New(pool, nil)
	defer eng.Close()

	waves := [][]pipeline.Step{
		{{Name: "a", Image: "alpine:3.19", Run: pipeline.Cmd("echo a")}},
		{{Name: "b", Image: "alpine:3.19", Run: pipeline.Cmd("echo b"), DependsOn: []string{"a"}}},
	}
	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
	}, waves)
	require.NoError(t, err)

	loop := NewLoop(eng, ExecutorRegistry{"run": hangingExecutor{}}, LoopConfig{ClaimBatchSize: 1000})
	loop.claimAndDispatchSimple(ctx)

	require.NoError(t, eng.DeliverSignal(ctx, wfID, "step-result", map[string]any{
		"stepName": "a",
		"runID":    runID,
		"success":  true,
	}))

	loop.processStepResultSignals(ctx)

	state, err := eng.QueryWorkflow(ctx, wfID)
	require.NoError(t, err)
	assert.Equal(t, "succeeded", stepByName(state, "a").Status)
	assert.Equal(t, "queued", stepByName(state, "b").Status, "downstream step should be queued for dispatch")
}
