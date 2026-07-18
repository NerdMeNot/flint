package ci

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// variantSuffixes returns the sorted matrix-variant suffixes of a compiled job
// (the part after "<job>::"), so a test can assert the exact combination set.
func variantSuffixes(waves [][]pipeline.Step, job string) []string {
	var out []string
	prefix := job + matrixSep
	for _, w := range waves {
		for _, s := range w {
			if len(s.Name) > len(prefix) && s.Name[:len(prefix)] == prefix {
				out = append(out, s.Name[len(prefix):])
			}
		}
	}
	sort.Strings(out)
	return out
}

// C3: exclude removes matching combinations from the cartesian product.
func TestMatrix_Exclude(t *testing.T) {
	p, err := Parse([]byte(`
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  test:
    matrix:
      go: ["1.25", "1.26"]
      os: [linux, mac]
      exclude:
        - { os: mac, go: "1.25" }
    steps: [{ run: go test ./... }]
`))
	require.NoError(t, err)
	waves, err := Compile(p, "")
	require.NoError(t, err)
	// 4 combos minus the excluded (mac,1.25) = 3. Suffix order is by dimension
	// key (go, os) → values joined.
	assert.Equal(t, []string{"1.25-linux", "1.26-linux", "1.26-mac"}, variantSuffixes(waves, "test"))
}

// C3: an include entry that names new dimension values adds a brand-new
// combination; one that only adds extra keys extends matching combinations.
func TestMatrix_Include(t *testing.T) {
	p, err := Parse([]byte(`
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  test:
    matrix:
      go: ["1.25", "1.26"]
      include:
        - { go: "1.27", experimental: "true" }
        - { go: "1.26", coverage: "on" }
    steps: [{ run: go test ./... }]
`))
	require.NoError(t, err)
	waves, err := Compile(p, "")
	require.NoError(t, err)
	// Base {1.25, 1.26}; include adds the new value 1.27; the {go:1.26,...} entry
	// extends the existing 1.26 combo (no new combo). Extra include keys join the
	// variant name (values sorted by key), GitHub-style: 1.26+coverage=on →
	// "on-1.26", 1.27+experimental=true → "true-1.27".
	assert.Equal(t, []string{"1.25", "on-1.26", "true-1.27"}, variantSuffixes(waves, "test"))
}

// C3: an include key introduced only via include is referenceable in the job.
func TestMatrix_IncludeExtraKeyReferenceable(t *testing.T) {
	p, err := Parse([]byte(`
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  test:
    matrix:
      go: ["1.26"]
      include:
        - { go: "1.26", flags: "-race" }
    env:
      GO_FLAGS: "${{ matrix.flags }}"
    steps:
      - run: "go test ${{ matrix.flags }} ./..."
`))
	require.NoError(t, err, "matrix.flags (include-only key) must validate")
	waves, err := Compile(p, "")
	require.NoError(t, err)
	var variant pipeline.Step
	count := 0
	for _, w := range waves {
		for _, s := range w {
			if len(s.Name) > len("test::") && s.Name[:len("test::")] == "test::" {
				variant = s
				count++
			}
		}
	}
	require.Equal(t, 1, count, "the single extended variant compiles")
	assert.Equal(t, "-race", variant.Env["GO_FLAGS"], "include-only key interpolates in env")
	assert.Equal(t, "go test -race ./...", variant.Steps[0].Run.Commands[0], "include-only key interpolates in run")
}

// C3: exclude on a non-dimension key is a typo, not a silent no-op.
func TestMatrix_ExcludeUnknownDimensionRejected(t *testing.T) {
	_, err := Parse([]byte(`
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  test:
    matrix:
      go: ["1.25", "1.26"]
      exclude:
        - { platform: mac }
    steps: [{ run: go test ./... }]
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exclude references unknown matrix dimension")
}

// C3: failFast: false is accepted (it is the current all-variants-run behavior);
// failFast: true is still rejected as unenforced.
func TestMatrix_FailFast(t *testing.T) {
	ok, err := Parse([]byte(`
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  test:
    matrix: { go: ["1.25", "1.26"] }
    failFast: false
    steps: [{ run: go test ./... }]
`))
	require.NoError(t, err)
	assert.Empty(t, ok.ValidateDetailed().Errors(), "failFast: false is accepted")

	_, err = Parse([]byte(`
image: alpine
triggers: { push: { branches: [main] } }
jobs:
  test:
    matrix: { go: ["1.25", "1.26"] }
    failFast: true
    steps: [{ run: go test ./... }]
`))
	require.Error(t, err, "failFast: true is not yet enforced")
	assert.Contains(t, err.Error(), "failFast")
}
