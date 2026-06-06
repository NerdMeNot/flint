package pipeline_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

func TestResolveDag_Linear(t *testing.T) {
	p := &pipeline.Pipeline{
		Triggers: pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}},
		Steps: []pipeline.Step{
			{Name: "a", Run: pipeline.Cmd("echo a")},
			{Name: "b", Run: pipeline.Cmd("echo b"), DependsOn: []string{"a"}},
			{Name: "c", Run: pipeline.Cmd("echo c"), DependsOn: []string{"b"}},
		},
	}

	waves, err := pipeline.ResolveDag(p)
	require.NoError(t, err)
	require.Len(t, waves, 3)
	assertWaveContains(t, waves[0], "a")
	assertWaveContains(t, waves[1], "b")
	assertWaveContains(t, waves[2], "c")
}

func TestResolveDag_Parallel(t *testing.T) {
	p := &pipeline.Pipeline{
		Triggers: pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}},
		Steps: []pipeline.Step{
			{Name: "deps", Run: pipeline.Cmd("echo deps")},
			{Name: "lint", Run: pipeline.Cmd("echo lint"), DependsOn: []string{"deps"}},
			{Name: "test", Run: pipeline.Cmd("echo test"), DependsOn: []string{"deps"}},
			{Name: "build", Run: pipeline.Cmd("echo build"), DependsOn: []string{"deps"}},
			{Name: "deploy", Run: pipeline.Cmd("echo deploy"), DependsOn: []string{"lint", "test", "build"}},
		},
	}

	waves, err := pipeline.ResolveDag(p)
	require.NoError(t, err)
	require.Len(t, waves, 3)
	assertWaveContains(t, waves[0], "deps")
	assertWaveContains(t, waves[1], "lint", "test", "build")
	assertWaveContains(t, waves[2], "deploy")
}

func TestResolveDag_NoDeps(t *testing.T) {
	p := &pipeline.Pipeline{
		Triggers: pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}},
		Steps: []pipeline.Step{
			{Name: "a", Run: pipeline.Cmd("echo a")},
			{Name: "b", Run: pipeline.Cmd("echo b")},
			{Name: "c", Run: pipeline.Cmd("echo c")},
		},
	}

	waves, err := pipeline.ResolveDag(p)
	require.NoError(t, err)
	require.Len(t, waves, 1)
	assertWaveContains(t, waves[0], "a", "b", "c")
}

func TestResolveDag_Cycle(t *testing.T) {
	p := &pipeline.Pipeline{
		Triggers: pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}},
		Steps: []pipeline.Step{
			{Name: "a", Run: pipeline.Cmd("echo"), DependsOn: []string{"c"}},
			{Name: "b", Run: pipeline.Cmd("echo"), DependsOn: []string{"a"}},
			{Name: "c", Run: pipeline.Cmd("echo"), DependsOn: []string{"b"}},
		},
	}

	_, err := pipeline.ResolveDag(p)
	require.Error(t, err)
	assert.True(t, errors.Is(err, pipeline.ErrCycleDetected))
}

func TestResolveDag_FromParsedFile(t *testing.T) {
	data := readTestdata(t, "ci.yaml")
	p, err := pipeline.Parse(data)
	require.NoError(t, err)

	waves, err := pipeline.ResolveDag(p)
	require.NoError(t, err)

	// ci.yaml: deps → {lint, test, build} → integration-test → report (always)
	// But report depends on test, lint, build — so it's wave 3.
	// integration-test depends on build — wave 2.
	require.True(t, len(waves) >= 3, "expected at least 3 waves, got %d", len(waves))
	assertWaveContains(t, waves[0], "deps")
}

func TestResolveDagForEnv_Filters(t *testing.T) {
	p := &pipeline.Pipeline{
		Environments: []string{"staging", "production"},
		Triggers:     pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}},
		Steps: []pipeline.Step{
			{Name: "test", Run: pipeline.Cmd("make test")},
			{Name: "security-scan", Run: pipeline.Cmd("make scan"), Environments: []string{"production"}, DependsOn: []string{"test"}},
			{Name: "deploy", Run: pipeline.Cmd("make deploy"), DependsOn: []string{"test", "security-scan"}},
		},
	}

	// Staging: security-scan is skipped, deploy depends only on test.
	waves, err := pipeline.ResolveDagForEnv(p, "staging")
	require.NoError(t, err)
	require.Len(t, waves, 2)
	assertWaveContains(t, waves[0], "test")
	assertWaveContains(t, waves[1], "deploy")

	// Production: all steps run.
	waves, err = pipeline.ResolveDagForEnv(p, "production")
	require.NoError(t, err)
	require.Len(t, waves, 3)
	assertWaveContains(t, waves[0], "test")
	assertWaveContains(t, waves[1], "security-scan")
	assertWaveContains(t, waves[2], "deploy")
}

func TestResolveDag_SingleStep(t *testing.T) {
	p := &pipeline.Pipeline{
		Triggers: pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}},
		Steps:    []pipeline.Step{{Name: "only", Run: pipeline.Cmd("echo")}},
	}
	waves, err := pipeline.ResolveDag(p)
	require.NoError(t, err)
	require.Len(t, waves, 1)
	assert.Len(t, waves[0], 1)
}

func TestResolveDagForEnv_AllFiltered(t *testing.T) {
	p := &pipeline.Pipeline{
		Triggers: pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}},
		Steps: []pipeline.Step{
			{Name: "prod-only", Run: pipeline.Cmd("echo"), Environments: []string{"production"}},
		},
	}
	waves, err := pipeline.ResolveDagForEnv(p, "staging")
	require.NoError(t, err)
	assert.Len(t, waves, 0)
}

func TestResolveDagForEnv_EmptyEnv(t *testing.T) {
	p := &pipeline.Pipeline{
		Triggers: pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}},
		Steps:    []pipeline.Step{{Name: "test", Run: pipeline.Cmd("echo")}},
	}
	// Empty env = all steps included.
	waves, err := pipeline.ResolveDagForEnv(p, "")
	require.NoError(t, err)
	assert.Len(t, waves, 1)
}

func assertWaveContains(t *testing.T, wave []pipeline.Step, names ...string) {
	t.Helper()
	nameSet := make(map[string]bool, len(wave))
	for _, s := range wave {
		nameSet[s.Name] = true
	}
	for _, name := range names {
		assert.True(t, nameSet[name], "wave does not contain step %q (has: %v)", name, waveNames(wave))
	}
	assert.Len(t, wave, len(names), "wave has %v, expected %v", waveNames(wave), names)
}

func waveNames(wave []pipeline.Step) []string {
	names := make([]string, len(wave))
	for i, s := range wave {
		names[i] = s.Name
	}
	return names
}

func TestResolveDag_LargeIndependent(t *testing.T) {
	p := &pipeline.Pipeline{Triggers: pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}}}
	for i := 0; i < 60; i++ {
		p.Steps = append(p.Steps, pipeline.Step{Name: fmt.Sprintf("s%d", i), Run: pipeline.Cmd("echo")})
	}
	waves, err := pipeline.ResolveDag(p)
	require.NoError(t, err)
	require.Len(t, waves, 1) // all independent → a single wave
	assert.Len(t, waves[0], 60)
}

func TestResolveDag_WithMatrix(t *testing.T) {
	p := &pipeline.Pipeline{
		Triggers: pipeline.Triggers{Push: &pipeline.PushTrigger{Branches: []string{"main"}}},
		Steps: []pipeline.Step{
			{Name: "test", Run: pipeline.Cmd("echo"), Matrix: map[string][]string{"go": {"1.22", "1.23", "1.24"}}},
			{Name: "report", Run: pipeline.Cmd("echo"), DependsOn: []string{"test"}},
		},
	}
	expanded := pipeline.ExpandMatrix(p)
	waves, err := pipeline.ResolveDag(expanded)
	require.NoError(t, err)
	require.Len(t, waves, 2)
	assert.Len(t, waves[0], 3) // 3 matrix variants in the first wave
	assertWaveContains(t, waves[1], "report")
}
