package ci

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// D1: the derived key is deterministic, sensitive to every input namespace, and
// collision-safe across namespaces — the properties that make declare-inputs
// caching correct (a wrong-but-stable key silently serves stale results).
func TestDeriveContentKey(t *testing.T) {
	files := map[string]string{"go.sum": "aaa", "main.go": "bbb"}
	outs := map[string]string{"build.version": "1.2.3"}
	env := map[string]string{"GOFLAGS": "-mod=readonly"}

	base := DeriveContentKey(files, outs, env)
	assert.True(t, len(base) > len("content-"), "key has a hash body")
	assert.Contains(t, base, "content-")

	t.Run("deterministic — map order independent", func(t *testing.T) {
		f2 := map[string]string{"main.go": "bbb", "go.sum": "aaa"} // reversed insert
		assert.Equal(t, base, DeriveContentKey(f2, outs, env))
	})

	t.Run("a changed file hash changes the key", func(t *testing.T) {
		f2 := map[string]string{"go.sum": "aaa", "main.go": "CHANGED"}
		assert.NotEqual(t, base, DeriveContentKey(f2, outs, env))
	})

	t.Run("a changed upstream output changes the key", func(t *testing.T) {
		o2 := map[string]string{"build.version": "9.9.9"}
		assert.NotEqual(t, base, DeriveContentKey(files, o2, env))
	})

	t.Run("a changed env value changes the key", func(t *testing.T) {
		e2 := map[string]string{"GOFLAGS": ""}
		assert.NotEqual(t, base, DeriveContentKey(files, outs, e2))
	})

	t.Run("namespaces don't alias — same k/v in different namespaces differ", func(t *testing.T) {
		asFile := DeriveContentKey(map[string]string{"X": "1"}, nil, nil)
		asEnv := DeriveContentKey(nil, nil, map[string]string{"X": "1"})
		assert.NotEqual(t, asFile, asEnv)
	})

	t.Run("separator injection can't forge a collision", func(t *testing.T) {
		// {"a":"b","c":"d"} must not equal {"a":"b:c:d"} etc.
		k1 := DeriveContentKey(map[string]string{"a": "b", "c": "d"}, nil, nil)
		k2 := DeriveContentKey(map[string]string{"a:b:c": "d"}, nil, nil)
		assert.NotEqual(t, k1, k2)
	})

	t.Run("empty inputs yield a stable key", func(t *testing.T) {
		assert.Equal(t, DeriveContentKey(nil, nil, nil), DeriveContentKey(map[string]string{}, map[string]string{}, map[string]string{}))
	})
}

// D1: declared job inputs are validated — needs refs must be real, needed jobs;
// files/env must be well-formed.
func TestValidate_JobInputs(t *testing.T) {
	ok, err := Parse([]byte(`
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  build:
    steps: [{ run: go build ./... }]
    outputs: { version: "${{ steps.outputs.version }}" }
  test:
    needs: [build]
    inputs:
      files: ["go.sum", "**/*.go"]
      needs: ["build.version"]
      env: [GOFLAGS]
    steps: [{ run: go test ./... }]
`))
	require.NoError(t, err)
	assert.Empty(t, ok.ValidateDetailed().Errors(), "well-formed inputs validate clean")

	// inputs.needs referencing a job not in needs: is rejected with a fix.
	_, err = Parse([]byte(`
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  build: { steps: [{ run: echo }] }
  test:
    inputs: { needs: ["build.version"] }
    steps: [{ run: echo }]
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in needs")

	// bad env name rejected.
	_, err = Parse([]byte(`
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  test:
    inputs: { env: ["not a name"] }
    steps: [{ run: echo }]
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "environment variable name")
}
