package pipeline_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

func TestEvalExpr_Basic(t *testing.T) {
	tests := []struct {
		name string
		expr string
		ctx  pipeline.ExprContext
		want any
	}{
		{"simple string", "name", pipeline.ExprContext{"name": "flint"}, "flint"},
		{"boolean expression", "x > 5", pipeline.ExprContext{"x": 10}, true},
		{"string equality", `branch == "main"`, pipeline.ExprContext{"branch": "main"}, true},
		{"template wrapped", "${{ x + y }}", pipeline.ExprContext{"x": 3, "y": 4}, 7},
		{"nested access", `project.name == "api"`, pipeline.ExprContext{"project": map[string]any{"name": "api"}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := pipeline.EvalExpr(tt.expr, tt.ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
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
			require.Error(t, err)
			assert.True(t, errors.Is(err, pipeline.ErrInvalidExpr))
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
		{"true", `${{ branch == "main" }}`, pipeline.ExprContext{"branch": "main"}, true},
		{"false", `${{ branch == "main" }}`, pipeline.ExprContext{"branch": "feat"}, false},
		{"boolean literal", "${{ skip == false }}", pipeline.ExprContext{"skip": false}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := pipeline.EvalCondition(tt.expr, tt.ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestInterpolate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		ctx   pipeline.ExprContext
		want  string
	}{
		{"single", "image: golang:${{ version }}", pipeline.ExprContext{"version": "1.23"}, "image: golang:1.23"},
		{"multiple", "${{ org }}/${{ repo }}:${{ tag }}", pipeline.ExprContext{"org": "acme", "repo": "api", "tag": "v1"}, "acme/api:v1"},
		{"no templates", "plain string", pipeline.ExprContext{}, "plain string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := pipeline.Interpolate(tt.input, tt.ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestBuildRuntimeContext(t *testing.T) {
	ctx := pipeline.BuildRuntimeContext(pipeline.RuntimeContextOpts{
		Branch:      "main",
		CommitSha:   "abc1234567890",
		Environment: "staging",
		ProjectName: "api",
		ProjectRepo: "acme/api",
		RunID:       "r-123",
		TriggerType: "push",
		TriggeredBy: "alice",
		Status:      "running",
	})

	assert.Equal(t, "main", ctx["branch"])
	assert.Equal(t, "abc1234", ctx["shortSha"])
	assert.Equal(t, "staging", ctx["environment"])

	project := ctx["project"].(map[string]any)
	assert.Equal(t, "api", project["name"])
	assert.Equal(t, "acme/api", project["repo"])

	// hashFiles should return deterministic placeholder when no FileHasher is set.
	hashFn := ctx["hashFiles"].(func(string) string)
	assert.Equal(t, "placeholder:go.sum", hashFn("go.sum"))
	assert.NotEqual(t, hashFn("go.sum"), hashFn("package-lock.json")) // different patterns → different hashes
}
