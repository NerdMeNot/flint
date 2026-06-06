package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluateCacheKey_SimpleString(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		CacheKey:  "my-cache-key-v1",
		Workspace: dir,
		GitRef:    "refs/heads/main",
		GitSHA:    "abc123",
	}

	key, err := EvaluateCacheKey(cfg)
	require.NoError(t, err)
	assert.Equal(t, "my-cache-key-v1", key)
}

func TestEvaluateCacheKey_WithHashFiles(t *testing.T) {
	dir := t.TempDir()

	// Create a file in the workspace to hash.
	lockContent := `{"name":"test-app","version":"1.0.0","lockfileVersion":3}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(lockContent), 0644))

	cfg := &Config{
		CacheKey:  "npm-${{ hashFiles('package-lock.json') }}",
		Workspace: dir,
		GitRef:    "refs/heads/main",
		GitSHA:    "abc123",
	}

	key, err := EvaluateCacheKey(cfg)
	require.NoError(t, err)

	// The key should have the "npm-" prefix followed by a hex hash.
	assert.Regexp(t, `^npm-[a-f0-9]{16}$`, key)
	assert.NotEqual(t, "npm-empty", key)
}

func TestEvaluateCacheKey_HashFilesNoMatch(t *testing.T) {
	dir := t.TempDir()
	// No matching file exists in the workspace.

	cfg := &Config{
		CacheKey:  "npm-${{ hashFiles('package-lock.json') }}",
		Workspace: dir,
		GitRef:    "refs/heads/main",
		GitSHA:    "abc123",
	}

	key, err := EvaluateCacheKey(cfg)
	require.NoError(t, err)
	// When no files match, hashFiles returns "empty".
	assert.Equal(t, "npm-empty", key)
}

func TestEvaluateCacheKey_DeterministicHash(t *testing.T) {
	dir := t.TempDir()

	content := "some-lockfile-content-here"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "yarn.lock"), []byte(content), 0644))

	cfg := &Config{
		CacheKey:  "yarn-${{ hashFiles('yarn.lock') }}",
		Workspace: dir,
		GitRef:    "refs/heads/main",
		GitSHA:    "abc123",
	}

	key1, err := EvaluateCacheKey(cfg)
	require.NoError(t, err)

	key2, err := EvaluateCacheKey(cfg)
	require.NoError(t, err)

	assert.Equal(t, key1, key2, "hash should be deterministic across calls")
}

func TestEvaluateCacheKey_DifferentContentDifferentHash(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.sum"), []byte("content-v1"), 0644))

	cfg := &Config{
		CacheKey:  "go-${{ hashFiles('go.sum') }}",
		Workspace: dir,
		GitRef:    "refs/heads/main",
		GitSHA:    "abc123",
	}

	key1, err := EvaluateCacheKey(cfg)
	require.NoError(t, err)

	// Change the file contents.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.sum"), []byte("content-v2"), 0644))

	key2, err := EvaluateCacheKey(cfg)
	require.NoError(t, err)

	assert.NotEqual(t, key1, key2, "different file contents should produce different hashes")
}

func TestEvaluateCacheKey_WithBranchInterpolation(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		CacheKey:  "cache-${{ branch }}",
		Workspace: dir,
		GitRef:    "feature/awesome",
		GitSHA:    "abc123",
	}

	key, err := EvaluateCacheKey(cfg)
	require.NoError(t, err)
	assert.Equal(t, "cache-feature/awesome", key)
}
