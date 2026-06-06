package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupAgentFiles_RemovesFiles(t *testing.T) {
	dir := t.TempDir()

	files := []string{".flint-emit", ".flint-exit", ".flint-step.log"}
	for _, name := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte("test content"), 0644))
	}

	// Verify files exist before cleanup.
	for _, name := range files {
		_, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err, "expected %s to exist before cleanup", name)
	}

	cleanupAgentFiles(dir)

	// Verify files are gone after cleanup.
	for _, name := range files {
		_, err := os.Stat(filepath.Join(dir, name))
		assert.True(t, os.IsNotExist(err), "expected %s to be removed after cleanup", name)
	}
}

func TestCleanupAgentFiles_NonExistentFiles(t *testing.T) {
	dir := t.TempDir()

	// Should not panic or error on missing files.
	assert.NotPanics(t, func() {
		cleanupAgentFiles(dir)
	})
}

func TestCleanupAgentFiles_PreservesOtherFiles(t *testing.T) {
	dir := t.TempDir()

	// Create an agent file and a user file.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".flint-emit"), []byte("test"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "user-file.txt"), []byte("keep me"), 0644))

	cleanupAgentFiles(dir)

	// Agent file removed.
	_, err := os.Stat(filepath.Join(dir, ".flint-emit"))
	assert.True(t, os.IsNotExist(err))

	// User file preserved.
	data, err := os.ReadFile(filepath.Join(dir, "user-file.txt"))
	require.NoError(t, err)
	assert.Equal(t, "keep me", string(data))
}
