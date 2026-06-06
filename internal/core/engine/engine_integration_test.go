package engine_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/dbkit"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/pkg/forge"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	// Pipeline location now lives in pipeline_source jsonb (defaults to .flint/).
	_, err = pool.Exec(ctx,
		`INSERT INTO projects (id, org_id, forge_id, display_name, repo_path, repo_url)
		 VALUES ($1, $2, $3, 'test-project', 'acme/test', 'https://example.com/acme/test')
		 ON CONFLICT DO NOTHING`,
		projectID, orgID, forgeID)
	require.NoError(t, err)

	return orgID, projectID
}

// insertTestRun creates a pipeline_runs row for testing.
func insertTestRun(t *testing.T, q *db.Queries, runID, projectID, orgID string) {
	t.Helper()
	err := q.InsertPipelineRun(context.Background(), db.InsertPipelineRunParams{
		ID:           runID,
		ProjectID:    projectID,
		OrgID:        orgID,
		WorkflowFile: "ci.yaml",
		TriggerType:  "manual",
		TriggerRef:   strPtr("main"),
		TriggeredBy:  strPtr("test"),
	})
	require.NoError(t, err)
}

// ─────────────────────────────────────────────────────────────
// Mock forge
// ─────────────────────────────────────────────────────────────

type mockForge struct {
	files map[string][]byte // path → content
}

func (m *mockForge) ParseWebhook(http.Header, []byte, string) (*forge.WebhookEvent, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockForge) PostCommitStatus(context.Context, string, string, forge.CommitStatus) error {
	return nil
}
func (m *mockForge) GetFile(_ context.Context, _, _, path string) ([]byte, error) {
	data, ok := m.files[path]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	return data, nil
}
func (m *mockForge) GetDirectory(context.Context, string, string, string) (map[string][]byte, error) {
	return nil, nil
}
func (m *mockForge) CreateWebhook(context.Context, string, string, string, []string) (string, error) {
	return "", nil
}
func (m *mockForge) DeleteWebhook(context.Context, string, string) error { return nil }
func (m *mockForge) CloneURL(repo string) string {
	return "https://github.com/" + repo + ".git"
}
func (m *mockForge) Type() string { return "mock" }

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
	f := &mockForge{files: map[string][]byte{".flint/ci.yaml": []byte(pipelineYAML)}}
	eng := engine.New(pool, f, nil)
	defer eng.Close()

	runID := uuid.NewString()
	insertTestRun(t, q, runID, projectID, orgID)

	// Start workflow.
	wfID, err := eng.StartWorkflow(ctx, engine.StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID,
		Repo: "acme/test", Ref: "main", CommitSHA: "abc123",
		TriggerType: "manual", TriggeredBy: "test",
		WorkflowFile: "ci.yaml", PipelinePath: ".flint",
	})
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
	f := &mockForge{files: map[string][]byte{".flint/ci.yaml": []byte(pipelineYAML)}}
	eng := engine.New(pool, f, nil)
	defer eng.Close()

	runID := uuid.NewString()
	insertTestRun(t, q, runID, projectID, orgID)

	wfID, err := eng.StartWorkflow(ctx, engine.StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID,
		Repo: "acme/test", Ref: "main", CommitSHA: "abc123",
		TriggerType: "manual", TriggeredBy: "test",
		WorkflowFile: "ci.yaml", PipelinePath: ".flint",
	})
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
	f := &mockForge{files: map[string][]byte{".flint/ci.yaml": []byte(pipelineYAML)}}
	eng := engine.New(pool, f, nil)
	defer eng.Close()

	runID := uuid.NewString()
	insertTestRun(t, q, runID, projectID, orgID)

	wfID, err := eng.StartWorkflow(ctx, engine.StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID,
		Repo: "acme/test", Ref: "main", CommitSHA: "abc123",
		TriggerType: "manual", TriggeredBy: "test",
		WorkflowFile: "ci.yaml", PipelinePath: ".flint",
	})
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
	f := &mockForge{files: map[string][]byte{".flint/ci.yaml": []byte(pipelineYAML)}}
	eng := engine.New(pool, f, nil)
	defer eng.Close()

	runID := uuid.NewString()
	insertTestRun(t, q, runID, projectID, orgID)

	wfID, err := eng.StartWorkflow(ctx, engine.StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID,
		Repo: "acme/test", Ref: "main", CommitSHA: "abc123",
		TriggerType: "manual", TriggeredBy: "test",
		WorkflowFile: "ci.yaml", PipelinePath: ".flint",
	})
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

func TestEngine_ErrorMessageOnParseFailure(t *testing.T) {
	pool, q := setupTestDB(t)
	ctx := context.Background()
	orgID, projectID := seedOrgAndProject(t, pool)

	// Invalid YAML — missing steps.
	badYAML := `triggers:
  push:
    branches: [main]
`
	f := &mockForge{files: map[string][]byte{".flint/ci.yaml": []byte(badYAML)}}
	eng := engine.New(pool, f, nil)
	defer eng.Close()

	runID := uuid.NewString()
	insertTestRun(t, q, runID, projectID, orgID)

	_, err := eng.StartWorkflow(ctx, engine.StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID,
		Repo: "acme/test", Ref: "main", CommitSHA: "abc123",
		TriggerType: "manual", TriggeredBy: "test",
		WorkflowFile: "ci.yaml", PipelinePath: ".flint",
	})
	assert.Error(t, err, "StartWorkflow should fail on invalid pipeline YAML")
}

// ─────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────

func strPtr(s string) *string { return &s }
