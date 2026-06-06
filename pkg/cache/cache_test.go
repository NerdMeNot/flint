package cache

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheKey_Namespacing(t *testing.T) {
	c := &S3Cache{orgID: "org1", projectID: "proj1"}

	key1 := c.cacheKey("npm-abc123")
	key2 := c.cacheKey("npm-def456")

	assert.Equal(t, "cache/org1/proj1/npm-abc123.tar.zst", key1)
	assert.Equal(t, "cache/org1/proj1/npm-def456.tar.zst", key2)
	assert.NotEqual(t, key1, key2)
}

func TestCacheKey_ProjectIsolation(t *testing.T) {
	c1 := &S3Cache{orgID: "org1", projectID: "proj1"}
	c2 := &S3Cache{orgID: "org1", projectID: "proj2"}

	assert.NotEqual(t,
		c1.cacheKey("npm-abc123"),
		c2.cacheKey("npm-abc123"),
		"same cache key in different projects should have different S3 keys")
}

func TestCompressExtract_RoundTrip(t *testing.T) {
	// Create source files that simulate a typical cache (e.g. node_modules).
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "node_modules/pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "node_modules/pkg/index.js"), []byte("module.exports = {}"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "node_modules/pkg/package.json"), []byte(`{"name":"pkg"}`), 0o644))

	// Compress the paths.
	paths := []string{filepath.Join(srcDir, "node_modules")}
	var buf bytes.Buffer
	require.NoError(t, compress(paths, &buf))
	assert.Greater(t, buf.Len(), 0, "compressed output should be non-empty")

	// Extract to simulate restoring. The cache extract function writes to
	// the same absolute paths (like the cache package does), so we extract
	// from the same working directory context. We'll verify the round-trip
	// by removing and re-extracting.
	require.NoError(t, os.RemoveAll(filepath.Join(srcDir, "node_modules")))
	require.NoError(t, extract(&buf))

	// Verify files are restored.
	got, err := os.ReadFile(filepath.Join(srcDir, "node_modules/pkg/index.js"))
	require.NoError(t, err)
	assert.Equal(t, "module.exports = {}", string(got))

	got2, err := os.ReadFile(filepath.Join(srcDir, "node_modules/pkg/package.json"))
	require.NoError(t, err)
	assert.Equal(t, `{"name":"pkg"}`, string(got2))
}

func TestRestore_EmptyKey_ReturnsNoOp(t *testing.T) {
	c := NewS3("org1", "proj1", "bucket", "us-east-1")

	// Empty key should return (false, nil) without touching S3.
	hit, err := c.Restore(t.Context(), "", []string{"/some/path"})
	assert.NoError(t, err)
	assert.False(t, hit, "empty key should be a no-op (no cache hit)")
}

func TestSave_EmptyKey_ReturnsNoOp(t *testing.T) {
	c := NewS3("org1", "proj1", "bucket", "us-east-1")

	// Empty key should return nil without touching S3.
	err := c.Save(t.Context(), "", []string{"/some/path"})
	assert.NoError(t, err, "empty key should be a no-op")
}

func TestRestore_EmptyPaths_ReturnsNoOp(t *testing.T) {
	c := NewS3("org1", "proj1", "bucket", "us-east-1")

	// Empty paths should return (false, nil) without touching S3.
	hit, err := c.Restore(t.Context(), "some-key", nil)
	assert.NoError(t, err)
	assert.False(t, hit, "empty paths should be a no-op")
}

func TestSave_EmptyPaths_ReturnsNoOp(t *testing.T) {
	c := NewS3("org1", "proj1", "bucket", "us-east-1")

	// Empty paths should return nil without touching S3.
	err := c.Save(t.Context(), "some-key", nil)
	assert.NoError(t, err, "empty paths should be a no-op")
}
