package ci

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The examples under docs/design/examples/runnable/ are the ones we promise
// compile on the current engine (no external module registry, no roadmap-only
// features). This test is the enforcement: every file there must Parse and
// Compile clean, so a doc example can never silently rot. `go test` runs with
// the package dir as CWD, so the path is relative to internal/products/ci.
//
// The aspirational examples one level up (payments-api, orders-api, release)
// reference a versioned module registry and features still on the DSL roadmap
// (action modules, matrix failFast); they are design sketches, not runnable yet,
// and deliberately excluded until those land.
func TestRunnableExamplesCompile(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "docs", "design", "examples", "runnable")
	matches, err := filepath.Glob(filepath.Join(dir, "*.ci.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, matches, "expected runnable example pipelines under %s", dir)

	for _, path := range matches {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			require.NoError(t, err)

			p, err := Parse(raw)
			require.NoError(t, err, "example must parse+validate clean")

			// Compile against every declared environment (plus the plain run) so
			// env-scoped jobs are exercised too.
			envs := append([]string{""}, p.Environments...)
			for _, env := range envs {
				_, err := Compile(p, env)
				assert.NoErrorf(t, err, "example must compile for environment %q", env)
			}
		})
	}
}
