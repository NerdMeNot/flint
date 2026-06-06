package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestStepShouldRun validates the `when:` field semantics used by advanceWorkflow.
func TestStepShouldRun(t *testing.T) {
	tests := []struct {
		when           string
		pipelineFailed bool
		want           bool
	}{
		// Default / onSuccess
		{"", false, true},
		{"", true, false},
		{"onSuccess", false, true},
		{"onSuccess", true, false},
		// onFailure
		{"onFailure", false, false},
		{"onFailure", true, true},
		// always
		{"always", false, true},
		{"always", true, true},
	}

	for _, tc := range tests {
		got := stepShouldRun(tc.when, tc.pipelineFailed)
		assert.Equal(t, tc.want, got,
			"stepShouldRun(%q, pipelineFailed=%v)", tc.when, tc.pipelineFailed)
	}
}

// TestIsPipelineFailed validates failure detection across DAG waves.
func TestIsPipelineFailed(t *testing.T) {
	waves := [][]string{{"a", "b"}, {"c"}}

	t.Run("no failures", func(t *testing.T) {
		steps := map[string]stepRow{
			"a": {status: "succeeded"},
			"b": {status: "succeeded"},
			"c": {status: "pending"},
		}
		assert.False(t, isPipelineFailed(waves, steps))
	})

	t.Run("failed with continue", func(t *testing.T) {
		steps := map[string]stepRow{
			"a": {status: "failed", onFailure: "continue"},
			"b": {status: "succeeded"},
			"c": {status: "pending"},
		}
		assert.False(t, isPipelineFailed(waves, steps), "continueOnError failure should not mark pipeline failed")
	})

	t.Run("failed without continue", func(t *testing.T) {
		steps := map[string]stepRow{
			"a": {status: "failed", onFailure: "fail"},
			"b": {status: "succeeded"},
			"c": {status: "pending"},
		}
		assert.True(t, isPipelineFailed(waves, steps))
	})

	t.Run("skipped steps do not count", func(t *testing.T) {
		steps := map[string]stepRow{
			"a": {status: "skipped"},
			"b": {status: "succeeded"},
			"c": {status: "succeeded"},
		}
		assert.False(t, isPipelineFailed(waves, steps))
	})
}
