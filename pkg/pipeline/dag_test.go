package pipeline_test

import (
	"errors"
	"testing"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

func TestResolveDag_Linear(t *testing.T) {
	p := &pipeline.Pipeline{
		Name: "test",
		Steps: []pipeline.Step{
			{Name: "a", Run: "echo a"},
			{Name: "b", Run: "echo b", After: []string{"a"}},
			{Name: "c", Run: "echo c", After: []string{"b"}},
		},
	}

	waves, err := pipeline.ResolveDag(p)
	if err != nil {
		t.Fatalf("ResolveDag() error: %v", err)
	}

	if len(waves) != 3 {
		t.Fatalf("len(waves) = %d, want 3", len(waves))
	}
	assertWaveContains(t, waves[0], "a")
	assertWaveContains(t, waves[1], "b")
	assertWaveContains(t, waves[2], "c")
}

func TestResolveDag_Parallel(t *testing.T) {
	p := &pipeline.Pipeline{
		Name: "test",
		Steps: []pipeline.Step{
			{Name: "deps", Run: "echo deps"},
			{Name: "lint", Run: "echo lint", After: []string{"deps"}},
			{Name: "test", Run: "echo test", After: []string{"deps"}},
			{Name: "build", Run: "echo build", After: []string{"deps"}},
			{Name: "deploy", Run: "echo deploy", After: []string{"lint", "test", "build"}},
		},
	}

	waves, err := pipeline.ResolveDag(p)
	if err != nil {
		t.Fatalf("ResolveDag() error: %v", err)
	}

	if len(waves) != 3 {
		t.Fatalf("len(waves) = %d, want 3", len(waves))
	}
	assertWaveContains(t, waves[0], "deps")
	assertWaveContains(t, waves[1], "lint", "test", "build")
	assertWaveContains(t, waves[2], "deploy")
}

func TestResolveDag_NoDeps(t *testing.T) {
	p := &pipeline.Pipeline{
		Name: "test",
		Steps: []pipeline.Step{
			{Name: "a", Run: "echo a"},
			{Name: "b", Run: "echo b"},
			{Name: "c", Run: "echo c"},
		},
	}

	waves, err := pipeline.ResolveDag(p)
	if err != nil {
		t.Fatalf("ResolveDag() error: %v", err)
	}

	if len(waves) != 1 {
		t.Fatalf("len(waves) = %d, want 1", len(waves))
	}
	assertWaveContains(t, waves[0], "a", "b", "c")
}

func TestResolveDag_Cycle(t *testing.T) {
	p := &pipeline.Pipeline{
		Name: "test",
		Steps: []pipeline.Step{
			{Name: "a", Run: "echo", After: []string{"c"}},
			{Name: "b", Run: "echo", After: []string{"a"}},
			{Name: "c", Run: "echo", After: []string{"b"}},
		},
	}

	_, err := pipeline.ResolveDag(p)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !errors.Is(err, pipeline.ErrCycleDetected) {
		t.Errorf("expected ErrCycleDetected, got: %v", err)
	}
}

func TestResolveDag_FromParsedFile(t *testing.T) {
	data := readTestdata(t, "ci.yaml")
	p, err := pipeline.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	waves, err := pipeline.ResolveDag(p)
	if err != nil {
		t.Fatalf("ResolveDag() error: %v", err)
	}

	// ci.yaml: deps → {lint, test, build} → approve-production → deploy
	if len(waves) != 4 {
		t.Fatalf("len(waves) = %d, want 4", len(waves))
	}
	assertWaveContains(t, waves[0], "deps")
	assertWaveContains(t, waves[1], "lint", "test", "build")
	assertWaveContains(t, waves[2], "approve-production")
	assertWaveContains(t, waves[3], "deploy")
}

func assertWaveContains(t *testing.T, wave []pipeline.Step, names ...string) {
	t.Helper()

	nameSet := make(map[string]bool, len(wave))
	for _, s := range wave {
		nameSet[s.Name] = true
	}

	for _, name := range names {
		if !nameSet[name] {
			t.Errorf("wave does not contain step %q (has: %v)", name, waveNames(wave))
		}
	}

	if len(wave) != len(names) {
		t.Errorf("wave has %d steps %v, expected %d %v", len(wave), waveNames(wave), len(names), names)
	}
}

func waveNames(wave []pipeline.Step) []string {
	names := make([]string, len(wave))
	for i, s := range wave {
		names[i] = s.Name
	}
	return names
}
