package fleet_test

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/NerdMeNot/flint/internal/agentd"
	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/dbkit"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/core/fleet"
	"github.com/NerdMeNot/flint/internal/core/runner"
	"github.com/NerdMeNot/flint/internal/platform/agentgrpc"
	"github.com/NerdMeNot/flint/internal/testutil/pgtest"
	"github.com/NerdMeNot/flint/pkg/logsink"
	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// TestEndToEnd_PipelineOnRealAgent is the pivot's proof: a two-step pipeline
// flows engine → machine executor → fleet scheduler → a REAL flint-agent
// daemon (hostshell runtime) → step execution → gRPC completion → engine
// advancement — no Kubernetes anywhere.
func TestEndToEnd_PipelineOnRealAgent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dsn := pgtest.DSN(t)

	require.NoError(t, dbkit.RunMigrations(dsn, dbkit.Migrations, "migrations"))
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	q := db.New(pool)

	signingKey := []byte("e2e-signing-key-0123456789abcdef")
	eng := engine.New(pool, signingKey)
	t.Cleanup(func() { eng.Close() })
	fl := fleet.New(pool, eng)

	// Pool + join token.
	require.NoError(t, q.UpsertMachinePool(ctx, db.UpsertMachinePoolParams{
		Name: "standard", Provider: "static", Arch: "amd64", Cpu: "8", Memory: "16Gi",
		CapacityType: "on_demand", Objective: "balanced", MaxMachines: 10, IdleTtlSeconds: 900,
	}))
	joinToken, hash, err := fleet.MintToken()
	require.NoError(t, err)
	require.NoError(t, q.SetPoolJoinTokenHash(ctx, db.SetPoolJoinTokenHashParams{Name: "standard", JoinTokenHash: &hash}))

	// AgentService on a real TCP port (the daemon dials by address).
	logDir := t.TempDir()
	agentSrv := agentgrpc.New(fl, eng, &logsink.FilesystemSink{BaseDir: logDir}, nil, agentgrpc.Config{
		ServerHTTPURL: "http://flint.e2e:8080",
	})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = agentSrv.GRPCServer().Serve(lis) }()
	t.Cleanup(agentSrv.GRPCServer().Stop)
	grpcAddr := lis.Addr().String()

	// Engine loop with the machine executor.
	registry := runner.NewRegistry()
	require.NoError(t, runner.LoadAll(ctx, q, registry))
	machineExec := engine.NewMachineExecutor(pool, registry, engine.MachineExecutorConfig{
		ServerHTTPURL: "http://flint.e2e:8080",
	})
	loop := engine.NewLoop(eng, engine.ExecutorRegistry{
		"run": machineExec, "use": machineExec, "steps": machineExec,
	}, engine.LoopConfig{PollInterval: 100 * time.Millisecond, SigningKey: signingKey})
	go func() { _ = loop.Run(ctx) }()

	// Fleet loop (scheduling + sweeps).
	go func() { _ = fleet.NewLoop(fl, fleet.LoopConfig{TickInterval: 200 * time.Millisecond}).Run(ctx) }()

	// The REAL agent daemon on the hostshell runtime. Group steps exec the
	// flint-agent binary, so build it — in production the daemon IS that
	// binary; in-process here it would be the test binary.
	agentBin := buildAgentBinary(t)
	go func() {
		_ = agentd.Run(ctx, agentd.Config{
			ServerURL:   grpcAddr,
			Token:       joinToken,
			DataDir:     t.TempDir(),
			Capacity:    2,
			Runtime:     "hostshell",
			Insecure:    true,
			AgentBinary: agentBin,
		})
	}()

	// Start a two-step pipeline with a cross-step output dependency.
	orgID, projectID := seedOrgProject(t, pool)
	runID := uuid.NewString()
	insertRun(t, q, runID, projectID, orgID)

	// Wave 0: a plain run step. Wave 1: a group ("steps") job exercising the
	// in-container steps driver, plus a dependent run step.
	waves := wavesFrom(t, `
image: alpine:3.19
steps:
  - name: build
    run: |
      echo building the thing
      emit artifact core-v1
  - name: test
    run: echo testing
    dependsOn: [build]
`)
	waves = append(waves, []pipeline.Step{{
		Name:      "grouped",
		DependsOn: []string{"test"},
		Steps: []pipeline.Step{
			{Name: "one", Run: pipeline.Cmd("echo sub-step one")},
			{Name: "two", Run: pipeline.Cmd(`emit verdict shipped`)},
		},
		// Only declared outputs cross the job boundary.
		DeclaredOutputs: map[string]string{"verdict": "${{ steps.outputs.verdict }}"},
	}})

	wfID, err := eng.StartWorkflowWithWaves(ctx, engine.StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID,
		Repo: "acme/e2e", Ref: "main", CommitSHA: "cafe1234",
		TriggerType: "manual", TriggeredBy: "e2e",
		PipelineImage: "alpine:3.19",
	}, waves)
	require.NoError(t, err)

	// The run should complete end-to-end on the real agent.
	deadline := time.Now().Add(45 * time.Second)
	var state *engine.WorkflowState
	for time.Now().Before(deadline) {
		state, err = eng.QueryWorkflow(ctx, wfID)
		require.NoError(t, err)
		if state.Status == "succeeded" || state.Status == "failed" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.NotNil(t, state)
	require.Equal(t, "succeeded", state.Status, "workflow steps: %+v", state.Steps)

	// The emit() output was captured and recorded on the step result.
	var artifactOut string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COALESCE(result->'outputs'->>'artifact', '') FROM steps
		 WHERE workflow_id = $1 AND name = 'build' ORDER BY attempt DESC LIMIT 1`, wfID).
		Scan(&artifactOut))
	assert.Equal(t, "core-v1", artifactOut, "emit output should flow through")

	// The group job's driver-run sub-step emitted an output too.
	var verdictOut string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COALESCE(result->'outputs'->>'verdict', '') FROM steps
		 WHERE workflow_id = $1 AND name = 'grouped' ORDER BY attempt DESC LIMIT 1`, wfID).
		Scan(&verdictOut))
	assert.Equal(t, "shipped", verdictOut, "group sub-step emit should flow through the driver")

	// Assignments went terminal on one machine, which returned to idle.
	rows, err := q.ListRunAssignments(ctx, runID)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	machineID := ""
	for _, a := range rows {
		assert.Equal(t, "succeeded", a.Status)
		require.NotNil(t, a.MachineID)
		if machineID == "" {
			machineID = *a.MachineID
		} else {
			assert.Equal(t, machineID, *a.MachineID, "warmth affinity: same run ⇒ same machine")
		}
	}
	waitFor(t, 5*time.Second, func() bool {
		m, err := q.GetMachine(ctx, machineID)
		return err == nil && m.Status == "idle"
	}, "machine should return to idle")

	// Live logs landed in the server-side sink via the gRPC stream.
	lines, err := (&logsink.FilesystemSink{BaseDir: logDir}).Read(ctx,
		logsink.LogRef{OrgID: orgID, RunID: runID, StepName: "build"})
	require.NoError(t, err)
	found := false
	for _, l := range lines {
		if l.Content == "building the thing" {
			found = true
		}
	}
	assert.True(t, found, "step stdout should reach the server log sink, got %d lines", len(lines))
}

// ── helpers ──────────────────────────────────────────────────

func seedOrgProject(t *testing.T, pool *pgxpool.Pool) (orgID, projectID string) {
	t.Helper()
	ctx := context.Background()
	orgID, projectID = uuid.NewString(), uuid.NewString()
	forgeID, wsID := uuid.NewString(), uuid.NewString()

	_, err := pool.Exec(ctx, `INSERT INTO orgs (id, name, slug) VALUES ($1, $2, $3)`,
		orgID, "e2e-org-"+orgID[:8], "e2e-"+orgID[:8])
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO forge_connections (id, org_id, forge_type, display_name, webhook_secret, credentials_enc)
		 VALUES ($1, $2, 'github', 'e2e-forge', 'e2e-secret', $3)`, forgeID, orgID, []byte{})
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO workspaces (id, org_id, name, slug, is_default) VALUES ($1, $2, 'Default', $3, true)`,
		wsID, orgID, "default-"+orgID[:8])
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO projects (id, org_id, forge_id, workspace_id, display_name, repo_path, repo_url)
		 VALUES ($1, $2, $3, $4, 'e2e-project', 'acme/e2e', 'https://example.com/acme/e2e')`,
		projectID, orgID, forgeID, wsID)
	require.NoError(t, err)
	return orgID, projectID
}

func insertRun(t *testing.T, q *db.Queries, runID, projectID, orgID string) {
	t.Helper()
	file, ref, by := "ci.yaml", "main", "e2e"
	require.NoError(t, q.InsertPipelineRun(context.Background(), db.InsertPipelineRunParams{
		ID: runID, ProjectID: &projectID, OrgID: orgID,
		WorkflowFile: &file, TriggerType: "manual", TriggerRef: &ref, TriggeredBy: &by,
	}))
}

func wavesFrom(t *testing.T, src string) [][]pipeline.Step {
	t.Helper()
	var p pipeline.Pipeline
	require.NoError(t, yaml.Unmarshal([]byte(src), &p))
	waves, err := pipeline.ResolveDag(&p)
	require.NoError(t, err)
	return waves
}

// buildAgentBinary compiles cmd/flint-agent for the group-steps driver.
func buildAgentBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "flint-agent")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/NerdMeNot/flint/cmd/flint-agent")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "building flint-agent: %s", out)
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "go.mod not found above test dir")
		dir = parent
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out: %s", msg)
}
