package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func driverConfig(t *testing.T, steps []SubStep) *StepsConfig {
	t.Helper()
	return &StepsConfig{
		Workspace: t.TempDir(),
		Steps:     steps,
		Git:       map[string]any{"sha": "deadbeef", "branch": "main", "repoUrl": "acme/app"},
		Run:       map[string]any{"id": "run-1", "trigger": "push"},
	}
}

func readFileOr(t *testing.T, path, fallback string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	return string(b)
}

// TestRunSteps_SequentialWithOutputs: sub-steps run in order, $FLINT_OUTPUT
// values flow into later sub-steps' expressions and env, and declared job
// outputs land in the emit file.
func TestRunSteps_SequentialWithOutputs(t *testing.T) {
	cfg := driverConfig(t, []SubStep{
		{Name: "produce", Run: pipeline.Cmd(`echo "version=1.2.3" >> "$FLINT_OUTPUT"`)},
		{
			Name: "consume",
			If:   `${{ steps.outputs.version == "1.2.3" }}`,
			Env:  map[string]string{"VER": "${{ steps.outputs.version }}"},
			Run:  pipeline.Cmd(`echo "got $VER" && echo "tag=v$VER" >> "$FLINT_OUTPUT"`),
		},
	})
	cfg.JobOutputs = map[string]string{"release": "${{ steps.outputs.tag }}"}

	require.NoError(t, RunSteps(context.Background(), cfg))

	// Pod contract: exit code 0.
	assert.Equal(t, "0", readFileOr(t, filepath.Join(cfg.Workspace, ".flint-exit"), ""))

	// Log carries both steps' output.
	logText := readFileOr(t, filepath.Join(cfg.Workspace, ".flint-step.log"), "")
	assert.Contains(t, logText, "got 1.2.3")

	// Declared job outputs in the emit file (what the sidecar reports).
	emit := readFileOr(t, filepath.Join(cfg.Workspace, ".flint-emit"), "")
	assert.Contains(t, emit, "release=v1.2.3")
	// Undeclared step outputs must NOT cross the job boundary.
	assert.NotContains(t, emit, "version=1.2.3")
}

// TestRunSteps_FailureStopsAndSkips: a failing sub-step fails the job and the
// remaining sub-steps are skipped.
func TestRunSteps_FailureStopsAndSkips(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "should-not-exist")
	cfg := driverConfig(t, []SubStep{
		{Name: "boom", Run: pipeline.Cmd("exit 3")},
		{Name: "after", Run: pipeline.Cmd("touch " + marker)},
	})

	err := RunSteps(context.Background(), cfg)
	require.Error(t, err)
	assert.Equal(t, "3", readFileOr(t, filepath.Join(cfg.Workspace, ".flint-exit"), ""))
	_, statErr := os.Stat(marker)
	assert.True(t, os.IsNotExist(statErr), "steps after a failure must be skipped")

	logText := readFileOr(t, filepath.Join(cfg.Workspace, ".flint-step.log"), "")
	assert.Contains(t, logText, "skipped (previous step failed)")
}

// TestRunSteps_ContinueOnError: a continueOnError failure doesn't stop the job.
func TestRunSteps_ContinueOnError(t *testing.T) {
	cfg := driverConfig(t, []SubStep{
		{Name: "flaky", Run: pipeline.Cmd("exit 1"), ContinueOnError: true},
		{Name: "after", Run: pipeline.Cmd(`echo "ran=yes" >> "$FLINT_OUTPUT"`)},
	})
	cfg.JobOutputs = map[string]string{"ran": "${{ steps.outputs.ran }}"}

	require.NoError(t, RunSteps(context.Background(), cfg))
	emit := readFileOr(t, filepath.Join(cfg.Workspace, ".flint-emit"), "")
	assert.Contains(t, emit, "ran=yes")
}

// TestRunSteps_IfFalseSkips: a false if: skips only that sub-step.
func TestRunSteps_IfFalseSkips(t *testing.T) {
	cfg := driverConfig(t, []SubStep{
		{Name: "skipme", If: `${{ git.branch == "release" }}`, Run: pipeline.Cmd("exit 1")},
		{Name: "runme", Run: pipeline.Cmd("true")},
	})
	require.NoError(t, RunSteps(context.Background(), cfg))
	logText := readFileOr(t, filepath.Join(cfg.Workspace, ".flint-step.log"), "")
	assert.Contains(t, logText, "skipped (if condition false)")
}

// TestRunSteps_IfErrorFailsLoudly: an if: that cannot evaluate fails the job
// (same policy as the engine — never silently skip).
func TestRunSteps_IfErrorFailsLoudly(t *testing.T) {
	cfg := driverConfig(t, []SubStep{
		{Name: "broken", If: `${{ bogus_ns.x == "y" }}`, Run: pipeline.Cmd("true")},
	})
	err := RunSteps(context.Background(), cfg)
	require.Error(t, err)
	logText := readFileOr(t, filepath.Join(cfg.Workspace, ".flint-step.log"), "")
	assert.Contains(t, logText, "FAILED: if condition")
}

// TestRunSteps_NeedsContext: needs.<job>.outputs.* from upstream jobs is
// available to expressions and env interpolation.
func TestRunSteps_NeedsContext(t *testing.T) {
	cfg := driverConfig(t, []SubStep{
		{
			Name: "deploy",
			If:   `${{ needs.build.outputs.version == "2.0" }}`,
			Env:  map[string]string{"V": "${{ needs.build.outputs.version }}"},
			Run:  pipeline.Cmd(`echo "deploying $V" && [ "$V" = "2.0" ]`),
		},
	})
	cfg.NeedsOutputs = map[string]map[string]string{
		"build": {"version": "2.0"},
	}
	require.NoError(t, RunSteps(context.Background(), cfg))
	logText := readFileOr(t, filepath.Join(cfg.Workspace, ".flint-step.log"), "")
	assert.Contains(t, logText, "deploying 2.0")
}

// TestRunSteps_Retry: a sub-step with retry attempts is re-run until it passes.
func TestRunSteps_Retry(t *testing.T) {
	ws := t.TempDir()
	cfg := &StepsConfig{
		Workspace: ws,
		Steps: []SubStep{{
			Name: "flaky",
			// Fails on the first attempt (no marker), succeeds on the second.
			Run:   pipeline.Cmd(`if [ -f flaky-marker ]; then true; else touch flaky-marker && exit 1; fi`),
			Retry: &pipeline.RetrySpec{Attempts: 2, Delay: "1ms"},
		}},
		Git: map[string]any{}, Run: map[string]any{},
	}
	require.NoError(t, RunSteps(context.Background(), cfg))
	logText := readFileOr(t, filepath.Join(ws, ".flint-step.log"), "")
	assert.Contains(t, logText, "retry 2/2")
}

// TestRunSteps_WorkingDirAndShell: workingDir is relative to the workspace and
// bash is selectable.
func TestRunSteps_WorkingDirAndShell(t *testing.T) {
	cfg := driverConfig(t, nil)
	require.NoError(t, os.MkdirAll(filepath.Join(cfg.Workspace, "sub"), 0o755))
	cfg.Steps = []SubStep{
		{Name: "wd", WorkingDir: "sub", Run: pipeline.Cmd(`[ "$(basename $(pwd))" = "sub" ]`)},
		{Name: "bash", Shell: "bash", Run: pipeline.Cmd(`[[ 1 -eq 1 ]]`)},
	}
	require.NoError(t, RunSteps(context.Background(), cfg))
}

// TestRunSteps_Timeout: a sub-step exceeding its timeout fails with 124.
func TestRunSteps_Timeout(t *testing.T) {
	cfg := driverConfig(t, []SubStep{
		{Name: "slow", Timeout: "100ms", Run: pipeline.Cmd("sleep 5")},
	})
	err := RunSteps(context.Background(), cfg)
	require.Error(t, err)
	assert.Equal(t, "124", readFileOr(t, filepath.Join(cfg.Workspace, ".flint-exit"), ""))
	logText := readFileOr(t, filepath.Join(cfg.Workspace, ".flint-step.log"), "")
	assert.Contains(t, logText, "timed out")
}

func TestCollectOutputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out")
	require.NoError(t, os.WriteFile(path, []byte("a=1\n# comment\n\nb = spaced \nnoequals\n"), 0o644))
	got := map[string]string{}
	collectOutputs(path, got)
	assert.Equal(t, map[string]string{"a": "1", "b": "spaced"}, got)
}

// TestInstallDriver copies the running binary into the workspace.
func TestInstallDriver(t *testing.T) {
	ws := t.TempDir()
	require.NoError(t, InstallDriver(ws))
	info, err := os.Stat(filepath.Join(ws, ".flint-bin", "flint-agent"))
	require.NoError(t, err)
	assert.True(t, strings.Contains(info.Mode().String(), "x") || info.Mode()&0o111 != 0,
		"driver copy must be executable")
}
