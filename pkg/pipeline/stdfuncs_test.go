package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// C5: fromJSON/toJSON/format are available in every expression context and
// behave as GitHub Actions users expect. These feed runtime fan-out (fromJSON on
// a step output) and dynamic string building (format).
func TestStdExprFuncs(t *testing.T) {
	ctx := exprValidationContext() // any builder works; they all merge StdExprFuncs

	t.Run("fromJSON array", func(t *testing.T) {
		v, err := EvalExpr(`${{ fromJSON('[1,2,3]')[1] }}`, ctx)
		require.NoError(t, err)
		assert.EqualValues(t, 2, v)
	})

	t.Run("fromJSON object length", func(t *testing.T) {
		v, err := EvalExpr(`${{ len(fromJSON('{"a":1,"b":2}')) }}`, ctx)
		require.NoError(t, err)
		assert.EqualValues(t, 2, v)
	})

	t.Run("fromJSON invalid is a loud error", func(t *testing.T) {
		_, err := EvalExpr(`${{ fromJSON('not json') }}`, ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fromJSON")
	})

	t.Run("toJSON roundtrip", func(t *testing.T) {
		v, err := EvalExpr(`${{ toJSON(fromJSON('[1,2]')) }}`, ctx)
		require.NoError(t, err)
		assert.Equal(t, "[1,2]", v)
	})

	t.Run("format positional", func(t *testing.T) {
		v, err := EvalExpr(`${{ format('{0}-{1}-{0}', 'a', 'b') }}`, ctx)
		require.NoError(t, err)
		assert.Equal(t, "a-b-a", v)
	})

	t.Run("format out-of-range placeholder is left verbatim", func(t *testing.T) {
		v, err := EvalExpr(`${{ format('{0}-{5}', 'a') }}`, ctx)
		require.NoError(t, err)
		assert.Equal(t, "a-{5}", v)
	})
}

// Brace-escaping is tested against formatTemplate directly: a literal }} inside
// a ${{ }} template collides with the flint template delimiter, so it can't be
// exercised through EvalExpr.
func TestFormatTemplate_Escaping(t *testing.T) {
	assert.Equal(t, "{x}", formatTemplate("{{{0}}}", []any{"x"}))
	assert.Equal(t, "{0}", formatTemplate("{{0}}", nil))
	assert.Equal(t, "a-b", formatTemplate("{0}-{1}", []any{"a", "b"}))
	assert.Equal(t, "100%", formatTemplate("{0}%", []any{100}))
}

// C5: the functions compile-check in the validation context (validates-clean).
func TestStdExprFuncs_ValidateInIf(t *testing.T) {
	for _, expr := range []string{
		`${{ len(fromJSON('[1]')) > 0 }}`,
		`${{ format('{0}', branch) == 'main' }}`,
		`${{ toJSON(inputs) != '' }}`,
	} {
		require.NoError(t, compileExpr(expr, exprValidationContext()), "%s must type-check", expr)
	}
}
