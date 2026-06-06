package engine_test

import (
	"testing"

	"github.com/NerdMeNot/flint/internal/core/engine"
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

func TestTaskToken_SignedRoundTrip(t *testing.T) {
	key := []byte("test-signing-key-at-least-32-chars!!")
	tok := engine.TaskToken{WorkflowID: "wf1", StepName: "build", Attempt: 1}

	enc := engine.EncodeTaskToken(tok, key)
	got, err := engine.DecodeTaskToken(enc, key)
	if err != nil {
		t.Fatalf("DecodeTaskToken(signed) error: %v", err)
	}
	if got != tok {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, tok)
	}
}

func TestTaskToken_RejectsForgedAndTampered(t *testing.T) {
	key := []byte("the-real-signing-key-32-chars-min!!!")
	tok := engine.TaskToken{WorkflowID: "wf1", StepName: "build", Attempt: 1}

	// 1. A token signed with a DIFFERENT key must be rejected (forgery).
	forged := engine.EncodeTaskToken(tok, []byte("attacker-key-also-32-characters-x!!!"))
	if _, err := engine.DecodeTaskToken(forged, key); err == nil {
		t.Error("expected forged token (wrong key) to be rejected")
	}

	// 2. An UNSIGNED token must be rejected when a key is required.
	unsigned := engine.EncodeTaskToken(tok) // no key
	if _, err := engine.DecodeTaskToken(unsigned, key); err == nil {
		t.Error("expected unsigned token to be rejected when key is set")
	}

	// 3. A tampered payload must be rejected.
	valid := engine.EncodeTaskToken(tok, key)
	tampered := "x" + valid // corrupt the payload prefix
	if _, err := engine.DecodeTaskToken(tampered, key); err == nil {
		t.Error("expected tampered token to be rejected")
	}
}
