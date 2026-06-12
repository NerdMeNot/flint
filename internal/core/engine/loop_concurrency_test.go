package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEngine_ConcurrentCompleteStep completes a wide wave of steps from many
// goroutines at once, calling CompleteStep twice per step. The per-workflow
// FOR UPDATE lock must serialize advancement and the isTerminal guard must make
// completion idempotent: every step finishes exactly once and the workflow ends
// succeeded. Run under -race, this also guards against data races in advance.
func TestEngine_ConcurrentCompleteStep(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)
	eng := New(pool, nil)
	defer eng.Close()

	const n = 8
	wave := make([]pipeline.Step, n)
	for i := range wave {
		wave[i] = pipeline.Step{Name: fmt.Sprintf("s%d", i), Image: "alpine:3.19", Run: pipeline.Cmd("echo")}
	}
	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
	}, [][]pipeline.Step{wave})
	require.NoError(t, err)

	// Claim every step (queued → running) so completion is valid.
	_, err = pool.Exec(ctx, "UPDATE steps SET status='running' WHERE workflow_id=$1", wfID)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("s%d", i)
		tok := EncodeTaskToken(TaskToken{WorkflowID: wfID, StepName: name, Attempt: 0})
		for r := 0; r < 2; r++ { // double-complete to exercise idempotency
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = eng.CompleteStep(ctx, tok, StepResult{StepName: name, Success: true})
			}()
		}
	}
	wg.Wait()

	state, err := eng.QueryWorkflow(ctx, wfID)
	require.NoError(t, err)
	assert.Equal(t, "succeeded", state.Status)
	require.Len(t, state.Steps, n)
	for _, s := range state.Steps {
		assert.Equal(t, "succeeded", s.Status, "step %s", s.Name)
	}
}

// TestEngine_ConcurrentStart_Idempotent starts the same run from many goroutines
// at once. The partial unique index on workflows(run_id) WHERE parent_id IS NULL
// must ensure exactly one root workflow exists regardless of races (losing
// callers error; that's fine — the run is created once).
func TestEngine_ConcurrentStart_Idempotent(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)
	eng := New(pool, nil)
	defer eng.Close()

	waves := wavesSingle("a")
	const n = 6
	var wg sync.WaitGroup
	var mu sync.Mutex
	okCount := 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
				RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
			}, waves); err == nil {
				mu.Lock()
				okCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM workflows WHERE run_id=$1", runID).Scan(&count))
	assert.Equal(t, 1, count, "concurrent starts must create exactly one root workflow")
	assert.GreaterOrEqual(t, okCount, 1, "at least one start must succeed")
}

// TestEngine_ConcurrentGateApproval processes a gate approval signal from several
// goroutines at once. Concurrent signal processing must converge: the gate ends
// succeeded exactly once and the downstream step is queued — no corruption.
func TestEngine_ConcurrentGateApproval(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)
	eng := New(pool, nil)
	defer eng.Close()

	waves := [][]pipeline.Step{
		{{Name: "approve", Gate: &pipeline.Gate{Approvers: []string{"role:admin"}}}},
		{{Name: "deploy", Image: "alpine:3.19", Run: pipeline.Cmd("echo"), DependsOn: []string{"approve"}}},
	}
	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
	}, waves)
	require.NoError(t, err)

	// Move the gate to 'waiting' (as ClaimQueuedSteps would) and deliver approval.
	_, err = pool.Exec(ctx, "UPDATE steps SET status='waiting' WHERE workflow_id=$1 AND name='approve'", wfID)
	require.NoError(t, err)
	require.NoError(t, eng.DeliverSignal(ctx, wfID, "gate-approve", map[string]string{"by": "admin"}))

	loop := NewLoop(eng, ExecutorRegistry{}, LoopConfig{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); loop.processSignals(ctx) }()
	}
	wg.Wait()

	state, err := eng.QueryWorkflow(ctx, wfID)
	require.NoError(t, err)
	steps := map[string]string{}
	for _, s := range state.Steps {
		steps[s.Name] = s.Status
	}
	assert.Equal(t, "succeeded", steps["approve"], "gate should be approved exactly once")
	assert.Equal(t, "queued", steps["deploy"], "downstream step should advance after approval")

	// The approval signal must be consumed (not left for reprocessing).
	var unconsumed int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM signals WHERE workflow_id=$1 AND consumed=false", wfID).Scan(&unconsumed))
	assert.Equal(t, 0, unconsumed)
}
