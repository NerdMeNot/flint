package ci

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// mustDecode decodes YAML into a Pipeline without running validation, so the
// validator's own behaviour can be tested in isolation.
func mustDecode(t *testing.T, src string) *Pipeline {
	t.Helper()
	var p Pipeline
	require.NoError(t, yaml.Unmarshal([]byte(src), &p))
	return &p
}

func issueByCode(r *pipeline.ValidationResult, code string) *pipeline.ValidationIssue {
	for i := range r.Issues {
		if r.Issues[i].Code == code {
			return &r.Issues[i]
		}
	}
	return nil
}

func TestValidateDetailed_NeedsDidYouMean(t *testing.T) {
	p := mustDecode(t, `
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  build: { steps: [{ run: make }] }
  test:  { needs: [biuld], steps: [{ run: make test }] }
`)
	r := p.ValidateDetailed()
	assert.False(t, r.Valid())

	issue := issueByCode(r, pipeline.CodeUnknownRef)
	require.NotNil(t, issue)
	assert.Contains(t, issue.Message, `needs unknown job "biuld"`)
	assert.Contains(t, issue.Suggestion, `Did you mean "build"?`)
	assert.Equal(t, "jobs.test.needs[0]", issue.Field)
}

func TestValidateDetailed_CollectsAllIssues(t *testing.T) {
	p := mustDecode(t, `
triggers: { push: { branches: [main] } }
jobs:
  a: { steps: [{ run: x, timeout: "5 minutes" }] }
  b: { needs: [ghost], steps: [{ run: y }], image: alpine }
`)
	r := p.ValidateDetailed()
	errs := r.Errors()
	// Missing image on a, bad duration on a's step, unknown needs on b —
	// ALL collected in one pass, not fail-first.
	assert.GreaterOrEqual(t, len(errs), 3)
}

func TestValidateDetailed_ExpressionChecking(t *testing.T) {
	// Valid runtime-context expressions pass; unknown namespaces fail.
	p := mustDecode(t, `
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  good:
    if: ${{ git.branch == "main" }}
    steps: [{ run: make }]
  bad:
    if: ${{ nonexistent.thing == "x" }}
    steps: [{ run: make }]
`)
	r := p.ValidateDetailed()
	errs := r.Errors()
	require.Len(t, errs, 1, "only the unknown-namespace expression should fail: %v", errs)
	assert.Equal(t, pipeline.CodeInvalidExpression, errs[0].Code)
	assert.Equal(t, "jobs.bad.if", errs[0].Field)
	assert.Contains(t, errs[0].Suggestion, "git.*")
}

func TestValidateDetailed_ConstantConditionWarns(t *testing.T) {
	p := mustDecode(t, `
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  a:
    if: "true"
    steps: [{ run: make }]
`)
	r := p.ValidateDetailed()
	assert.True(t, r.Valid(), "constant condition is a warning, not an error")
	require.Len(t, r.Warnings(), 1)
	assert.Contains(t, r.Warnings()[0].Message, "constant")
}

func TestValidateDetailed_MatrixDimensionDidYouMean(t *testing.T) {
	p := mustDecode(t, `
triggers: { push: { branches: [main] } }
jobs:
  test:
    image: golang:${{ matrix.goo }}
    matrix: { go: ["1.25", "1.26"] }
    steps: [{ run: go test ./... }]
`)
	r := p.ValidateDetailed()
	issue := issueByCode(r, pipeline.CodeUnknownRef)
	require.NotNil(t, issue)
	assert.Contains(t, issue.Message, `unknown matrix dimension "goo"`)
	assert.Contains(t, issue.Suggestion, `Did you mean "go"?`)
}

func TestValidateDetailed_MatrixInExpressionAllowed(t *testing.T) {
	p := mustDecode(t, `
triggers: { push: { branches: [main] } }
jobs:
  test:
    image: golang:1.26
    matrix: { go: ["1.25", "1.26"] }
    if: ${{ matrix.go == "1.26" && git.branch == "main" }}
    steps: [{ run: go test ./... }]
`)
	r := p.ValidateDetailed()
	assert.True(t, r.Valid(), "matrix refs in matrix-job expressions are valid: %v", r.Errors())
}

func TestValidateDetailed_MatrixRefWithoutMatrix(t *testing.T) {
	p := mustDecode(t, `
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  build:
    if: ${{ matrix.go == "1.26" }}
    steps: [{ run: make }]
`)
	r := p.ValidateDetailed()
	issue := issueByCode(r, pipeline.CodeUnknownRef)
	require.NotNil(t, issue)
	assert.Contains(t, issue.Message, "has no matrix")
}

func TestValidateDetailed_CycleNamesPath(t *testing.T) {
	p := mustDecode(t, `
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  a: { needs: [c], steps: [{ run: x }] }
  b: { needs: [a], steps: [{ run: x }] }
  c: { needs: [b], steps: [{ run: x }] }
`)
	r := p.ValidateDetailed()
	issue := issueByCode(r, pipeline.CodeCycleDetected)
	require.NotNil(t, issue)
	assert.Contains(t, issue.Message, "→", "the cycle path should be spelled out")
}

func TestValidateDetailed_EnvironmentDidYouMean(t *testing.T) {
	p := mustDecode(t, `
image: alpine
environments: [staging, production]
triggers: { push: { branches: [main] } }
jobs:
  deploy:
    environments: [prodcution]
    steps: [{ run: ./deploy.sh }]
`)
	r := p.ValidateDetailed()
	issue := issueByCode(r, pipeline.CodeEnvMismatch)
	require.NotNil(t, issue)
	assert.Contains(t, issue.Suggestion, `Did you mean "production"?`)
	assert.Contains(t, issue.Suggestion, "staging")
}

// TestValidateDetailed_MatrixExprInterpolation locks the compile-side promise
// the validator relies on: matrix tokens inside if: expressions are replaced
// with quoted literals during expansion, so the engine never sees a matrix
// namespace.
func TestCompile_MatrixExprInterpolation(t *testing.T) {
	p, err := Parse([]byte(`
triggers: { push: { branches: [main] } }
jobs:
  test:
    image: golang:${{ matrix.go }}
    matrix: { go: ["1.25", "1.26"] }
    if: ${{ matrix.go == "1.26" }}
    env: { GO_VERSION: "${{ matrix.go }}" }
    steps:
      - run: go test ./...
        if: ${{ matrix.go != "1.25" }}
`))
	require.NoError(t, err)

	waves, err := Compile(p, "")
	require.NoError(t, err)

	var v126 *pipeline.Step
	for wi := range waves {
		for si := range waves[wi] {
			if waves[wi][si].Name == "test::1.26" {
				v126 = &waves[wi][si]
			}
		}
	}
	require.NotNil(t, v126)
	assert.Equal(t, `${{ "1.26" == "1.26" }}`, v126.If)
	assert.Equal(t, "1.26", v126.Env["GO_VERSION"])
	require.Len(t, v126.Steps, 1)
	assert.Equal(t, `${{ "1.26" != "1.25" }}`, v126.Steps[0].If)
	assert.NotContains(t, v126.If, "matrix.", "no matrix token may survive expansion")

	// And the interpolated condition actually evaluates under the runtime context.
	ok, err := pipeline.EvalCondition(v126.If, pipeline.ExprContext{})
	require.NoError(t, err)
	assert.True(t, ok)
}
