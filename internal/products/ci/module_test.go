package ci

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustModule(t *testing.T, src string) *Module {
	t.Helper()
	m, err := ParseModule([]byte(src))
	require.NoError(t, err)
	return m
}

// stepRuns returns the run command of each step in a job, for assertions.
func stepRuns(job Job) []string {
	var out []string
	for _, s := range job.Steps {
		if len(s.Run.Commands) > 0 {
			out = append(out, s.Run.Commands[0])
		}
	}
	return out
}

func TestResolve_JobModule_StepsHoleAndInterpolation(t *testing.T) {
	resolver := MapResolver{
		"go-ci": mustModule(t, `
name: go-ci
kind: job
inputs:
  go: { type: string, default: "1.26" }
  steps: { type: steps }
job:
  image: golang:${{ inputs.go }}
  steps:
    - run: go mod download
    - inject: ${{ inputs.steps }}
    - run: go test ./...
`),
	}

	p, err := Parse([]byte(`
triggers: { push: { branches: [main] } }
jobs:
  build:
    needs: []
    use: go-ci
    with:
      go: "1.27"
      steps:
        - run: go build ./cmd/app
`))
	require.NoError(t, err)

	out, err := ResolveModules(context.Background(), p, resolver)
	require.NoError(t, err)

	build := out.Jobs["build"]
	assert.Equal(t, "golang:1.27", build.Image) // input interpolated
	assert.Equal(t, "", build.Use)              // resolved away
	// steps-hole filled between the module's setup and teardown:
	assert.Equal(t, []string{"go mod download", "go build ./cmd/app", "go test ./..."}, stepRuns(build))

	// And it still compiles onto the engine.
	waves, err := Compile(out, "")
	require.NoError(t, err)
	assert.Equal(t, 0, waveOf(waves, "build"))
}

func TestResolve_StepModule_Inline(t *testing.T) {
	resolver := MapResolver{
		"apt-install": mustModule(t, `
name: apt-install
kind: steps
requires: { family: debian }
inputs:
  packages: { type: string, required: true }
steps:
  - run: apt-get update && apt-get install -y ${{ inputs.packages }}
`),
	}
	p, err := Parse([]byte(`
triggers: { push: { branches: [main] } }
jobs:
  tools:
    image: ubuntu:24.04
    steps:
      - use: apt-install
        with: { packages: "jq curl" }
      - run: jq --version
`))
	require.NoError(t, err)

	out, err := ResolveModules(context.Background(), p, resolver)
	require.NoError(t, err)
	assert.Equal(t, []string{"apt-get update && apt-get install -y jq curl", "jq --version"}, stepRuns(out.Jobs["tools"]))
}

func TestResolve_StepModule_EnvContractRejected(t *testing.T) {
	// apt-install requires debian, but the job image is rhel-family → reject.
	resolver := MapResolver{
		"apt-install": mustModule(t, `
name: apt-install
kind: steps
requires: { family: debian }
steps:
  - run: apt-get install -y jq
`),
	}
	p, err := Parse([]byte(`
triggers: { push: { branches: [main] } }
jobs:
  tools:
    image: rockylinux:9
    steps:
      - use: apt-install
`))
	require.NoError(t, err)

	_, err = ResolveModules(context.Background(), p, resolver)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires family")
}

func TestResolve_Extends_PipelineModule(t *testing.T) {
	resolver := MapResolver{
		"go-service": mustModule(t, `
name: go-service
kind: pipeline
inputs:
  service: { type: string, required: true }
jobs:
  build:
    image: golang:1.26
    steps:
      - run: go build -o bin/${{ inputs.service }} ./cmd/${{ inputs.service }}
  deploy:
    needs: [build]
    image: alpine
    steps:
      - run: deploy ${{ inputs.service }}
`),
	}
	p, err := Parse([]byte(`
extends: go-service
with: { service: orders }
triggers: { push: { branches: [main] } }
`))
	require.NoError(t, err)

	out, err := ResolveModules(context.Background(), p, resolver)
	require.NoError(t, err)
	assert.Equal(t, "", out.Extends)
	assert.Equal(t, []string{"go build -o bin/orders ./cmd/orders"}, stepRuns(out.Jobs["build"]))
	assert.Equal(t, []string{"deploy orders"}, stepRuns(out.Jobs["deploy"]))

	waves, err := Compile(out, "")
	require.NoError(t, err)
	assert.Greater(t, waveOf(waves, "deploy"), waveOf(waves, "build"))
}

func TestResolve_InputValidation(t *testing.T) {
	resolver := MapResolver{
		"needs-input": mustModule(t, `
name: needs-input
kind: steps
inputs:
  mode: { type: enum, options: [a, b] }
  must: { type: string, required: true }
steps:
  - run: echo ${{ inputs.mode }}
`),
	}
	// missing required input
	p, _ := Parse([]byte(`
triggers: { push: { branches: [main] } }
jobs: { j: { image: alpine, steps: [{ use: needs-input, with: { mode: a } }] } }
`))
	_, err := ResolveModules(context.Background(), p, resolver)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required input")

	// bad enum value
	p2, _ := Parse([]byte(`
triggers: { push: { branches: [main] } }
jobs: { j: { image: alpine, steps: [{ use: needs-input, with: { mode: z, must: x } }] } }
`))
	_, err = ResolveModules(context.Background(), p2, resolver)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in options")
}

func TestResolve_ActionDeferred(t *testing.T) {
	resolver := MapResolver{
		"slack": mustModule(t, `
name: slack
kind: action
run: { image: ghcr.io/acme/slack:1 }
`),
	}
	p, _ := Parse([]byte(`
triggers: { push: { branches: [main] } }
jobs: { j: { image: alpine, steps: [{ use: slack }] } }
`))
	_, err := ResolveModules(context.Background(), p, resolver)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "action modules not yet supported")
}

func TestResolve_NoModulesIsNoop(t *testing.T) {
	p, err := Parse([]byte(`
triggers: { push: { branches: [main] } }
jobs: { build: { image: alpine, steps: [{ run: make }] } }
`))
	require.NoError(t, err)
	out, err := ResolveModules(context.Background(), p, MapResolver{})
	require.NoError(t, err)
	assert.Equal(t, []string{"make"}, stepRuns(out.Jobs["build"]))
}
