package ci

import (
	"testing"

	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waveOf returns the index of the wave containing the named group-step, or -1.
func waveOf(waves [][]pipeline.Step, name string) int {
	for i, w := range waves {
		for _, s := range w {
			if s.Name == name {
				return i
			}
		}
	}
	return -1
}

func names(waves [][]pipeline.Step) []string {
	var out []string
	for _, w := range waves {
		for _, s := range w {
			out = append(out, s.Name)
		}
	}
	return out
}

func TestParseAndCompile_Basic(t *testing.T) {
	p, err := Parse([]byte(`
image: golang:1.26
triggers:
  push: { branches: [main] }
jobs:
  build:
    steps:
      - run: go build ./...
  lint:
    needs: [build]
    steps:
      - run: go vet ./...
  test:
    needs: [build]
    steps:
      - run: go test ./...
`))
	require.NoError(t, err)

	waves, err := Compile(p, "")
	require.NoError(t, err)

	// build in wave 0; lint and test in a later wave, after build.
	assert.Equal(t, 0, waveOf(waves, "build"))
	assert.Greater(t, waveOf(waves, "lint"), waveOf(waves, "build"))
	assert.Greater(t, waveOf(waves, "test"), waveOf(waves, "build"))
	assert.Len(t, names(waves), 3)

	// image default propagates onto the group-step.
	require.Equal(t, 0, waveOf(waves, "build"))
	assert.Equal(t, "golang:1.26", waves[0][0].Image)
}

func TestCompile_EnvFiltering(t *testing.T) {
	src := []byte(`
image: alpine
environments: [staging, production]
triggers:
  pull_request: { branches: [main] }
  push: { branches: [main], environments: [staging] }
jobs:
  test:
    steps: [{ run: make test }]
  deploy:
    needs: [test]
    environments: [staging, production]
    steps: [{ run: make deploy }]
`)
	p, err := Parse(src)
	require.NoError(t, err)

	// Plain run (PR): the env-scoped deploy job is dropped.
	plain, err := Compile(p, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"test"}, names(plain))

	// Staging run: deploy is included, after test.
	staging, err := Compile(p, "staging")
	require.NoError(t, err)
	assert.Greater(t, waveOf(staging, "deploy"), waveOf(staging, "test"))
}

func TestCompile_SkippedNeedIsSatisfied(t *testing.T) {
	// deploy needs build + approve; approve is production-only. On staging,
	// approve is dropped but deploy must still run (skipped need = satisfied).
	p, err := Parse([]byte(`
image: alpine
environments: [staging, production]
triggers: { push: { branches: [main], environments: [staging] } }
jobs:
  build:
    steps: [{ run: make build }]
  approve:
    needs: [build]
    environments: [production]
    gate: { approvers: [role:rm] }
  deploy:
    needs: [build, approve]
    environments: [staging, production]
    steps: [{ run: make deploy }]
`))
	require.NoError(t, err)

	staging, err := Compile(p, "staging")
	require.NoError(t, err)
	assert.Equal(t, -1, waveOf(staging, "approve"), "approve should be dropped on staging")
	assert.NotEqual(t, -1, waveOf(staging, "deploy"), "deploy should still run")
	assert.Greater(t, waveOf(staging, "deploy"), waveOf(staging, "build"))
}

func TestCompile_Matrix(t *testing.T) {
	p, err := Parse([]byte(`
triggers: { push: { branches: [main] } }
jobs:
  test:
    image: golang:${{ matrix.go }}
    matrix: { go: ["1.25", "1.26"] }
    steps:
      - run: go test -tags=${{ matrix.go }} ./...
`))
	require.NoError(t, err)

	waves, err := Compile(p, "")
	require.NoError(t, err)
	got := names(waves)
	assert.ElementsMatch(t, []string{"test::1.25", "test::1.26"}, got)

	// matrix value interpolated into image + step command.
	for _, w := range waves {
		for _, s := range w {
			if s.Name == "test::1.26" {
				assert.Equal(t, "golang:1.26", s.Image)
				require.Len(t, s.Steps, 1)
				assert.Equal(t, "go test -tags=1.26 ./...", s.Steps[0].Run.Commands[0])
			}
		}
	}
}

func TestValidate_Errors(t *testing.T) {
	cases := map[string]string{
		"no jobs": `
triggers: { push: { branches: [main] } }
jobs: {}`,
		"no triggers": `
jobs: { build: { steps: [{ run: x }] } }`,
		"steps and gate": `
triggers: { push: { branches: [main] } }
jobs:
  j: { steps: [{ run: x }], gate: { approvers: [a] } }`,
		"step both run and use": `
triggers: { push: { branches: [main] } }
jobs:
  j: { image: alpine, steps: [{ run: x, use: y }] }`,
		"needs unknown job": `
triggers: { push: { branches: [main] } }
jobs:
  j: { image: alpine, steps: [{ run: x }], needs: [ghost] }`,
		"dependency cycle": `
triggers: { push: { branches: [main] } }
jobs:
  a: { image: alpine, steps: [{ run: x }], needs: [b] }
  b: { image: alpine, steps: [{ run: y }], needs: [a] }`,
		"container job without image": `
triggers: { push: { branches: [main] } }
jobs:
  j: { steps: [{ run: x }] }`,
		"env not allowed by pipeline": `
environments: [staging]
triggers: { push: { branches: [main], environments: [staging] } }
jobs:
  j: { image: alpine, steps: [{ run: x }], environments: [production] }`,
		"secret two targets": `
triggers: { push: { branches: [main] } }
jobs:
  j:
    image: alpine
    secrets: [{ name: s, env: E, file: /f }]
    steps: [{ run: x }]`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(src))
			assert.Error(t, err)
		})
	}
}

func TestValidate_GateJobNeedsNoImage(t *testing.T) {
	_, err := Parse([]byte(`
triggers: { push: { branches: [main] } }
jobs:
  approve:
    gate: { approvers: [role:rm], minApprovals: 2 }
`))
	require.NoError(t, err)
}
