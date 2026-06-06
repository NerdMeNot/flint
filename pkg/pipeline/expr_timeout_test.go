package pipeline

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEvalExpr_Timeout verifies a long-running expression is cut off rather than
// allowed to run unbounded. Uses an internal-package test to tighten the timeout.
func TestEvalExpr_Timeout(t *testing.T) {
	orig := exprEvalTimeout
	exprEvalTimeout = time.Nanosecond
	t.Cleanup(func() { exprEvalTimeout = orig })

	_, err := EvalExpr("len(map(1..500000, {# * 2}))", ExprContext{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

// Sanity: a normal expression still evaluates fine under the default timeout.
func TestEvalExpr_NormalStillWorks(t *testing.T) {
	got, err := EvalExpr("1 + 2", ExprContext{})
	require.NoError(t, err)
	assert.Equal(t, 3, got)
}
