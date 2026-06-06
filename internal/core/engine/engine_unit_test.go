package engine

import (
	"testing"

	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/stretchr/testify/assert"
)

// ─────────────────────────────────────────────────────────────
// stepShouldRun — already tested in advance_test.go but let's
// add edge cases
// ─────────────────────────────────────────────────────────────

func TestStepShouldRun_UnknownWhenValue(t *testing.T) {
	// Unknown "when" value is treated as default (onSuccess).
	assert.True(t, stepShouldRun("unknownValue", false))
	assert.False(t, stepShouldRun("unknownValue", true))
}

// ─────────────────────────────────────────────────────────────
// isPipelineFailed
// ─────────────────────────────────────────────────────────────

func TestIsPipelineFailed_EmptyWaves(t *testing.T) {
	assert.False(t, isPipelineFailed(nil, nil))
	assert.False(t, isPipelineFailed([][]string{}, map[string]stepRow{}))
}

func TestIsPipelineFailed_MissingStepInMap(t *testing.T) {
	waves := [][]string{{"a", "b"}}
	steps := map[string]stepRow{
		"a": {status: "succeeded"},
		// "b" is missing from the map — should not panic
	}
	assert.False(t, isPipelineFailed(waves, steps))
}

// ─────────────────────────────────────────────────────────────
// TaskToken encode/decode
// ─────────────────────────────────────────────────────────────

func TestTaskToken_RoundTrip(t *testing.T) {
	original := TaskToken{
		WorkflowID: "wf-123",
		StepName:   "build",
		Attempt:    2,
	}

	encoded := EncodeTaskToken(original)
	assert.NotEmpty(t, encoded)

	decoded, err := DecodeTaskToken(encoded)
	assert.NoError(t, err)
	assert.Equal(t, original.WorkflowID, decoded.WorkflowID)
	assert.Equal(t, original.StepName, decoded.StepName)
	assert.Equal(t, original.Attempt, decoded.Attempt)
}

func TestTaskToken_InvalidBase64(t *testing.T) {
	_, err := DecodeTaskToken("not-valid-base64!!!")
	assert.Error(t, err)
}

func TestTaskToken_InvalidJSON(t *testing.T) {
	// Valid base64 but not valid JSON
	_, err := DecodeTaskToken("bm90LWpzb24=") // "not-json"
	assert.Error(t, err)
}

// ─────────────────────────────────────────────────────────────
// isTerminal
// ─────────────────────────────────────────────────────────────

func TestIsTerminal(t *testing.T) {
	assert.True(t, isTerminal("succeeded"))
	assert.True(t, isTerminal("failed"))
	assert.True(t, isTerminal("skipped"))
	assert.True(t, isTerminal("cancelled"))
	assert.False(t, isTerminal("running"))
	assert.False(t, isTerminal("pending"))
	assert.False(t, isTerminal("queued"))
	assert.False(t, isTerminal("waiting"))
	assert.False(t, isTerminal(""))
}

// ─────────────────────────────────────────────────────────────
// backoffDuration
// ─────────────────────────────────────────────────────────────

func TestBackoffDuration_Exponential(t *testing.T) {
	d0 := backoffDuration(0, "exponential", 5)
	d1 := backoffDuration(1, "exponential", 5)
	d2 := backoffDuration(2, "exponential", 5)

	// With jitter (0.8-1.2x), base values are 5s, 10s, 20s.
	assert.InDelta(t, 5.0, d0.Seconds(), 2.0, "attempt 0: ~5s")
	assert.InDelta(t, 10.0, d1.Seconds(), 3.0, "attempt 1: ~10s")
	assert.InDelta(t, 20.0, d2.Seconds(), 5.0, "attempt 2: ~20s")
}

func TestBackoffDuration_Linear(t *testing.T) {
	d0 := backoffDuration(0, "linear", 5)
	d1 := backoffDuration(1, "linear", 5)

	assert.InDelta(t, 5.0, d0.Seconds(), 2.0, "attempt 0: ~5s")
	assert.InDelta(t, 10.0, d1.Seconds(), 3.0, "attempt 1: ~10s")
}

func TestBackoffDuration_Fixed(t *testing.T) {
	d0 := backoffDuration(0, "fixed", 5)
	d1 := backoffDuration(1, "fixed", 5)

	// Unknown policy = fixed base.
	assert.InDelta(t, 5.0, d0.Seconds(), 2.0)
	assert.InDelta(t, 5.0, d1.Seconds(), 2.0)
}

// ─────────────────────────────────────────────────────────────
// waveComplete / allWavesComplete
// ─────────────────────────────────────────────────────────────

func TestWaveComplete(t *testing.T) {
	steps := map[string]stepRow{
		"a": {status: "succeeded"},
		"b": {status: "failed"},
		"c": {status: "running"},
	}

	assert.True(t, waveComplete([]string{"a", "b"}, steps), "both terminal")
	assert.False(t, waveComplete([]string{"a", "c"}, steps), "c is running")
	assert.True(t, waveComplete([]string{}, steps), "empty wave is complete")
}

func TestAllWavesComplete(t *testing.T) {
	steps := map[string]stepRow{
		"a": {status: "succeeded"},
		"b": {status: "succeeded"},
	}

	waves := [][]string{{"a"}, {"b"}}
	assert.True(t, allWavesComplete(waves, steps))

	steps["b"] = stepRow{status: "running"}
	assert.False(t, allWavesComplete(waves, steps))
}

// ─────────────────────────────────────────────────────────────
// sanitizeK8sName
// ─────────────────────────────────────────────────────────────

func TestSanitizeK8sName(t *testing.T) {
	tests := []struct {
		input string
		check func(t *testing.T, result string)
	}{
		{
			input: "simple",
			check: func(t *testing.T, result string) {
				assert.Contains(t, result, "simple")
			},
		},
		{
			input: "My.Build_Step",
			check: func(t *testing.T, result string) {
				// All lowercase, no dots or underscores.
				for _, c := range result {
					assert.True(t, (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-',
						"unexpected char: %c", c)
				}
			},
		},
		{
			input: "my.build",
			check: func(t *testing.T, result string) {
				// Two different names that would collide without hash.
				other := sanitizeK8sName("my-build")
				assert.NotEqual(t, result, other,
					"different inputs should produce different names (hash suffix)")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := sanitizeK8sName(tc.input)
			assert.LessOrEqual(t, len(result), 63, "K8s name must be ≤63 chars")
			tc.check(t, result)
		})
	}
}

// ─────────────────────────────────────────────────────────────
// extractMatrixKey
// ─────────────────────────────────────────────────────────────

func TestExtractMatrixKey(t *testing.T) {
	assert.Equal(t, "node=16,os=ubuntu", extractMatrixKey("test[node=16,os=ubuntu]"))
	assert.Equal(t, "", extractMatrixKey("test"))
	assert.Equal(t, "", extractMatrixKey("test["))
	assert.Equal(t, "a", extractMatrixKey("[a]"))
}

// ─────────────────────────────────────────────────────────────
// resolveServiceAccount
// ─────────────────────────────────────────────────────────────

func TestResolveServiceAccount(t *testing.T) {
	assert.Equal(t, "step-sa", resolveServiceAccount("step-sa", "pipeline-sa"))
	assert.Equal(t, "pipeline-sa", resolveServiceAccount("", "pipeline-sa"))
	assert.Equal(t, "", resolveServiceAccount("", ""))
}

// ─────────────────────────────────────────────────────────────
// wavesToNames
// ─────────────────────────────────────────────────────────────

func TestWavesToNames(t *testing.T) {
	waves := [][]pipeline.Step{
		{{Name: "a"}, {Name: "b"}},
		{{Name: "c"}},
	}
	names := wavesToNames(waves)
	assert.Equal(t, [][]string{{"a", "b"}, {"c"}}, names)
}
