package pipeline_test

import (
	"errors"
	"testing"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

func TestEvalExpr_Basic(t *testing.T) {
	tests := []struct {
		name string
		expr string
		ctx  pipeline.ExprContext
		want any
	}{
		{
			name: "simple string",
			expr: "name",
			ctx:  pipeline.ExprContext{"name": "flint"},
			want: "flint",
		},
		{
			name: "boolean expression",
			expr: "x > 5",
			ctx:  pipeline.ExprContext{"x": 10},
			want: true,
		},
		{
			name: "string equality",
			expr: `branch == "main"`,
			ctx:  pipeline.ExprContext{"branch": "main"},
			want: true,
		},
		{
			name: "template wrapped",
			expr: "${{ x + y }}",
			ctx:  pipeline.ExprContext{"x": 3, "y": 4},
			want: 7,
		},
		{
			name: "nested access",
			expr: `git.branch == "main"`,
			ctx: pipeline.ExprContext{
				"git": map[string]any{"branch": "main", "sha": "abc123"},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := pipeline.EvalExpr(tt.expr, tt.ctx)
			if err != nil {
				t.Fatalf("EvalExpr() error: %v", err)
			}
			if result != tt.want {
				t.Errorf("EvalExpr() = %v (%T), want %v (%T)", result, result, tt.want, tt.want)
			}
		})
	}
}

func TestEvalExpr_Errors(t *testing.T) {
	tests := []struct {
		name string
		expr string
		ctx  pipeline.ExprContext
	}{
		{"empty expression", "${{ }}", nil},
		{"undefined variable", "missing_var", pipeline.ExprContext{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pipeline.EvalExpr(tt.expr, tt.ctx)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, pipeline.ErrInvalidExpr) {
				t.Errorf("expected ErrInvalidExpr, got: %v", err)
			}
		})
	}
}

func TestEvalCondition(t *testing.T) {
	tests := []struct {
		name string
		expr string
		ctx  pipeline.ExprContext
		want bool
	}{
		{
			name: "true condition",
			expr: `${{ branch == "main" }}`,
			ctx:  pipeline.ExprContext{"branch": "main"},
			want: true,
		},
		{
			name: "false condition",
			expr: `${{ branch == "main" }}`,
			ctx:  pipeline.ExprContext{"branch": "feature/foo"},
			want: false,
		},
		{
			name: "boolean literal",
			expr: "${{ skip == false }}",
			ctx:  pipeline.ExprContext{"skip": false},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := pipeline.EvalCondition(tt.expr, tt.ctx)
			if err != nil {
				t.Fatalf("EvalCondition() error: %v", err)
			}
			if result != tt.want {
				t.Errorf("EvalCondition() = %v, want %v", result, tt.want)
			}
		})
	}
}

func TestEvalCondition_NonBoolError(t *testing.T) {
	_, err := pipeline.EvalCondition("name", pipeline.ExprContext{"name": "flint"})
	if err == nil {
		t.Fatal("expected error for non-bool result")
	}
}

func TestInterpolate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		ctx   pipeline.ExprContext
		want  string
	}{
		{
			name:  "single substitution",
			input: "image: golang:${{ version }}",
			ctx:   pipeline.ExprContext{"version": "1.23"},
			want:  "image: golang:1.23",
		},
		{
			name:  "multiple substitutions",
			input: "${{ org }}/${{ repo }}:${{ tag }}",
			ctx:   pipeline.ExprContext{"org": "acme", "repo": "api", "tag": "v1"},
			want:  "acme/api:v1",
		},
		{
			name:  "no templates",
			input: "plain string",
			ctx:   pipeline.ExprContext{},
			want:  "plain string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := pipeline.Interpolate(tt.input, tt.ctx)
			if err != nil {
				t.Fatalf("Interpolate() error: %v", err)
			}
			if result != tt.want {
				t.Errorf("Interpolate() = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestInterpolate_Error(t *testing.T) {
	_, err := pipeline.Interpolate("${{ missing }}", pipeline.ExprContext{})
	if err == nil {
		t.Fatal("expected error for undefined variable")
	}
}
