package engine

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// TestDAGGating_PerEdgeParallelism proves the engine gates on a step's own
// dependencies, not on the whole previous wave. Two independent chains a→b and
// c→d share wave levels (a,c in wave 0; b,d in wave 1). Completing only c must
// release d immediately, while b stays pending because a is unfinished. Under the
// old wave-barrier model d would have been blocked until a finished too.
func TestDAGGating_PerEdgeParallelism(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)
	eng := New(pool, nil)
	defer eng.Close()

	waves := [][]pipeline.Step{
		{
			{Name: "a", Image: "alpine:3.19", Run: pipeline.Cmd("echo")},
			{Name: "c", Image: "alpine:3.19", Run: pipeline.Cmd("echo")},
		},
		{
			{Name: "b", Image: "alpine:3.19", Run: pipeline.Cmd("echo"), DependsOn: []string{"a"}},
			{Name: "d", Image: "alpine:3.19", Run: pipeline.Cmd("echo"), DependsOn: []string{"c"}},
		},
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

	// Wave 0 (a, c) both queued; wave 1 (b, d) pending.
	assert.Equal(t, "queued", status("a"))
	assert.Equal(t, "queued", status("c"))
	assert.Equal(t, "pending", status("b"))
	assert.Equal(t, "pending", status("d"))

	// Complete ONLY c (a remains unfinished).
	_, err = pool.Exec(ctx, "UPDATE steps SET status='running' WHERE workflow_id=$1 AND name='c'", wfID)
	require.NoError(t, err)
	tok := EncodeTaskToken(TaskToken{WorkflowID: wfID, StepName: "c", Attempt: 0})
	require.NoError(t, eng.CompleteStep(ctx, tok, StepResult{StepName: "c", Success: true}))

	// d's dependency (c) is terminal → d is released even though a is unfinished.
	assert.Equal(t, "queued", status("d"), "d should be released as soon as c completes")
	// b's dependency (a) is not terminal → b stays pending.
	assert.Equal(t, "pending", status("b"), "b must wait for a")
	assert.Equal(t, "queued", status("a"), "a is unaffected")
}

// TestDAGGating_DependencyScopedWhen proves `when:` is scoped to a step's own
// upstream, not the whole pipeline: a failure in branch a→b must NOT skip the
// independent branch c→d. (Under the old global semantics, d would wrongly skip.)
func TestDAGGating_DependencyScopedWhen(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)
	eng := New(pool, nil)
	defer eng.Close()

	waves := [][]pipeline.Step{
		{
			{Name: "a", Image: "alpine:3.19", Run: pipeline.Cmd("echo")},
			{Name: "c", Image: "alpine:3.19", Run: pipeline.Cmd("echo")},
		},
		{
			{Name: "b", Image: "alpine:3.19", Run: pipeline.Cmd("echo"), DependsOn: []string{"a"}},
			{Name: "d", Image: "alpine:3.19", Run: pipeline.Cmd("echo"), DependsOn: []string{"c"}},
		},
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
	complete := func(name string, ok bool) {
		_, err := pool.Exec(ctx, "UPDATE steps SET status='running' WHERE workflow_id=$1 AND name=$2", wfID, name)
		require.NoError(t, err)
		tok := EncodeTaskToken(TaskToken{WorkflowID: wfID, StepName: name, Attempt: 0})
		require.NoError(t, eng.CompleteStep(ctx, tok, StepResult{StepName: name, Success: ok}))
	}

	complete("a", false) // branch a→b fails
	complete("c", true)  // branch c→d succeeds

	assert.Equal(t, "skipped", status("b"), "b skipped because its ancestor a failed")
	assert.Equal(t, "queued", status("d"), "d runs — its branch is unaffected by a's failure")
}

// TestDAGGating_TransitiveFailurePropagates verifies onFailure fires on a step
// whose DIRECT dependency was skipped, when the failure is further upstream
// (a→b→c, a fails → b skipped → c[onFailure] must still run).
func TestDAGGating_TransitiveFailurePropagates(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)
	eng := New(pool, nil)
	defer eng.Close()

	waves := [][]pipeline.Step{
		{{Name: "a", Image: "alpine:3.19", Run: pipeline.Cmd("echo")}},
		{{Name: "b", Image: "alpine:3.19", Run: pipeline.Cmd("echo"), DependsOn: []string{"a"}}},
		{{Name: "c", Image: "alpine:3.19", Run: pipeline.Cmd("echo"), DependsOn: []string{"b"}, When: "onFailure"}},
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

	_, err = pool.Exec(ctx, "UPDATE steps SET status='running' WHERE workflow_id=$1 AND name='a'", wfID)
	require.NoError(t, err)
	tok := EncodeTaskToken(TaskToken{WorkflowID: wfID, StepName: "a", Attempt: 0})
	require.NoError(t, eng.CompleteStep(ctx, tok, StepResult{StepName: "a", Success: false}))

	assert.Equal(t, "skipped", status("b"), "b skipped (onSuccess, ancestor a failed)")
	assert.Equal(t, "queued", status("c"), "c runs (onFailure) — transitive failure via skipped b")
}

// TestDAGGating_FailedDepSkipsDownstream verifies onSuccess downstream is skipped
// when an upstream fails, while onFailure downstream runs — under per-edge gating.
func TestDAGGating_FailedDepSkipsDownstream(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)
	eng := New(pool, nil)
	defer eng.Close()

	waves := [][]pipeline.Step{
		{{Name: "build", Image: "alpine:3.19", Run: pipeline.Cmd("echo")}},
		{
			{Name: "deploy", Image: "alpine:3.19", Run: pipeline.Cmd("echo"), DependsOn: []string{"build"}},
			{Name: "alert", Image: "alpine:3.19", Run: pipeline.Cmd("echo"), DependsOn: []string{"build"}, When: "onFailure"},
		},
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

	// Fail build.
	_, err = pool.Exec(ctx, "UPDATE steps SET status='running' WHERE workflow_id=$1 AND name='build'", wfID)
	require.NoError(t, err)
	tok := EncodeTaskToken(TaskToken{WorkflowID: wfID, StepName: "build", Attempt: 0})
	require.NoError(t, eng.CompleteStep(ctx, tok, StepResult{StepName: "build", Success: false}))

	assert.Equal(t, "skipped", status("deploy"), "onSuccess downstream skipped after upstream failure")
	assert.Equal(t, "queued", status("alert"), "onFailure downstream runs after upstream failure")
}
