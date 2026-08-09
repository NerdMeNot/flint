package observe_test

import (
	"context"
	"testing"

	"github.com/NerdMeNot/flint/internal/core/observe"
)

func TestInit_NoEndpoint(t *testing.T) {
	ctx := context.Background()

	shutdown, err := observe.Init(ctx, observe.Config{
		ServiceName:    "flint-test",
		ServiceVersion: "test",
		LogLevel:       "debug",
	})
	if err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	t.Cleanup(func() {
		if err := shutdown(ctx); err != nil {
			t.Errorf("shutdown() error: %v", err)
		}
	})
}

func TestContext_RequestID(t *testing.T) {
	ctx := context.Background()

	// Auto-generate if empty.
	ctx = observe.WithRequestID(ctx, "")
	if id := observe.RequestID(ctx); id == "" {
		t.Error("WithRequestID('') should auto-generate an ID")
	}

	// Use provided value.
	ctx = observe.WithRequestID(ctx, "req-123")
	if id := observe.RequestID(ctx); id != "req-123" {
		t.Errorf("RequestID() = %q, want %q", id, "req-123")
	}
}

func TestContext_AllKeys(t *testing.T) {
	ctx := context.Background()
	ctx = observe.WithRequestID(ctx, "req-1")
	ctx = observe.WithOrgID(ctx, "org-1")
	ctx = observe.WithRunID(ctx, "run-1")
	ctx = observe.WithUserID(ctx, "user-1")
	ctx = observe.WithStepName(ctx, "test")

	if observe.RequestID(ctx) != "req-1" {
		t.Error("RequestID mismatch")
	}
	if observe.OrgID(ctx) != "org-1" {
		t.Error("OrgID mismatch")
	}
	if observe.RunID(ctx) != "run-1" {
		t.Error("RunID mismatch")
	}
}

func TestContext_EmptyContext(t *testing.T) {
	ctx := context.Background()

	if observe.RequestID(ctx) != "" {
		t.Error("RequestID should be empty on bare context")
	}
	if observe.OrgID(ctx) != "" {
		t.Error("OrgID should be empty on bare context")
	}
}

func TestLogger_WithContext(t *testing.T) {
	ctx := context.Background()
	ctx = observe.WithRequestID(ctx, "req-123")
	ctx = observe.WithOrgID(ctx, "org-abc")

	l := observe.Logger(ctx)
	// Logger should not panic — just verify it returns a usable logger.
	l.Info().Msg("test log from observe_test")
}
