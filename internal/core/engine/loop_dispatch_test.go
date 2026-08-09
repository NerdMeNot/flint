package engine

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/dbkit"
	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// erroringExecutor always fails Dispatch — simulates a step that cannot be
// scheduled (bad image, pool resolution failure, transient K8s API error).
type erroringExecutor struct{}

func (erroringExecutor) Kind() string { return "erroring" }
func (erroringExecutor) Dispatch(context.Context, claimedStep) (string, error) {
	return "", errors.New("simulated dispatch failure")
}

// internalTestDB resolves the shared test Postgres (exported by TestMain via
// FLINT_TEST_DSN) for package-internal tests. Skips when unavailable, matching
// the external harness's behaviour.
func internalTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	dsn := os.Getenv("FLINT_TEST_DSN")
	if dsn == "" {
		t.Skip("no FLINT_TEST_DSN (initdb/pg_ctl) available")
	}
	require.NoError(t, dbkit.RunMigrations(dsn, dbkit.Migrations, "migrations"))
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// seedRun creates the minimum org/forge/project/pipeline_run rows for a run.
func seedRun(t *testing.T, pool *pgxpool.Pool, runID string) (orgID, projectID string) {
	t.Helper()
	ctx := context.Background()
	orgID = uuid.NewString()
	projectID = uuid.NewString()
	forgeID := uuid.NewString()

	_, err := pool.Exec(ctx,
		`INSERT INTO orgs (id, name, slug) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		orgID, "test-org-"+orgID[:8], "test-"+orgID[:8])
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO forge_connections (id, org_id, forge_type, display_name, webhook_secret, credentials_enc)
		 VALUES ($1, $2, 'github', 'test-forge', 'test-secret', $3) ON CONFLICT DO NOTHING`,
		forgeID, orgID, []byte{})
	require.NoError(t, err)
	wsID := uuid.NewString()
	_, err = pool.Exec(ctx,
		`INSERT INTO workspaces (id, org_id, name, slug, is_default)
		 VALUES ($1, $2, 'Default', 'default', true) ON CONFLICT DO NOTHING`,
		wsID, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO projects (id, org_id, forge_id, workspace_id, display_name, repo_path, repo_url)
		 VALUES ($1, $2, $3, $4, 'test-project', 'acme/test', 'https://example.com/acme/test') ON CONFLICT DO NOTHING`,
		projectID, orgID, forgeID, wsID)
	require.NoError(t, err)

	q := db.New(pool)
	pf := "ci.yaml"
	ref := "main"
	by := "test"
	require.NoError(t, q.InsertPipelineRun(ctx, db.InsertPipelineRunParams{
		ID: runID, ProjectID: &projectID, OrgID: orgID,
		WorkflowFile: &pf, TriggerType: "manual", TriggerRef: &ref, TriggeredBy: &by,
	}))
	return orgID, projectID
}

// wavesSingle builds a one-step, one-wave DAG for tests.
func wavesSingle(name string) [][]pipeline.Step {
	return [][]pipeline.Step{{{Name: name, Image: "alpine:3.19", Run: pipeline.Cmd("echo " + name)}}}
}

// TestLoop_DispatchFailure_AdvancesWorkflow is the regression guard for the
// dispatch-failure wedge: when a step cannot be dispatched, the workflow must
// reach a terminal state (the failed step terminal, the downstream step skipped)
// rather than hanging forever in 'running'.
func TestLoop_DispatchFailure_AdvancesWorkflow(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)

	eng := New(pool, nil)
	defer eng.Close()

	// Two waves: b depends on a. a will fail to dispatch.
	waves := [][]pipeline.Step{
		{{Name: "a", Image: "alpine:3.19", Run: pipeline.Cmd("echo a")}},
		{{Name: "b", Image: "alpine:3.19", Run: pipeline.Cmd("echo b"), DependsOn: []string{"a"}}},
	}

	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID,
		Repo: "acme/test", Ref: "main", CommitSHA: "abc123",
		TriggerType: "manual", TriggeredBy: "test",
	}, waves)
	require.NoError(t, err)

	// ClaimQueuedSteps claims a global batch ordered by queued_at; sibling tests
	// in the shared DB may leave queued steps, so use a large batch to guarantee
	// this run's just-queued step is included.
	loop := NewLoop(eng, ExecutorRegistry{"run": erroringExecutor{}}, LoopConfig{ClaimBatchSize: 1000})

	// One synchronous dispatch pass: claims 'a' (queued→running), dispatch fails,
	// failStepAndAdvance marks it failed and advances the workflow.
	loop.claimAndDispatchSimple(ctx)

	state, err := eng.QueryWorkflow(ctx, wfID)
	require.NoError(t, err)
	steps := map[string]StepState{}
	for _, s := range state.Steps {
		steps[s.Name] = s
	}
	assert.Equal(t, "failed", steps["a"].Status, "dispatched step should be failed")
	assert.Equal(t, "skipped", steps["b"].Status, "downstream step should be skipped, not wedged in pending")
	assert.Equal(t, "failed", state.Status, "workflow should terminate, not hang in running")
}
