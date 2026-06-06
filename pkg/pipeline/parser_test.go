package pipeline_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

func TestParse_Minimal(t *testing.T) {
	data := readTestdata(t, "minimal.yaml")
	p, err := pipeline.Parse(data)
	require.NoError(t, err)

	require.Len(t, p.Steps, 1)
	assert.Equal(t, "hello", p.Steps[0].Name)
	assert.Equal(t, "echo hello", p.Steps[0].Run.String())
	assert.Equal(t, "run", p.Steps[0].ExecType())
	assert.NotNil(t, p.Triggers.Push)
	assert.Equal(t, []string{"main"}, p.Triggers.Push.Branches)
}

func TestParse_CI(t *testing.T) {
	data := readTestdata(t, "ci.yaml")
	p, err := pipeline.Parse(data)
	require.NoError(t, err)

	// Triggers.
	assert.NotNil(t, p.Triggers.PullRequest)
	assert.Equal(t, []string{"main"}, p.Triggers.PullRequest.Branches)
	assert.NotNil(t, p.Triggers.Push)
	assert.Equal(t, []string{"main", "feature/**"}, p.Triggers.Push.Branches)
	assert.NotNil(t, p.Triggers.Manual)
	require.Len(t, p.Triggers.Manual.Inputs, 1)
	assert.Equal(t, "skip_tests", p.Triggers.Manual.Inputs[0].Name)

	// Steps.
	require.Len(t, p.Steps, 6)
	stepNames := make(map[string]string)
	for _, s := range p.Steps {
		stepNames[s.Name] = s.ExecType()
	}
	assert.Equal(t, "run", stepNames["deps"])
	assert.Equal(t, "run", stepNames["lint"])
	assert.Equal(t, "run", stepNames["test"])
	assert.Equal(t, "run", stepNames["build"])

	// ContinueOnError.
	lint := findStep(p, "lint")
	assert.True(t, lint.ContinueOnError)

	// Services.
	testStep := findStep(p, "test")
	require.Len(t, testStep.Services, 1)
	assert.Equal(t, "postgres", testStep.Services[0].Name)

	// Retry.
	require.NotNil(t, testStep.Retry)
	assert.Equal(t, 2, testStep.Retry.Attempts)

	// Cache.
	build := findStep(p, "build")
	require.NotNil(t, build.Cache)

	// When.
	report := findStep(p, "report")
	assert.Equal(t, "always", report.When)
}

func TestParse_Deploy(t *testing.T) {
	data := readTestdata(t, "deploy.yaml")
	p, err := pipeline.Parse(data)
	require.NoError(t, err)

	assert.Equal(t, []string{"staging", "production"}, p.Environments)
	require.Len(t, p.Triggers.Promotion, 1)
	assert.Equal(t, "staging", p.Triggers.Promotion[0].From)

	// Gate.
	approve := findStep(p, "approve")
	require.NotNil(t, approve.Gate)
	assert.Equal(t, []string{"role:release-manager", "team:platform"}, approve.Gate.Approvers)

	// Nested steps.
	push := findStep(p, "push")
	assert.True(t, push.IsNested())
	require.Len(t, push.Steps, 3)
}

func TestParse_Errors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"no triggers", "steps:\n  - name: a\n    run: echo"},
		{"no steps", "triggers:\n  push:\n    branches: [main]\nsteps: []"},
		{"missing step name", "triggers:\n  push:\n    branches: [main]\nsteps:\n  - run: echo"},
		{"duplicate step name", "triggers:\n  push:\n    branches: [main]\nsteps:\n  - name: a\n    run: echo\n  - name: a\n    run: echo"},
		{"no exec type", "triggers:\n  push:\n    branches: [main]\nsteps:\n  - name: a\n    image: alpine"},
		{"unknown dependsOn", "triggers:\n  push:\n    branches: [main]\nsteps:\n  - name: a\n    run: echo\n    dependsOn: [missing]"},
		{"self reference", "triggers:\n  push:\n    branches: [main]\nsteps:\n  - name: a\n    run: echo\n    dependsOn: [a]"},
		{"invalid yaml", "triggers: [[["},
		{"promotion no from", "triggers:\n  promotion:\n    - environments: [prod]\nsteps:\n  - name: a\n    run: echo"},
		{"promotion no envs", "triggers:\n  promotion:\n    - from: staging\nsteps:\n  - name: a\n    run: echo"},
		{"sub-step with runner", "triggers:\n  push:\n    branches: [main]\nsteps:\n  - name: g\n    steps:\n      - name: a\n        run: echo\n        runner: gpu"},
		{"deeply nested", "triggers:\n  push:\n    branches: [main]\nsteps:\n  - name: g\n    steps:\n      - name: a\n        steps:\n          - name: b\n            run: echo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pipeline.Parse([]byte(tt.yaml))
			require.Error(t, err)
			assert.True(t, errors.Is(err, pipeline.ErrInvalidPipeline), "got: %v", err)
		})
	}
}

func TestParse_PullRequestShorthand(t *testing.T) {
	p, err := pipeline.Parse([]byte(`
triggers:
  pull_request: [main, develop]
steps:
  - name: test
    run: echo test
`))
	require.NoError(t, err)
	require.NotNil(t, p.Triggers.PullRequest)
	assert.Equal(t, []string{"main", "develop"}, p.Triggers.PullRequest.Branches)
}

func TestParse_PromotionSingleAndArray(t *testing.T) {
	// Single promotion.
	p, err := pipeline.Parse([]byte(`
triggers:
  push:
    branches: [main]
    environments: [staging]
  promotion:
    from: staging
    environments: [production]
steps:
  - name: a
    run: echo
`))
	require.NoError(t, err)
	require.Len(t, p.Triggers.Promotion, 1)

	// Array of promotions.
	p, err = pipeline.Parse([]byte(`
triggers:
  push:
    branches: [main]
    environments: [dev]
  promotion:
    - from: dev
      environments: [staging]
    - from: staging
      environments: [production]
steps:
  - name: a
    run: echo
`))
	require.NoError(t, err)
	require.Len(t, p.Triggers.Promotion, 2)
	assert.Equal(t, "dev", p.Triggers.Promotion[0].From)
	assert.Equal(t, "staging", p.Triggers.Promotion[1].From)
}

func TestParse_AllTriggerTypes(t *testing.T) {
	// Pipeline with all 7 trigger types simultaneously.
	yaml := `
triggers:
  push:
    branches: [main]
  pull_request: [main]
  manual:
    environments: [staging]
  schedule:
    cron: "0 2 * * *"
  tag:
    patterns: ["v*"]
  promotion:
    - from: staging
      environments: [production]
  webhook:
    secret: test-secret
steps:
  - name: test
    run: echo test
`
	p, err := pipeline.Parse([]byte(yaml))
	require.NoError(t, err)
	assert.True(t, p.Triggers.HasAny())
	assert.NotNil(t, p.Triggers.Push)
	assert.NotNil(t, p.Triggers.PullRequest)
	assert.NotNil(t, p.Triggers.Manual)
	assert.NotNil(t, p.Triggers.Schedule)
	assert.NotNil(t, p.Triggers.Tag)
	assert.Len(t, p.Triggers.Promotion, 1)
	assert.NotNil(t, p.Triggers.Webhook)
}

func TestParse_StepWithAllFields(t *testing.T) {
	yaml := `
triggers:
  push:
    branches: [main]
steps:
  - name: full-step
    image: node:22
    run: npm test
    runner: large
    shell: bash
    workingDir: /workspace/frontend
    dependsOn: []
    environments: [staging]
    timeout: 30m
    if: "${{ branch == 'main' }}"
    when: onSuccess
    continueOnError: true
    retry:
      attempts: 3
      delay: 10s
    env:
      NODE_ENV: test
    outputs:
      - path: /workspace/dist
    services:
      - name: redis
        image: redis:7
    cache:
      key: "npm-${{ hashFiles('package-lock.json') }}"
      paths: [node_modules]
    matrix:
      node: ["20", "22"]
`
	p, err := pipeline.Parse([]byte(yaml))
	require.NoError(t, err)
	s := p.Steps[0]
	assert.Equal(t, "full-step", s.Name)
	assert.Equal(t, "node:22", s.Image)
	assert.Equal(t, "bash", s.Shell)
	assert.Equal(t, "/workspace/frontend", s.WorkingDir)
	assert.Equal(t, "30m", s.Timeout)
	assert.True(t, s.ContinueOnError)
	assert.NotNil(t, s.Retry)
	assert.Equal(t, 3, s.Retry.Attempts)
	assert.Len(t, s.Services, 1)
	assert.NotNil(t, s.Cache)
	assert.Len(t, s.Matrix, 1)
}

func TestParse_SingleStepPipeline(t *testing.T) {
	yaml := `
triggers:
  manual: {}
steps:
  - name: deploy
    run: make deploy
`
	p, err := pipeline.Parse([]byte(yaml))
	require.NoError(t, err)
	assert.Len(t, p.Steps, 1)
}

func TestParse_RejectsAnchorBomb(t *testing.T) {
	// Classic "billion laughs": each level references the previous nine times.
	bomb := `
a: &a ["x","x","x","x","x","x","x","x","x"]
b: &b [*a,*a,*a,*a,*a,*a,*a,*a,*a]
c: &c [*b,*b,*b,*b,*b,*b,*b,*b,*b]
d: &d [*c,*c,*c,*c,*c,*c,*c,*c,*c]
e: &e [*d,*d,*d,*d,*d,*d,*d,*d,*d]
f: &f [*e,*e,*e,*e,*e,*e,*e,*e,*e]
g: [*f,*f,*f,*f,*f,*f,*f,*f,*f]
`
	_, err := pipeline.Parse([]byte(bomb))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nodes")
}

func TestParse_RejectsOversizedEnvValue(t *testing.T) {
	big := strings.Repeat("x", pipeline.MaxEnvValueSize+1)
	yaml := `
triggers:
  push:
    branches: [main]
steps:
  - name: test
    run: echo
    env:
      BIG: "` + big + `"
`
	_, err := pipeline.Parse([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "env value")
}

func TestParse_MatrixExceedsMaxCombinations(t *testing.T) {
	vals := strings.TrimSuffix(strings.Repeat(`"x",`, 257), ",")
	yaml := "triggers:\n  push:\n    branches: [main]\nsteps:\n  - name: test\n    run: echo\n    matrix:\n      a: [" + vals + "]\n"
	_, err := pipeline.Parse([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "combinations")
}

func TestParse_UnicodeStepName(t *testing.T) {
	yaml := `
triggers:
  push:
    branches: [main]
steps:
  - name: "tëst-ünïcode"
    run: echo
`
	p, err := pipeline.Parse([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, "tëst-ünïcode", p.Steps[0].Name)
}
