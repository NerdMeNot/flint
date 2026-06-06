package flinterr_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/NerdMeNot/flint/internal/core/flinterr"
)

func TestError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *flinterr.Error
		want string
	}{
		{
			name: "without cause",
			err:  flinterr.NewNotFound("project not found"),
			want: "flint: not_found: project not found",
		},
		{
			name: "with cause",
			err:  flinterr.WrapTransient("connection failed", fmt.Errorf("dial tcp: timeout")),
			want: "flint: transient: connection failed: dial tcp: timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestError_Unwrap(t *testing.T) {
	cause := fmt.Errorf("root cause")
	err := flinterr.WrapInternal("something broke", cause)

	if !errors.Is(err, cause) {
		t.Error("expected errors.Is to find the wrapped cause")
	}
}

func TestError_ErrorsAs(t *testing.T) {
	err := flinterr.NewConflict("duplicate key")
	wrapped := fmt.Errorf("handler: %w", err)

	var target *flinterr.Error
	if !errors.As(wrapped, &target) {
		t.Fatal("expected errors.As to find *flinterr.Error")
	}
	if target.Kind != flinterr.KindConflict {
		t.Errorf("Kind = %v, want KindConflict", target.Kind)
	}
}

func TestIsTransient(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"transient error", flinterr.NewTransient("retry me"), true},
		{"wrapped transient", fmt.Errorf("outer: %w", flinterr.NewTransient("inner")), true},
		{"not transient", flinterr.NewNotFound("gone"), false},
		{"plain error", fmt.Errorf("plain"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := flinterr.IsTransient(tt.err); got != tt.want {
				t.Errorf("IsTransient() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsNotFound(t *testing.T) {
	if !flinterr.IsNotFound(flinterr.NewNotFound("missing")) {
		t.Error("expected IsNotFound to return true")
	}
	if flinterr.IsNotFound(flinterr.NewTransient("nope")) {
		t.Error("expected IsNotFound to return false for transient")
	}
}

func TestIsKind(t *testing.T) {
	tests := []struct {
		name string
		err  error
		kind flinterr.ErrorKind
		want bool
	}{
		{"match", flinterr.NewForbidden("no"), flinterr.KindForbidden, true},
		{"no match", flinterr.NewForbidden("no"), flinterr.KindTransient, false},
		{"plain error", fmt.Errorf("plain"), flinterr.KindInternal, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := flinterr.IsKind(tt.err, tt.kind); got != tt.want {
				t.Errorf("IsKind() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestKind(t *testing.T) {
	if got := flinterr.Kind(flinterr.NewConflict("dup")); got != flinterr.KindConflict {
		t.Errorf("Kind() = %v, want KindConflict", got)
	}
	if got := flinterr.Kind(fmt.Errorf("plain")); got != flinterr.KindInternal {
		t.Errorf("Kind() = %v, want KindInternal for plain error", got)
	}
}

func TestErrorKind_String(t *testing.T) {
	tests := []struct {
		kind flinterr.ErrorKind
		want string
	}{
		{flinterr.KindTransient, "transient"},
		{flinterr.KindInvalidInput, "invalid_input"},
		{flinterr.KindNotFound, "not_found"},
		{flinterr.KindConflict, "conflict"},
		{flinterr.KindForbidden, "forbidden"},
		{flinterr.KindInternal, "internal"},
		{flinterr.ErrorKind(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.kind.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
