package artifact

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArtifactKey_NoCollision(t *testing.T) {
	ref1 := Ref{OrgID: "org1", RunID: "run1", StepName: "build", Name: "dist"}
	ref2 := Ref{OrgID: "org1", RunID: "run1", StepName: "build", Name: "dist-api"}

	key1 := artifactKey(ref1)
	key2 := artifactKey(ref2)

	assert.NotEqual(t, key1, key2, "different artifact names should produce different keys")
	assert.Equal(t, "artifacts/org1/run1/build/dist.tar.zst", key1)
	assert.Equal(t, "artifacts/org1/run1/build/dist-api.tar.zst", key2)
}

func TestArtifactKey_StepIsolation(t *testing.T) {
	ref1 := Ref{OrgID: "org1", RunID: "run1", StepName: "build", Name: "output"}
	ref2 := Ref{OrgID: "org1", RunID: "run1", StepName: "test", Name: "output"}

	assert.NotEqual(t, artifactKey(ref1), artifactKey(ref2),
		"same artifact name in different steps should produce different keys")
}

func TestCompressExtract_RoundTrip(t *testing.T) {
	// Create source files in a temp directory.
	srcDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "hello.txt"), []byte("hello world"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "sub/nested.txt"), []byte("nested content"), 0o644))

	// Compress.
	var buf bytes.Buffer
	require.NoError(t, compress(srcDir, &buf))
	assert.Greater(t, buf.Len(), 0, "compressed output should be non-empty")

	// Extract to a different directory.
	destDir := t.TempDir()
	require.NoError(t, extract(&buf, destDir))

	// Verify content matches.
	got1, err := os.ReadFile(filepath.Join(destDir, "hello.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(got1))

	got2, err := os.ReadFile(filepath.Join(destDir, "sub/nested.txt"))
	require.NoError(t, err)
	assert.Equal(t, "nested content", string(got2))
}

func TestExtract_PathTraversalSkipped(t *testing.T) {
	// Build a tar.zst archive with a malicious "../escape.txt" entry.
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	require.NoError(t, err)

	tw := tar.NewWriter(zw)
	payload := []byte("pwned")

	// Good entry.
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "safe.txt",
		Size:     int64(len(payload)),
		Typeflag: tar.TypeReg,
		Mode:     0o644,
	}))
	_, err = tw.Write(payload)
	require.NoError(t, err)

	// Malicious entry with path traversal.
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "../escape.txt",
		Size:     int64(len(payload)),
		Typeflag: tar.TypeReg,
		Mode:     0o644,
	}))
	_, err = tw.Write(payload)
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, zw.Close())

	// Extract into a dest directory.
	destDir := t.TempDir()
	require.NoError(t, extract(&buf, destDir))

	// The safe file should exist.
	_, err = os.Stat(filepath.Join(destDir, "safe.txt"))
	assert.NoError(t, err, "safe.txt should be extracted")

	// The traversal file should NOT exist outside destDir.
	_, err = os.Stat(filepath.Join(destDir, "..", "escape.txt"))
	assert.True(t, os.IsNotExist(err), "path traversal entry should be skipped")
}

func TestCompressExtract_EmptyDirectory(t *testing.T) {
	// Compress an empty directory.
	srcDir := t.TempDir()

	var buf bytes.Buffer
	require.NoError(t, compress(srcDir, &buf))

	// Extract to a new directory.
	destDir := t.TempDir()
	require.NoError(t, extract(&buf, destDir))

	// destDir should exist and be empty (just the "." from the walk).
	entries, err := os.ReadDir(destDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "extracting an empty archive should produce no files")
}
