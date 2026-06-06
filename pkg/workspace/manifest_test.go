package workspace

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeManifest(t *testing.T) {
	dir := t.TempDir()

	// Create some test files.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pkg/lib.go"), []byte("package pkg"), 0o644))

	// Create excluded files.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git/HEAD"), []byte("ref: refs/heads/main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".flint-exit"), []byte("0"), 0o644))

	m, err := ComputeManifest(dir)
	require.NoError(t, err)

	// Should include source files.
	assert.Contains(t, m, "main.go")
	assert.Contains(t, m, "pkg/lib.go")

	// Should exclude .git and .flint-exit.
	assert.NotContains(t, m, ".git/HEAD")
	assert.NotContains(t, m, ".flint-exit")

	// Hashes should be non-empty hex strings.
	assert.NotEmpty(t, m["main.go"].Hash)
	assert.Greater(t, m["main.go"].Size, int64(0))
}

func TestDiff(t *testing.T) {
	local := Manifest{
		"a.go":   {Hash: "aaa", Size: 10},
		"b.go":   {Hash: "bbb", Size: 20},
		"new.go": {Hash: "nnn", Size: 5},
	}
	remote := Manifest{
		"a.go":       {Hash: "aaa", Size: 10},     // same
		"b.go":       {Hash: "old-bbb", Size: 20}, // changed
		"deleted.go": {Hash: "ddd", Size: 15},     // deleted locally
	}

	upload, download, remove := Diff(local, remote)

	sort.Strings(upload)
	sort.Strings(download)
	sort.Strings(remove)

	// Upload: new.go (new) + b.go (changed).
	assert.Equal(t, []string{"b.go", "new.go"}, upload)

	// Download: b.go (changed) + deleted.go (not in local).
	assert.Equal(t, []string{"b.go", "deleted.go"}, download)

	// Remove: deleted.go (in remote, not in local).
	assert.Equal(t, []string{"deleted.go"}, remove)
}

func TestComputeManifest_FlintignoreRespected(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "node_modules/foo"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "node_modules/foo/index.js"), []byte("hi"), 0o644))

	// Create .flintignore that excludes node_modules.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".flintignore"), []byte("node_modules/**\n"), 0o644))

	m, err := ComputeManifest(dir)
	require.NoError(t, err)

	assert.Contains(t, m, "main.go")
	assert.NotContains(t, m, "node_modules/foo/index.js")
}
