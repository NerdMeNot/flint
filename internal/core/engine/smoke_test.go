package engine

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// TestSmoke_SimStack is the headless equivalent of `task dev-sim`'s worker: the
// REAL PgEngine + the REAL worker Loop (timers, claim/dispatch, signals, sweep)
// driving runs to completion against a live Postgres via the sim executor — no
// Kubernetes. It exercises the hardening changes end-to-end over real ticks and
// real backoff timing.
//
// Opt-in (it sleeps for real backoff windows): run with
//
//	SMOKE=1 go test ./internal/core/engine/ -run TestSmoke_SimStack -v
func TestSmoke_SimStack(t *testing.T) {
	if os.Getenv("SMOKE") != "1" {
		t.Skip("set SMOKE=1 to run the dev-sim smoke")
	}
	pool := internalTestDB(t)
	eng := New(pool, nil)
	defer eng.Close()

	sim := NewSimExecutor(eng.CompleteStep, nil)
	loop := NewLoop(eng, ExecutorRegistry{"run": sim, "use": sim, "steps": sim},
		LoopConfig{PollInterval: 200 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = loop.Run(ctx) }()
	t.Log("▶ real engine + worker loop started (sim executor, no k8s)")

	t.Run("retry honours backoff", func(t *testing.T) { smokeRetryBackoff(t, pool, eng) })
	t.Run("wait step resolves on signal", func(t *testing.T) { smokeWaitSignal(t, pool, eng) })
	t.Run("cancel tears a run down", func(t *testing.T) { smokeCancel(t, pool, eng) })
	cancel()
}

// smokeRetryBackoff: a flaky step (fails attempt 1, succeeds attempt 2) with a 2s
// backoff. We assert it ends succeeded AND that the second attempt was actually
// delayed by the backoff (the bug we fixed would have retried instantly).
func smokeRetryBackoff(t *testing.T, pool *pgxpool.Pool, eng *PgEngine) {
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)

	waves := [][]pipeline.Step{{{
		Name: "build", Image: "alpine:3.19", Run: pipeline.Cmd("build"),
		Retry: &pipeline.RetrySpec{Attempts: 2, Delay: "2s"},
		Env:   map[string]string{"SIM_FLAKY_UNTIL": "2", "SIM_DURATION": "200ms"},
	}}}
	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
	}, waves)
	require.NoError(t, err)

	start := time.Now()
	st := waitForWorkflow(t, eng, wfID, func(s *WorkflowState) bool { return isTerminalWF(s.Status) }, 20*time.Second)
	elapsed := time.Since(start)

	require.Equal(t, "succeeded", st.Status, "flaky step should pass on its retry")
	b := stepByName(st, "build")
	require.Equal(t, "succeeded", b.Status)
	require.Equal(t, 1, b.Attempt, "should have succeeded on attempt index 1 (the retry)")
	require.GreaterOrEqual(t, elapsed, 1500*time.Millisecond,
		"retry must wait out the ~2s backoff, not fire instantly (regression guard)")
	t.Logf("✓ retry succeeded on attempt %d after %s (backoff honoured)", b.Attempt, elapsed.Round(100*time.Millisecond))
}

// smokeWaitSignal: a wait step parks the run until an external signal arrives.
func smokeWaitSignal(t *testing.T, pool *pgxpool.Pool, eng *PgEngine) {
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)

	waves := [][]pipeline.Step{
		{{Name: "approval", Wait: &pipeline.WaitSpec{Signal: "deploy-ok", Timeout: "1h"}}},
		{{Name: "deploy", Image: "alpine:3.19", Run: pipeline.Cmd("deploy"),
			Env: map[string]string{"SIM_DURATION": "200ms"}, DependsOn: []string{"approval"}}},
	}
	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
	}, waves)
	require.NoError(t, err)

	// The loop should park the wait step in 'waiting'.
	waitForWorkflow(t, eng, wfID, func(s *WorkflowState) bool {
		return stepByName(s, "approval").Status == "waiting"
	}, 10*time.Second)
	t.Log("✓ wait step parked in 'waiting'")

	require.NoError(t, eng.DeliverSignal(ctx, wfID, "deploy-ok", map[string]any{"by": "smoke"}))
	t.Log("→ delivered external signal 'deploy-ok'")

	st := waitForWorkflow(t, eng, wfID, func(s *WorkflowState) bool { return isTerminalWF(s.Status) }, 15*time.Second)
	require.Equal(t, "succeeded", st.Status)
	require.Equal(t, "succeeded", stepByName(st, "deploy").Status, "downstream should run after the signal")
	t.Log("✓ signal resolved the wait; downstream deploy ran to success")
}

// smokeCancel: a long-running step is cancelled mid-flight.
func smokeCancel(t *testing.T, pool *pgxpool.Pool, eng *PgEngine) {
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)

	waves := [][]pipeline.Step{{{
		Name: "long", Image: "alpine:3.19", Run: pipeline.Cmd("sleep"),
		Env: map[string]string{"SIM_DURATION": "30s"},
	}}}
	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "manual",
	}, waves)
	require.NoError(t, err)

	waitForWorkflow(t, eng, wfID, func(s *WorkflowState) bool {
		return stepByName(s, "long").Status == "running"
	}, 10*time.Second)
	t.Log("✓ long step running")

	require.NoError(t, eng.CancelWorkflow(ctx, wfID))
	st := waitForWorkflow(t, eng, wfID, func(s *WorkflowState) bool { return s.Status == "cancelled" }, 10*time.Second)
	require.Equal(t, "cancelled", st.Status)
	t.Log("✓ run cancelled cleanly mid-flight")
}

func isTerminalWF(s string) bool { return s == "succeeded" || s == "failed" || s == "cancelled" }

func waitForWorkflow(t *testing.T, eng *PgEngine, wfID string, pred func(*WorkflowState) bool, timeout time.Duration) *WorkflowState {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		st, err := eng.QueryWorkflow(context.Background(), wfID)
		require.NoError(t, err)
		if pred(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for workflow %s (status=%s)", wfID, st.Status)
		}
		time.Sleep(150 * time.Millisecond)
	}
}
