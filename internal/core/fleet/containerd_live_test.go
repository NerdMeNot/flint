package fleet_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/dbkit"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/core/fleet"
	"github.com/NerdMeNot/flint/internal/core/runner"
	"github.com/NerdMeNot/flint/internal/platform/agentgrpc"
	"github.com/NerdMeNot/flint/internal/testutil/pgtest"
	"github.com/NerdMeNot/flint/pkg/logsink"
)

// TestContainerdLive validates the PRODUCTION runtime path — the flint-agent
// daemon supervising its own bundled containerd, executing steps as real
// containers with image pulls, binds, and cgroup limits — by running the
// agent inside a privileged podman container (a real Linux environment;
// byte-identical to a dind-style machine).
//
// Opt-in: it downloads the runtime bundle and pulls images, so it runs only
// with FLINT_CONTAINERD_E2E=1 and a working podman (or docker via
// FLINT_CONTAINER_TOOL). The runtime bundle is cached in a named volume
// across runs.
func TestContainerdLive(t *testing.T) {
	if os.Getenv("FLINT_CONTAINERD_E2E") == "" {
		t.Skip("set FLINT_CONTAINERD_E2E=1 (requires podman/docker) to run the live containerd validation")
	}
	tool := os.Getenv("FLINT_CONTAINER_TOOL")
	if tool == "" {
		tool = "podman"
	}
	if _, err := exec.LookPath(tool); err != nil {
		t.Skipf("%s not found on PATH", tool)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dsn := pgtest.DSN(t)

	require.NoError(t, dbkit.RunMigrations(dsn, dbkit.Migrations, "migrations"))
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	q := db.New(pool)

	signingKey := []byte("live-signing-key-0123456789abcdef")
	eng := engine.New(pool, signingKey)
	t.Cleanup(func() { eng.Close() })
	fl := fleet.New(pool, eng)

	require.NoError(t, q.UpsertMachinePool(ctx, db.UpsertMachinePoolParams{
		Name: "containerd-live", Provider: "static", Arch: runtime.GOARCH, Cpu: "2", Memory: "2Gi",
		CapacityType: "on_demand", Objective: "balanced", MaxMachines: 2, IdleTtlSeconds: 900,
	}))
	joinToken, hash, err := fleet.MintToken()
	require.NoError(t, err)
	require.NoError(t, q.SetPoolJoinTokenHash(ctx, db.SetPoolJoinTokenHashParams{
		Name: "containerd-live", JoinTokenHash: &hash,
	}))

	// AgentService on all interfaces so the container reaches the host.
	logDir := t.TempDir()
	agentSrv := agentgrpc.New(fl, eng, &logsink.FilesystemSink{BaseDir: logDir}, nil, agentgrpc.Config{
		ServerHTTPURL: "http://flint.live:8080",
	})
	lis, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	go func() { _ = agentSrv.GRPCServer().Serve(lis) }()
	t.Cleanup(agentSrv.GRPCServer().Stop)
	grpcPort := lis.Addr().(*net.TCPAddr).Port

	registry := runner.NewRegistry()
	require.NoError(t, runner.LoadAll(ctx, q, registry))
	machineExec := engine.NewMachineExecutor(pool, registry, engine.MachineExecutorConfig{
		ServerHTTPURL: "http://flint.live:8080",
	})
	loop := engine.NewLoop(eng, engine.ExecutorRegistry{
		"run": machineExec, "use": machineExec, "steps": machineExec,
	}, engine.LoopConfig{PollInterval: 200 * time.Millisecond, SigningKey: signingKey})
	go func() { _ = loop.Run(ctx) }()
	go func() { _ = fleet.NewLoop(fl, fleet.LoopConfig{TickInterval: 300 * time.Millisecond}).Run(ctx) }()

	// The linux agent binary, cross-compiled for the VM's architecture.
	agentBin := buildLinuxAgent(t)

	// Fresh data volume per run (identity must not leak across databases);
	// the runtime bundle (containerd/runc, ~100MB) is cached in a shared
	// named volume mounted at the bundle path.
	nonce := make([]byte, 4)
	_, _ = rand.Read(nonce)
	dataVol := "flint-live-" + hex.EncodeToString(nonce)
	t.Cleanup(func() { _ = exec.Command(tool, "volume", "rm", "-f", dataVol).Run() })

	// A glibc base image: containerd release binaries are dynamically linked
	// (musl images can't run them — same requirement real machines have).
	//
	// The cgroup dance is nested-container (dind) specific: cgroup v2's
	// "no internal processes" rule means the container's root cgroup (which
	// holds this shell) can't enable domain controllers for runc's child
	// cgroups until its processes move to a leaf. Real machines don't need
	// this — systemd owns the hierarchy there.
	script := fmt.Sprintf(`set -eu
if [ -f /sys/fs/cgroup/cgroup.subtree_control ]; then
  mkdir -p /sys/fs/cgroup/init
  while read -r p; do echo "$p" > /sys/fs/cgroup/init/cgroup.procs 2>/dev/null || true; done < /sys/fs/cgroup/cgroup.procs
  echo "+cpu +memory +pids +io" > /sys/fs/cgroup/cgroup.subtree_control 2>/dev/null || true
fi
/var/lib/flint-agent/bin/containerd --version >/dev/null 2>&1 || sh /bundle-runtime.sh /var/lib/flint-agent/bin
exec /usr/local/bin/flint-agent daemon \
  --server host.containers.internal:%d \
  --token %s --runtime containerd --insecure \
  --data-dir /var/lib/flint-agent --capacity 2`, grpcPort, joinToken)

	agentCtx, stopAgent := context.WithCancel(ctx)
	defer stopAgent()
	//nolint:gosec // test harness assembling its own container invocation
	agentCmd := exec.CommandContext(agentCtx, tool, "run", "--rm", "--privileged",
		"--name", dataVol,
		"-v", agentBin+":/usr/local/bin/flint-agent:ro",
		"-v", repoRoot(t)+"/scripts/bundle-runtime.sh:/bundle-runtime.sh:ro",
		"-v", "flint-live-bundle-cache:/var/lib/flint-agent/bin",
		"-v", dataVol+":/var/lib/flint-agent",
		"docker.io/library/buildpack-deps:bookworm-curl", "sh", "-c", script)
	var agentOut strings.Builder
	agentCmd.Stdout = &agentOut
	agentCmd.Stderr = &agentOut
	require.NoError(t, agentCmd.Start())
	t.Cleanup(func() {
		stopAgent()
		_ = exec.Command(tool, "rm", "-f", dataVol).Run()
		if t.Failed() {
			t.Logf("agent container output:\n%s", agentOut.String())
		}
	})

	// Registration includes the runtime bundle download on a cold cache.
	waitFor(t, 5*time.Minute, func() bool {
		var n int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM machines WHERE status = 'idle'`).Scan(&n)
		return n == 1
	}, "agent should bundle containerd and register (output:\n"+agentOut.String()+")")
	t.Log("machine registered with the bundled containerd runtime")

	// A real pipeline: image pull, container execution, env, emit outputs,
	// exit codes — through actual containers.
	orgID, projectID := seedOrgProject(t, pool)
	runID := uuid.NewString()
	insertRun(t, q, runID, projectID, orgID)

	wfID, err := eng.StartWorkflowWithWaves(ctx, engine.StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID,
		Repo: "acme/live", Ref: "main", CommitSHA: "beef1234",
		TriggerType: "manual", TriggeredBy: "containerd-live",
		PipelineImage: "docker.io/library/alpine:3.19",
	}, wavesFrom(t, `
image: docker.io/library/alpine:3.19
steps:
  - name: identify
    runner: containerd-live
    run: |
      cat /etc/alpine-release
      emit kernel "$(uname -r)"
  - name: workspace
    runner: containerd-live
    run: |
      touch /workspace/handoff.txt
      test -f /workspace/handoff.txt
    dependsOn: [identify]
`))
	require.NoError(t, err)

	deadline := time.Now().Add(4 * time.Minute) // first alpine pull included
	var state *engine.WorkflowState
	for time.Now().Before(deadline) {
		state, err = eng.QueryWorkflow(ctx, wfID)
		require.NoError(t, err)
		if state.Status == "succeeded" || state.Status == "failed" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	require.NotNil(t, state)
	require.Equalf(t, "succeeded", state.Status,
		"steps: %+v\nagent output:\n%s", state.Steps, agentOut.String())

	// The step ran in a REAL container: its emitted kernel is the Linux VM's,
	// and alpine-release output reached the log sink through the stream.
	var kernel string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COALESCE(result->'outputs'->>'kernel', '') FROM steps
		 WHERE workflow_id = $1 AND name = 'identify' ORDER BY attempt DESC LIMIT 1`, wfID).
		Scan(&kernel))
	assert.Regexp(t, `^\d+\.\d+`, kernel, "emitted kernel release should be a real uname -r")
	t.Logf("step container kernel: %s", kernel)

	lines, err := (&logsink.FilesystemSink{BaseDir: logDir}).Read(ctx,
		logsink.LogRef{OrgID: orgID, RunID: runID, StepName: "identify"})
	require.NoError(t, err)
	assert.NotEmpty(t, lines, "container stdout should reach the server sink")
}

// buildLinuxAgent cross-compiles flint-agent for linux on the host arch
// (matching the podman VM).
func buildLinuxAgent(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "flint-agent")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/NerdMeNot/flint/cmd/flint-agent")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+runtime.GOARCH, "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "building linux flint-agent: %s", out)
	return bin
}
