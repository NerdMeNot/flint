package ci

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func childNames(m map[string]Job) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// D3: fan-out materializes one child per array element, each carrying its shard
// in the environment.
func TestExpandFanOut(t *testing.T) {
	job := Job{Env: map[string]string{"CI": "true"}}

	t.Run("string elements pass through verbatim", func(t *testing.T) {
		children := ExpandFanOut("shard", job, []any{"a", "b", "c"})
		assert.Equal(t, []string{"shard[0]", "shard[1]", "shard[2]"}, childNames(children))
		assert.Equal(t, "a", children["shard[0]"].Env["FLINT_FANOUT_ITEM"])
		assert.Equal(t, "c", children["shard[2]"].Env["FLINT_FANOUT_ITEM"])
		assert.Equal(t, "2", children["shard[2]"].Env["FLINT_FANOUT_INDEX"])
		assert.Equal(t, "true", children["shard[0]"].Env["CI"], "existing env is preserved")
		assert.Empty(t, children["shard[0]"].FanOut, "children have fanOut cleared")
	})

	t.Run("object elements become compact JSON", func(t *testing.T) {
		children := ExpandFanOut("m", job, []any{map[string]any{"os": "linux", "n": 2}})
		item := children["m[0]"].Env["FLINT_FANOUT_ITEM"]
		// map key order in JSON is deterministic (sorted) in encoding/json.
		assert.Equal(t, `{"n":2,"os":"linux"}`, item)
	})

	t.Run("empty array yields zero children", func(t *testing.T) {
		assert.Empty(t, ExpandFanOut("shard", job, nil))
		assert.Empty(t, ExpandFanOut("shard", job, []any{}))
	})

	t.Run("does not mutate the template's env", func(t *testing.T) {
		_ = ExpandFanOut("shard", job, []any{"a"})
		_, leaked := job.Env["FLINT_FANOUT_ITEM"]
		assert.False(t, leaked, "template env must be untouched")
	})
}

// D3: fanOut is validated — a valid expression passes; matrix + fanOut together
// is rejected.
func TestValidate_FanOut(t *testing.T) {
	ok, err := Parse([]byte(`
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  plan:
    steps: [{ run: ./plan.sh }]
    outputs: { shards: "${{ steps.outputs.shards }}" }
  run-shard:
    needs: [plan]
    fanOut: "${{ fromJSON(needs.plan.outputs.shards) }}"
    steps: [{ run: ./run.sh "$FLINT_FANOUT_ITEM" }]
`))
	require.NoError(t, err)
	assert.Empty(t, ok.ValidateDetailed().Errors(), "a fanOut over an upstream output validates clean")

	_, err = Parse([]byte(`
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  bad:
    matrix: { go: ["1.26"] }
    fanOut: "${{ fromJSON('[]') }}"
    steps: [{ run: echo }]
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "both matrix and fanOut")
}
