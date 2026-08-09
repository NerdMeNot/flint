package engine_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/dbkit"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// testDSN returns the Postgres connection string for integration tests.
//
// Resolution order: skip under -short; FLINT_TEST_DSN if set (used by CI's
// Postgres service container); otherwise the throwaway instance TestMain spins
// up from local initdb/pg_ctl. If none is available, the test is skipped with a
// clear message — never a hard failure.
func testDSN(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	if dsn := os.Getenv("FLINT_TEST_DSN"); dsn != "" {
		return dsn
	}
	if autoDSN != "" {
		return autoDSN
	}
	t.Skip("no FLINT_TEST_DSN and no local postgres (initdb/pg_ctl) available")
	return ""
}

// setupTestDB creates a pool, runs migrations, and returns cleanup functions.
func setupTestDB(t *testing.T) (*pgxpool.Pool, *db.Queries) {
	t.Helper()
	dsn := testDSN(t)
	ctx := context.Background()

	err := dbkit.RunMigrations(dsn, dbkit.Migrations, "migrations")
	require.NoError(t, err, "migrations should apply cleanly")

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err, "pool should connect")
	t.Cleanup(func() { pool.Close() })

	return pool, db.New(pool)
}

// seedOrgAndProject creates the minimum org + project rows needed for a run.
func seedOrgAndProject(t *testing.T, pool *pgxpool.Pool) (orgID, projectID string) {
	t.Helper()
	ctx := context.Background()
	orgID = uuid.NewString()
	projectID = uuid.NewString()
	forgeID := uuid.NewString()

	// orgs.name and .slug are both UNIQUE — make them per-test so repeated seeds
	// in the same test DB don't collide and silently skip via ON CONFLICT.
	_, err := pool.Exec(ctx,
		`INSERT INTO orgs (id, name, slug) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		orgID, "test-org-"+orgID[:8], "test-"+orgID[:8])
	require.NoError(t, err)

	// projects.forge_id is a NOT NULL FK to forge_connections, so seed one first.
	_, err = pool.Exec(ctx,
		`INSERT INTO forge_connections (id, org_id, forge_type, display_name, webhook_secret, credentials_enc)
		 VALUES ($1, $2, 'github', 'test-forge', 'test-secret', $3) ON CONFLICT DO NOTHING`,
		forgeID, orgID, []byte{})
	require.NoError(t, err)

	// Projects belong to exactly one workspace (workspace_id is NOT NULL), so
	// seed a default workspace for the org first.
	wsID := uuid.NewString()
	_, err = pool.Exec(ctx,
		`INSERT INTO workspaces (id, org_id, name, slug, is_default)
		 VALUES ($1, $2, 'Default', 'default', true) ON CONFLICT DO NOTHING`,
		wsID, orgID)
	require.NoError(t, err)

	// Pipeline location now lives in pipeline_source jsonb (defaults to .flint/).
	_, err = pool.Exec(ctx,
		`INSERT INTO projects (id, org_id, forge_id, workspace_id, display_name, repo_path, repo_url)
		 VALUES ($1, $2, $3, $4, 'test-project', 'acme/test', 'https://example.com/acme/test')
		 ON CONFLICT DO NOTHING`,
		projectID, orgID, forgeID, wsID)
	require.NoError(t, err)

	return orgID, projectID
}

// insertTestRun creates a pipeline_runs row for testing.
func insertTestRun(t *testing.T, q *db.Queries, runID, projectID, orgID string) {
	t.Helper()
	err := q.InsertPipelineRun(context.Background(), db.InsertPipelineRunParams{
		ID:           runID,
		ProjectID:    &projectID,
		OrgID:        orgID,
		WorkflowFile: strPtr("ci.yaml"),
		TriggerType:  "manual",
		TriggerRef:   strPtr("main"),
		TriggeredBy:  strPtr("test"),
	})
	require.NoError(t, err)
}

// ─────────────────────────────────────────────────────────────
// Integration tests
// ─────────────────────────────────────────────────────────────

func TestEngine_HappyPath_LinearPipeline(t *testing.T) {
	pool, q := setupTestDB(t)
	ctx := context.Background()
	orgID, projectID := seedOrgAndProject(t, pool)

	pipelineYAML := `
image: alpine:3.19
triggers:
  push:
    branches: [main]
steps:
  - name: build
    run: echo building
  - name: test
    run: echo testing
    dependsOn: [build]
`
	eng := engine.New(pool, nil)
	defer eng.Close()

	runID := uuid.NewString()
	insertTestRun(t, q, runID, projectID, orgID)

	// Start workflow.
	wfID, err := eng.StartWorkflowWithWaves(ctx, engine.StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID,
		Repo: "acme/test", Ref: "main", CommitSHA: "abc123",
		TriggerType: "manual", TriggeredBy: "test",
	}, wavesFromYAML(t, pipelineYAML))
	require.NoError(t, err)
	require.NotEmpty(t, wfID)

	// Query initial state.
	state, err := eng.QueryWorkflow(ctx, wfID)
	require.NoError(t, err)
	assert.Equal(t, "running", state.Status)
	require.Len(t, state.Steps, 2)

	// Build should be queued (wave 0), test should be pending (wave 1).
	stepMap := make(map[string]engine.StepState)
	for _, s := range state.Steps {
		stepMap[s.Name] = s
	}
	assert.Equal(t, "queued", stepMap["build"].Status)
	assert.Equal(t, "pending", stepMap["test"].Status)

	// Complete build step.
	buildToken := engine.EncodeTaskToken(engine.TaskToken{
		WorkflowID: wfID, StepName: "build", Attempt: 0,
	})
	// Simulate claim: set status to running first.
	_, err = pool.Exec(ctx,
		"UPDATE steps SET status = 'running', started_at = now() WHERE workflow_id = $1 AND name = 'build'", wfID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		"UPDATE steps SET task_token = $1 WHERE workflow_id = $2 AND name = 'build'", &buildToken, wfID)
	require.NoError(t, err)

	err = eng.CompleteStep(ctx, buildToken, engine.StepResult{
		StepName: "build", Success: true, ExitCode: 0,
	})
	require.NoError(t, err)

	// After build completes, test should be queued.
	state, err = eng.QueryWorkflow(ctx, wfID)
	require.NoError(t, err)
	stepMap = make(map[string]engine.StepState)
	for _, s := range state.Steps {
		stepMap[s.Name] = s
	}
	assert.Equal(t, "succeeded", stepMap["build"].Status)
	assert.Equal(t, "queued", stepMap["test"].Status)

	// Complete test step.
	testToken := engine.EncodeTaskToken(engine.TaskToken{
		WorkflowID: wfID, StepName: "test", Attempt: 0,
	})
	_, err = pool.Exec(ctx,
		"UPDATE steps SET status = 'running', started_at = now(), task_token = $1 WHERE workflow_id = $2 AND name = 'test'",
		&testToken, wfID)
	require.NoError(t, err)

	err = eng.CompleteStep(ctx, testToken, engine.StepResult{
		StepName: "test", Success: true, ExitCode: 0,
	})
	require.NoError(t, err)

	// Workflow should be complete.
	state, err = eng.QueryWorkflow(ctx, wfID)
	require.NoError(t, err)
	assert.Equal(t, "succeeded", state.Status)
}

// TestEngine_StartWorkflowWithWaves_NoForge proves the product-neutral entry
// point: a workflow built from raw waves runs through the engine end-to-end with
// no forge and no pipeline YAML (engine.New is given a nil FileGetter). This is
// the foundation Flint Workflows builds on.
func TestEngine_StartWorkflowWithWaves_NoForge(t *testing.T) {
	pool, q := setupTestDB(t)
	ctx := context.Background()
	orgID, projectID := seedOrgAndProject(t, pool)
	runID := uuid.NewString()
	insertTestRun(t, q, runID, projectID, orgID)

	eng := engine.New(pool, nil)
	defer eng.Close()

	waves := [][]pipeline.Step{
		{{Name: "a", Run: pipeline.Cmd("echo a")}},
		{{Name: "b", Run: pipeline.Cmd("echo b"), DependsOn: []string{"a"}}},
	}

	wfID, err := eng.StartWorkflowWithWaves(ctx, engine.StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, Kind: "workflow",
	}, waves)
	require.NoError(t, err)
	require.NotEmpty(t, wfID)

	status := func(name string) string {
		s, qerr := eng.QueryWorkflow(ctx, wfID)
		require.NoError(t, qerr)
		for _, st := range s.Steps {
			if st.Name == name {
				return st.Status
			}
		}
		t.Fatalf("step %q not found", name)
		return ""
	}

	require.Len(t, mustQuery(t, eng, wfID).Steps, 2)
	assert.Equal(t, "queued", status("a"), "wave 0 should be queued")
	assert.Equal(t, "pending", status("b"), "wave 1 should be pending")

	complete := func(name string) {
		tok := engine.EncodeTaskToken(engine.TaskToken{WorkflowID: wfID, StepName: name, Attempt: 0})
		_, eerr := pool.Exec(ctx,
			"UPDATE steps SET status='running', started_at=now(), task_token=$1 WHERE workflow_id=$2 AND name=$3",
			&tok, wfID, name)
		require.NoError(t, eerr)
		require.NoError(t, eng.CompleteStep(ctx, tok, engine.StepResult{StepName: name, Success: true}))
	}

	complete("a")
	assert.Equal(t, "queued", status("b"), "completing a should queue b")
	complete("b")

	assert.Equal(t, "succeeded", mustQuery(t, eng, wfID).Status)
}

func mustQuery(t *testing.T, eng engine.Engine, wfID string) *engine.WorkflowState {
	t.Helper()
	s, err := eng.QueryWorkflow(context.Background(), wfID)
	require.NoError(t, err)
	return s
}

func TestEngine_WhenOnFailure(t *testing.T) {
	pool, q := setupTestDB(t)
	ctx := context.Background()
	orgID, projectID := seedOrgAndProject(t, pool)

	pipelineYAML := `
image: alpine:3.19
triggers:
  push:
    branches: [main]
steps:
  - name: build
    run: echo building
  - name: cleanup
    run: echo cleaning up
    dependsOn: [build]
    when: onFailure
  - name: notify
    run: echo notifying
    dependsOn: [build]
    when: always
`
	eng := engine.New(pool, nil)
	defer eng.Close()

	runID := uuid.NewString()
	insertTestRun(t, q, runID, projectID, orgID)

	wfID, err := eng.StartWorkflowWithWaves(ctx, engine.StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID,
		Repo: "acme/test", Ref: "main", CommitSHA: "abc123",
		TriggerType: "manual", TriggeredBy: "test",
	}, wavesFromYAML(t, pipelineYAML))
	require.NoError(t, err)

	// Fail the build step.
	buildToken := engine.EncodeTaskToken(engine.TaskToken{
		WorkflowID: wfID, StepName: "build", Attempt: 0,
	})
	_, err = pool.Exec(ctx,
		"UPDATE steps SET status = 'running', started_at = now(), task_token = $1 WHERE workflow_id = $2 AND name = 'build'",
		&buildToken, wfID)
	require.NoError(t, err)

	err = eng.CompleteStep(ctx, buildToken, engine.StepResult{
		StepName: "build", Success: false, ExitCode: 1, Error: "build failed",
	})
	require.NoError(t, err)

	// After build fails:
	// - cleanup (when: onFailure) should be queued
	// - notify (when: always) should be queued
	state, err := eng.QueryWorkflow(ctx, wfID)
	require.NoError(t, err)

	stepMap := make(map[string]engine.StepState)
	for _, s := range state.Steps {
		stepMap[s.Name] = s
	}
	assert.Equal(t, "failed", stepMap["build"].Status)
	assert.Equal(t, "queued", stepMap["cleanup"].Status, "onFailure step should run when pipeline failed")
	assert.Equal(t, "queued", stepMap["notify"].Status, "always step should run regardless")
}

func TestEngine_WhenOnSuccess_Skipped(t *testing.T) {
	pool, q := setupTestDB(t)
	ctx := context.Background()
	orgID, projectID := seedOrgAndProject(t, pool)

	pipelineYAML := `
image: alpine:3.19
triggers:
  push:
    branches: [main]
steps:
  - name: build
    run: echo building
  - name: deploy
    run: echo deploying
    dependsOn: [build]
  - name: cleanup
    run: echo cleaning up
    dependsOn: [build]
    when: onFailure
`
	eng := engine.New(pool, nil)
	defer eng.Close()

	runID := uuid.NewString()
	insertTestRun(t, q, runID, projectID, orgID)

	wfID, err := eng.StartWorkflowWithWaves(ctx, engine.StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID,
		Repo: "acme/test", Ref: "main", CommitSHA: "abc123",
		TriggerType: "manual", TriggeredBy: "test",
	}, wavesFromYAML(t, pipelineYAML))
	require.NoError(t, err)

	// Succeed the build step.
	buildToken := engine.EncodeTaskToken(engine.TaskToken{
		WorkflowID: wfID, StepName: "build", Attempt: 0,
	})
	_, err = pool.Exec(ctx,
		"UPDATE steps SET status = 'running', started_at = now(), task_token = $1 WHERE workflow_id = $2 AND name = 'build'",
		&buildToken, wfID)
	require.NoError(t, err)

	err = eng.CompleteStep(ctx, buildToken, engine.StepResult{
		StepName: "build", Success: true, ExitCode: 0,
	})
	require.NoError(t, err)

	// After build succeeds:
	// - deploy (default when: onSuccess) should be queued
	// - cleanup (when: onFailure) should be skipped
	state, err := eng.QueryWorkflow(ctx, wfID)
	require.NoError(t, err)

	stepMap := make(map[string]engine.StepState)
	for _, s := range state.Steps {
		stepMap[s.Name] = s
	}
	assert.Equal(t, "queued", stepMap["deploy"].Status, "onSuccess step should run when pipeline succeeds")
	assert.Equal(t, "skipped", stepMap["cleanup"].Status, "onFailure step should be skipped when pipeline succeeds")
}

func TestEngine_IdempotentCompleteStep(t *testing.T) {
	pool, q := setupTestDB(t)
	ctx := context.Background()
	orgID, projectID := seedOrgAndProject(t, pool)

	pipelineYAML := `
image: alpine:3.19
triggers:
  push:
    branches: [main]
steps:
  - name: build
    run: echo building
`
	eng := engine.New(pool, nil)
	defer eng.Close()

	runID := uuid.NewString()
	insertTestRun(t, q, runID, projectID, orgID)

	wfID, err := eng.StartWorkflowWithWaves(ctx, engine.StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID,
		Repo: "acme/test", Ref: "main", CommitSHA: "abc123",
		TriggerType: "manual", TriggeredBy: "test",
	}, wavesFromYAML(t, pipelineYAML))
	require.NoError(t, err)

	token := engine.EncodeTaskToken(engine.TaskToken{
		WorkflowID: wfID, StepName: "build", Attempt: 0,
	})
	_, err = pool.Exec(ctx,
		"UPDATE steps SET status = 'running', started_at = now(), task_token = $1 WHERE workflow_id = $2 AND name = 'build'",
		&token, wfID)
	require.NoError(t, err)

	result := engine.StepResult{StepName: "build", Success: true, ExitCode: 0}

	// First call: completes the step.
	err = eng.CompleteStep(ctx, token, result)
	require.NoError(t, err)

	// Second call: idempotent, no error.
	err = eng.CompleteStep(ctx, token, result)
	require.NoError(t, err, "second CompleteStep call should be idempotent")

	state, err := eng.QueryWorkflow(ctx, wfID)
	require.NoError(t, err)
	assert.Equal(t, "succeeded", state.Status)
}

// ─────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────

// wavesFromYAML resolves a flat-steps test pipeline into engine waves, standing
// in for what a product (CI) would compile and hand to StartWorkflowWithWaves.
func wavesFromYAML(t *testing.T, src string) [][]pipeline.Step {
	t.Helper()
	var p pipeline.Pipeline
	require.NoError(t, yaml.Unmarshal([]byte(src), &p))
	waves, err := pipeline.ResolveDag(&p)
	require.NoError(t, err)
	return waves
}

func strPtr(s string) *string { return &s }
