package ci

import (
	"strings"
	"testing"

	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validBase wraps a jobs fragment in a minimal valid pipeline.
func validBase(jobs string) string {
	return `
image: golang:1.26
triggers:
  push: { branches: [main] }
jobs:
` + jobs
}

// TestParse_UnknownFieldRejected: strict decoding — a typo'd field must be an
// error, never a silent drop.
func TestParse_UnknownFieldRejected(t *testing.T) {
	_, err := Parse([]byte(validBase(`
  build:
    timout: 5m
    steps:
      - run: make
`)))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timout", "the error should name the unknown field")
}

// TestParse_UnknownTriggerRejected: `cron:` or `pull-request:` (hyphen) must
// error, not vanish.
func TestParse_UnknownTriggerRejected(t *testing.T) {
	_, err := Parse([]byte(`
image: golang:1.26
triggers:
  pull-request: { branches: [main] }
jobs:
  build:
    steps: [{run: make}]
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown trigger")
	assert.Contains(t, err.Error(), "pull-request")
}

// TestParse_AliasBombRejected ports the billion-laughs defense to the ci dialect.
func TestParse_AliasBombRejected(t *testing.T) {
	bomb := `
a: &a [x, x, x, x, x, x, x, x, x, x]
b: &b [*a, *a, *a, *a, *a, *a, *a, *a, *a, *a]
c: &c [*b, *b, *b, *b, *b, *b, *b, *b, *b, *b]
d: &d [*c, *c, *c, *c, *c, *c, *c, *c, *c, *c]
e: &e [*d, *d, *d, *d, *d, *d, *d, *d, *d, *d]
f: &f [*e, *e, *e, *e, *e, *e, *e, *e, *e, *e]
`
	_, err := Parse([]byte(bomb))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many nodes")
}

// TestParse_SizeCap rejects oversized documents before decoding.
func TestParse_SizeCap(t *testing.T) {
	_, err := Parse([]byte("# " + strings.Repeat("x", 1<<20)))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

// TestValidate_UnimplementedFeaturesRejected: every schema-accepted feature the
// engine does not enforce yet must be a loud validation error.
func TestValidate_UnimplementedFeaturesRejected(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "schedule trigger",
			yaml: `
image: golang:1.26
triggers:
  schedule: { cron: "0 2 * * *" }
jobs:
  build: {steps: [{run: make}]}
`,
			wantErr: "schedule triggers are not yet supported",
		},
		{
			name: "promotion trigger",
			yaml: `
image: golang:1.26
triggers:
  promotion: { environments: [prod] }
jobs:
  build: {steps: [{run: make}]}
`,
			wantErr: "promotion triggers are not yet supported",
		},
		{
			name: "webhook trigger",
			yaml: `
image: golang:1.26
triggers:
  webhook: {}
jobs:
  build: {steps: [{run: make}]}
`,
			wantErr: "webhook triggers are not yet supported",
		},
		{
			name: "queue-mode concurrency",
			yaml: `
image: golang:1.26
concurrency: { group: main }
triggers:
  push: { branches: [main] }
jobs:
  build: {steps: [{run: make}]}
`,
			wantErr: "queued concurrency groups are not yet supported",
		},
		{
			name: "job-level concurrency",
			yaml: validBase(`
  build:
    concurrency: { group: g, cancelInProgress: true }
    steps: [{run: make}]
`),
			wantErr: "job-level concurrency",
		},
		{
			name: "matrix failFast",
			yaml: validBase(`
  test:
    failFast: true
    matrix: { go: ["1.25", "1.26"] }
    steps: [{run: go test}]
`),
			wantErr: "failFast",
		},
		{
			name: "matrix maxParallel",
			yaml: validBase(`
  test:
    maxParallel: 2
    matrix: { go: ["1.25", "1.26"] }
    steps: [{run: go test}]
`),
			wantErr: "maxParallel",
		},
		{
			name: "external secret provider",
			yaml: validBase(`
  build:
    secrets:
      - { from: "vault:kv/data/ci#token", env: TOKEN }
    steps: [{run: make}]
`),
			wantErr: "external secret providers",
		},
		{
			name: "file secret target",
			yaml: validBase(`
  build:
    secrets:
      - { name: kubeconfig, file: /secrets/kubeconfig }
    steps: [{run: make}]
`),
			wantErr: "file-mounted secrets",
		},
		{
			name: "matrix artifacts",
			yaml: validBase(`
  build:
    matrix: { go: ["1.25", "1.26"] }
    artifacts: [bin/]
    steps: [{run: make}]
`),
			wantErr: "matrix job",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestCompile_ArtifactsFlowAlongNeeds: producer's artifacts: become upload
// declarations; consumers with needs: get matching download declarations.
func TestCompile_ArtifactsFlowAlongNeeds(t *testing.T) {
	p, err := Parse([]byte(validBase(`
  build:
    artifacts: [bin/, dist/]
    steps: [{run: make}]
  deploy:
    needs: [build]
    steps: [{run: ./deploy.sh}]
  notify:
    needs: [deploy]
    steps: [{run: ./notify.sh}]
`)))
	require.NoError(t, err)

	waves, err := Compile(p, "")
	require.NoError(t, err)

	var build, deploy, notify *pipeline.Step
	for wi := range waves {
		for si := range waves[wi] {
			s := &waves[wi][si]
			switch s.Name {
			case "build":
				build = s
			case "deploy":
				deploy = s
			case "notify":
				notify = s
			}
		}
	}
	require.NotNil(t, build)
	require.NotNil(t, deploy)
	require.NotNil(t, notify)

	// Producer uploads.
	require.Len(t, build.Outputs, 2)
	assert.Equal(t, "bin/", build.Outputs[0].Path)

	// Direct consumer downloads from the producer.
	require.Len(t, deploy.Inputs, 2)
	assert.Equal(t, "build", deploy.Inputs[0].From)
	assert.Equal(t, "bin/", deploy.Inputs[0].Path)

	// Non-adjacent job gets nothing (deploy declared no artifacts).
	assert.Empty(t, notify.Inputs)
}
