package ci

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// stepByName finds a compiled wave step across all waves.
func stepByName(waves [][]pipeline.Step, name string) (pipeline.Step, bool) {
	for _, w := range waves {
		for _, s := range w {
			if s.Name == name {
				return s, true
			}
		}
	}
	return pipeline.Step{}, false
}

// C1: a job's `when:` threads through compile into the engine's pipeline.Step.When
// (which was previously always left ""), so the engine's outcome gate sees it.
func TestCompile_JobWhenThreadsToStep(t *testing.T) {
	p, err := Parse([]byte(`
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  build:
    steps: [{ run: make build }]
  rollback:
    needs: [build]
    when: onFailure
    steps: [{ run: ./rollback.sh }]
  notify:
    needs: [build]
    when: always
    steps: [{ run: ./notify.sh }]
`))
	require.NoError(t, err)
	waves, err := Compile(p, "")
	require.NoError(t, err)

	rollback, ok := stepByName(waves, "rollback")
	require.True(t, ok)
	assert.Equal(t, "onFailure", rollback.When, "onFailure job compiles to When=onFailure")

	notify, ok := stepByName(waves, "notify")
	require.True(t, ok)
	assert.Equal(t, "always", notify.When, "always job compiles to When=always")

	build, ok := stepByName(waves, "build")
	require.True(t, ok)
	assert.Equal(t, "", build.When, "a job with no when: stays default (onSuccess)")
}

// C1: job-level `when:` is enum-validated with a did-you-mean.
func TestValidate_JobWhen(t *testing.T) {
	valid, err := Parse([]byte(`
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  a: { steps: [{ run: echo hi }] }
  b: { needs: [a], when: always, steps: [{ run: echo bye }] }
`))
	require.NoError(t, err)
	assert.Empty(t, valid.ValidateDetailed().Errors(), "when: always is accepted")

	_, err = Parse([]byte(`
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  a: { steps: [{ run: echo hi }] }
  b: { needs: [a], when: onFailuer, steps: [{ run: echo bye }] }
`))
	require.Error(t, err, "a misspelled when: is rejected")
	assert.Contains(t, err.Error(), "jobs.b.when", "error is anchored to the field")
	assert.Contains(t, err.Error(), "onFailure", "suggests the closest valid value")
}

// C1: the status functions success()/failure()/always() are available to a job's
// if: — they validate clean (validates-clean-runs-clean, since validation and
// runtime share buildEngineExprContext).
func TestValidate_IfStatusFunctions(t *testing.T) {
	p, err := Parse([]byte(`
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  build:
    steps: [{ run: make build }]
  notify:
    needs: [build]
    when: always
    if: ${{ failure() }}
    steps: [{ run: ./notify.sh failed }]
  celebrate:
    needs: [build]
    if: ${{ success() && git.branch == 'main' }}
    steps: [{ run: ./celebrate.sh }]
`))
	require.NoError(t, err)
	assert.Empty(t, p.ValidateDetailed().Errors(), "failure()/success() in if: must validate clean")

	waves, err := Compile(p, "")
	require.NoError(t, err)
	notify, ok := stepByName(waves, "notify")
	require.True(t, ok)
	assert.Equal(t, "${{ failure() }}", notify.If)
	assert.Equal(t, "always", notify.When)
}

// C1: a realistic release pipeline (deploy → rollback-on-failure + always-notify)
// compiles and validates clean end to end.
func TestValidate_ReleaseRollbackExample(t *testing.T) {
	p, err := Parse([]byte(`
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  deploy:
    steps: [{ run: ./deploy.sh }]
  rollback:
    needs: [deploy]
    when: onFailure
    steps: [{ run: ./rollback.sh }]
  notify:
    needs: [deploy, rollback]
    when: always
    if: ${{ failure() }}
    steps: [{ run: ./notify.sh }]
`))
	require.NoError(t, err)
	assert.Empty(t, p.ValidateDetailed().Errors())
	waves, err := Compile(p, "")
	require.NoError(t, err)
	_, ok := stepByName(waves, "rollback")
	assert.True(t, ok)
}
