package engine_test

import (
	"testing"

	"github.com/NerdMeNot/flint/internal/engine"
)

func TestTaskToken_RoundTrip(t *testing.T) {
	original := engine.TaskToken{
		WorkflowID: "wf-abc-123",
		StepName:   "test",
		Attempt:    0,
	}

	encoded := engine.EncodeTaskToken(original)
	if encoded == "" {
		t.Fatal("EncodeTaskToken returned empty string")
	}

	decoded, err := engine.DecodeTaskToken(encoded)
	if err != nil {
		t.Fatalf("DecodeTaskToken error: %v", err)
	}

	if decoded.WorkflowID != original.WorkflowID {
		t.Errorf("WorkflowID = %q, want %q", decoded.WorkflowID, original.WorkflowID)
	}
	if decoded.StepName != original.StepName {
		t.Errorf("StepName = %q, want %q", decoded.StepName, original.StepName)
	}
	if decoded.Attempt != original.Attempt {
		t.Errorf("Attempt = %d, want %d", decoded.Attempt, original.Attempt)
	}
}

func TestTaskToken_InvalidEncoding(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"not base64", "!!!invalid!!!"},
		{"valid base64 but not json", "aGVsbG8="},
		{"missing fields", "e30="}, // base64 of "{}"
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := engine.DecodeTaskToken(tt.token)
			if err == nil {
				t.Fatal("expected error for invalid token")
			}
		})
	}
}

func TestTaskToken_RetryAttempt(t *testing.T) {
	t0 := engine.TaskToken{WorkflowID: "wf-1", StepName: "build", Attempt: 0}
	t1 := engine.TaskToken{WorkflowID: "wf-1", StepName: "build", Attempt: 1}
	t2 := engine.TaskToken{WorkflowID: "wf-1", StepName: "build", Attempt: 2}

	e0 := engine.EncodeTaskToken(t0)
	e1 := engine.EncodeTaskToken(t1)
	e2 := engine.EncodeTaskToken(t2)

	// All different tokens for same step.
	if e0 == e1 || e1 == e2 {
		t.Error("different attempts should produce different tokens")
	}

	// All decode correctly.
	for _, enc := range []string{e0, e1, e2} {
		_, err := engine.DecodeTaskToken(enc)
		if err != nil {
			t.Errorf("DecodeTaskToken(%q) error: %v", enc, err)
		}
	}
}
