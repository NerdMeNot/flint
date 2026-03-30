package pipeline_test

import (
	"errors"
	"os"
	"testing"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

func TestParse_Minimal(t *testing.T) {
	data := readTestdata(t, "minimal.yaml")
	p, err := pipeline.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if p.Name != "Minimal" {
		t.Errorf("Name = %q, want %q", p.Name, "Minimal")
	}
	if len(p.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1", len(p.Steps))
	}
	if p.Steps[0].Name != "hello" {
		t.Errorf("Steps[0].Name = %q, want %q", p.Steps[0].Name, "hello")
	}
	if p.Steps[0].Run != "echo hello" {
		t.Errorf("Steps[0].Run = %q, want %q", p.Steps[0].Run, "echo hello")
	}
	if p.Steps[0].ExecType() != "run" {
		t.Errorf("Steps[0].ExecType() = %q, want %q", p.Steps[0].ExecType(), "run")
	}
}

func TestParse_FullCI(t *testing.T) {
	data := readTestdata(t, "ci.yaml")
	p, err := pipeline.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if p.Name != "Skills Service CI" {
		t.Errorf("Name = %q", p.Name)
	}

	// Secrets — mixed string and map forms.
	if len(p.Secrets) != 2 {
		t.Fatalf("len(Secrets) = %d, want 2", len(p.Secrets))
	}
	if p.Secrets[0].Name != "GITHUB_TOKEN" || p.Secrets[0].Optional {
		t.Errorf("Secrets[0] = %+v", p.Secrets[0])
	}
	if p.Secrets[1].Name != "SLACK_WEBHOOK" || !p.Secrets[1].Optional {
		t.Errorf("Secrets[1] = %+v", p.Secrets[1])
	}

	// Triggers.
	if p.Triggers.Push == nil {
		t.Fatal("Push trigger is nil")
	}
	if len(p.Triggers.Push.Branches) != 2 {
		t.Errorf("Push.Branches = %v", p.Triggers.Push.Branches)
	}
	if p.Triggers.Manual == nil || len(p.Triggers.Manual.Inputs) != 1 {
		t.Errorf("Manual trigger inputs = %v", p.Triggers.Manual)
	}

	// Concurrency.
	if p.Concurrency == nil || p.Concurrency.Mode != "cancel" {
		t.Errorf("Concurrency = %+v", p.Concurrency)
	}

	// Steps count.
	if len(p.Steps) != 6 {
		t.Fatalf("len(Steps) = %d, want 6", len(p.Steps))
	}

	// Step types.
	steps := map[string]string{
		"deps":               "run",
		"lint":               "run",
		"test":               "do",
		"build":              "run",
		"approve-production": "gate",
		"deploy":             "run",
	}
	for _, s := range p.Steps {
		want, ok := steps[s.Name]
		if !ok {
			t.Errorf("unexpected step %q", s.Name)
			continue
		}
		if got := s.ExecType(); got != want {
			t.Errorf("step %q: ExecType() = %q, want %q", s.Name, got, want)
		}
	}

	// Do tasks.
	testStep := findStep(p, "test")
	if len(testStep.Do) != 2 {
		t.Fatalf("test step Do length = %d, want 2", len(testStep.Do))
	}
	if testStep.Do[0].Name != "run tests" {
		t.Errorf("Do[0].Name = %q", testStep.Do[0].Name)
	}

	// Runner ref — structured form.
	buildStep := findStep(p, "build")
	if buildStep.Runner == nil || buildStep.Runner.Size != "large" {
		t.Errorf("build step runner = %+v", buildStep.Runner)
	}

	// Runner ref — string form.
	depsStep := findStep(p, "deps")
	if depsStep.Runner == nil || depsStep.Runner.Name != "standard" {
		t.Errorf("deps step runner = %+v", depsStep.Runner)
	}

	// Gate.
	gate := findStep(p, "approve-production")
	if gate.Gate == nil || gate.Gate.Message != "Deploy to production?" {
		t.Errorf("gate = %+v", gate.Gate)
	}
	if len(gate.Gate.Form) != 1 {
		t.Errorf("gate form fields = %d, want 1", len(gate.Gate.Form))
	}

	// Services.
	if len(testStep.Services) != 1 {
		t.Fatalf("test step services = %d", len(testStep.Services))
	}
	pg := testStep.Services["postgres"]
	if pg.Image != "postgres:16" {
		t.Errorf("postgres image = %q", pg.Image)
	}

	// Lifecycle.
	lintStep := findStep(p, "lint")
	if lintStep.Lifecycle == nil || lintStep.Lifecycle.OnFailure != "continue" {
		t.Errorf("lint lifecycle = %+v", lintStep.Lifecycle)
	}
}

func TestParse_Errors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"missing pipeline name", "steps:\n  - name: a\n    run: echo"},
		{"no steps", "pipeline: Test\nsteps: []"},
		{"missing step name", "pipeline: Test\nsteps:\n  - run: echo"},
		{"duplicate step name", "pipeline: Test\nsteps:\n  - name: a\n    run: echo\n  - name: a\n    run: echo"},
		{"no exec type", "pipeline: Test\nsteps:\n  - name: a\n    image: alpine"},
		{"unknown after ref", "pipeline: Test\nsteps:\n  - name: a\n    run: echo\n    after: [missing]"},
		{"self reference", "pipeline: Test\nsteps:\n  - name: a\n    run: echo\n    after: [a]"},
		{"invalid yaml", "pipeline: [[["},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pipeline.Parse([]byte(tt.yaml))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, pipeline.ErrInvalidPipeline) {
				t.Errorf("expected ErrInvalidPipeline, got: %v", err)
			}
		})
	}
}

func findStep(p *pipeline.Pipeline, name string) *pipeline.Step {
	for i := range p.Steps {
		if p.Steps[i].Name == name {
			return &p.Steps[i]
		}
	}
	return nil
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading testdata/%s: %v", name, err)
	}
	return data
}
