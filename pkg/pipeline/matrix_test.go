package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCartesianProduct_SingleDimension(t *testing.T) {
	combos := CartesianProduct(map[string][]string{
		"node": {"16", "18", "20"},
	})
	assert.Len(t, combos, 3)
	assert.Equal(t, "16", combos[0]["node"])
	assert.Equal(t, "18", combos[1]["node"])
	assert.Equal(t, "20", combos[2]["node"])
}

func TestCartesianProduct_TwoDimensions(t *testing.T) {
	combos := CartesianProduct(map[string][]string{
		"node": {"16", "18"},
		"os":   {"ubuntu", "alpine"},
	})
	assert.Len(t, combos, 4)

	// Keys sorted: node first, then os.
	// Values iterate in declaration order: node=16 then 18, os=ubuntu then alpine.
	assert.Equal(t, map[string]string{"node": "16", "os": "ubuntu"}, combos[0])
	assert.Equal(t, map[string]string{"node": "16", "os": "alpine"}, combos[1])
	assert.Equal(t, map[string]string{"node": "18", "os": "ubuntu"}, combos[2])
	assert.Equal(t, map[string]string{"node": "18", "os": "alpine"}, combos[3])
}

func TestCartesianProduct_Empty(t *testing.T) {
	assert.Nil(t, CartesianProduct(nil))
	assert.Nil(t, CartesianProduct(map[string][]string{}))
}

func TestMatrixKey(t *testing.T) {
	key := MatrixKey(map[string]string{"os": "ubuntu", "node": "16"})
	assert.Equal(t, "node=16,os=ubuntu", key) // sorted by key
}

func TestExpandMatrix_NoMatrix(t *testing.T) {
	p := &Pipeline{
		Steps: []Step{
			{Name: "build", Run: Cmd("make build")},
			{Name: "test", Run: Cmd("make test")},
		},
	}
	result := ExpandMatrix(p)
	assert.Len(t, result.Steps, 2)
	assert.Equal(t, "build", result.Steps[0].Name)
	assert.Equal(t, "test", result.Steps[1].Name)
}

func TestExpandMatrix_SingleStep(t *testing.T) {
	p := &Pipeline{
		Steps: []Step{
			{
				Name: "test",
				Run:  Cmd("npm test"),
				Matrix: map[string][]string{
					"node": {"16", "18"},
				},
			},
		},
	}
	result := ExpandMatrix(p)
	require.Len(t, result.Steps, 2)

	assert.Equal(t, "test[node=16]", result.Steps[0].Name)
	assert.Equal(t, "test[node=18]", result.Steps[1].Name)

	// Matrix field cleared on expanded steps.
	assert.Nil(t, result.Steps[0].Matrix)
	assert.Nil(t, result.Steps[1].Matrix)

	// Matrix values injected as env vars.
	assert.Equal(t, "16", result.Steps[0].Env["MATRIX_NODE"])
	assert.Equal(t, "18", result.Steps[1].Env["MATRIX_NODE"])
}

func TestExpandMatrix_Interpolation(t *testing.T) {
	p := &Pipeline{
		Steps: []Step{
			{
				Name:  "build",
				Run:   Cmd("echo ${{ matrix.node }}"),
				Image: "node:${{ matrix.node }}",
				Matrix: map[string][]string{
					"node": {"16", "20"},
				},
			},
		},
	}
	result := ExpandMatrix(p)
	require.Len(t, result.Steps, 2)

	assert.Equal(t, "echo 16", result.Steps[0].Run.String())
	assert.Equal(t, "node:16", result.Steps[0].Image)
	assert.Equal(t, "echo 20", result.Steps[1].Run.String())
	assert.Equal(t, "node:20", result.Steps[1].Image)
}

func TestExpandMatrix_DependsOnRewriting(t *testing.T) {
	p := &Pipeline{
		Steps: []Step{
			{
				Name: "test",
				Run:  Cmd("npm test"),
				Matrix: map[string][]string{
					"node": {"16", "18"},
				},
			},
			{
				Name:      "deploy",
				Run:       Cmd("deploy.sh"),
				DependsOn: []string{"test"},
			},
		},
	}
	result := ExpandMatrix(p)
	require.Len(t, result.Steps, 3) // 2 test variants + 1 deploy

	// Deploy should depend on both test variants.
	deploy := result.Steps[2]
	assert.Equal(t, "deploy", deploy.Name)
	assert.ElementsMatch(t, []string{"test[node=16]", "test[node=18]"}, deploy.DependsOn)
}

func TestExpandMatrix_PreservesExistingEnv(t *testing.T) {
	p := &Pipeline{
		Steps: []Step{
			{
				Name: "test",
				Run:  Cmd("npm test"),
				Env:  map[string]string{"CI": "true"},
				Matrix: map[string][]string{
					"node": {"16"},
				},
			},
		},
	}
	result := ExpandMatrix(p)
	require.Len(t, result.Steps, 1)

	// Both original env and matrix env should be present.
	assert.Equal(t, "true", result.Steps[0].Env["CI"])
	assert.Equal(t, "16", result.Steps[0].Env["MATRIX_NODE"])
}

func TestExpandMatrix_NilPipeline(t *testing.T) {
	assert.Nil(t, ExpandMatrix(nil))
}

func TestExpandMatrix_OriginalUnmodified(t *testing.T) {
	p := &Pipeline{
		Steps: []Step{
			{
				Name: "test",
				Run:  Cmd("npm test"),
				Matrix: map[string][]string{
					"node": {"16", "18"},
				},
			},
		},
	}
	_ = ExpandMatrix(p)
	// Original should still have matrix.
	assert.Len(t, p.Steps, 1)
	assert.NotNil(t, p.Steps[0].Matrix)
	assert.Equal(t, "test", p.Steps[0].Name)
}
