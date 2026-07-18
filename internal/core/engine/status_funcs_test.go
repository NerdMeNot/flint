package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// C1: the if: status functions reflect THIS step's dependency-subgraph outcome.
// buildEngineExprContext is the single builder shared by runtime and validation,
// so proving it here proves the runtime semantics.
func TestStatusFunctions_ReflectUpstreamOutcome(t *testing.T) {
	in := StartWorkflowInput{Kind: "ci", Env: map[string]string{}}
	in.normalizeInputs()

	eval := func(upstreamFailed bool, expr string) bool {
		ctx := buildEngineExprContext(in, nil, nil, upstreamFailed)
		got, err := pipeline.EvalCondition(expr, ctx)
		require.NoError(t, err)
		return got
	}

	// Upstream succeeded.
	assert.True(t, eval(false, "${{ success() }}"))
	assert.False(t, eval(false, "${{ failure() }}"))
	assert.True(t, eval(false, "${{ always() }}"))

	// An ancestor failed.
	assert.False(t, eval(true, "${{ success() }}"))
	assert.True(t, eval(true, "${{ failure() }}"))
	assert.True(t, eval(true, "${{ always() }}"))

	// Compound conditions compose with the rest of the context.
	assert.True(t, eval(true, "${{ failure() && always() }}"))
	assert.False(t, eval(false, "${{ failure() || (success() && false) }}"))
}

// C1: the validation context (shared builder) exposes the same functions, so an
// if: using them type-checks — the validates-clean-runs-clean guarantee.
func TestValidationContext_HasStatusFunctions(t *testing.T) {
	ctx := ValidationExprContext()
	for _, fn := range []string{"${{ success() }}", "${{ failure() }}", "${{ always() }}"} {
		_, err := pipeline.EvalCondition(fn, ctx)
		assert.NoError(t, err, "%s must type-check against the validation context", fn)
	}
}
