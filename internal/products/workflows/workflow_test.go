package workflows

import (
	"testing"

	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_Valid(t *testing.T) {
	def, err := Parse([]byte(`
name: nightly
steps:
  - name: a
    run: echo a
  - name: b
    run: echo b
    dependsOn: [a]
`))
	require.NoError(t, err)
	assert.Equal(t, "nightly", def.Name)
	require.Len(t, def.Steps, 2)
	assert.Equal(t, "a", def.Steps[0].Name)
}

func TestParse_Invalid(t *testing.T) {
	cases := []struct {
		name, yaml, errContains string
	}{
		{"no name", "steps:\n  - name: a\n    run: x", "name is required"},
		{"no steps", "name: x", "at least one step"},
		{"empty step name", "name: x\nsteps:\n  - run: y", "every step needs a name"},
		{"duplicate", "name: x\nsteps:\n  - name: a\n    run: x\n  - name: a\n    run: y", "duplicate step name"},
		{"unknown dep", "name: x\nsteps:\n  - name: a\n    run: x\n    dependsOn: [z]", "unknown step"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestResolve_Waves(t *testing.T) {
	def, err := Parse([]byte(`
name: fan-out
steps:
  - name: a
    run: echo a
  - name: b
    run: echo b
    dependsOn: [a]
  - name: c
    run: echo c
    dependsOn: [a]
`))
	require.NoError(t, err)

	waves, err := def.Resolve()
	require.NoError(t, err)
	require.Len(t, waves, 2, "a in wave 0; b and c in wave 1")
	assert.Equal(t, "a", waves[0][0].Name)

	names := make([]string, 0, len(waves[1]))
	for _, s := range waves[1] {
		names = append(names, s.Name)
	}
	assert.ElementsMatch(t, []string{"b", "c"}, names)
}

func TestResolve_Cycle(t *testing.T) {
	// A dependency cycle (valid names, so Validate passes) must be rejected by
	// the shared DAG resolver.
	def := &Definition{
		Name: "cyclic",
		Steps: []pipeline.Step{
			{Name: "a", Run: pipeline.Cmd("echo a"), DependsOn: []string{"b"}},
			{Name: "b", Run: pipeline.Cmd("echo b"), DependsOn: []string{"a"}},
		},
	}
	_, err := def.Resolve()
	require.Error(t, err)
}
