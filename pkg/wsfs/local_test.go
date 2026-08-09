package wsfs_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/wsfs"
)

func newLocalFS(t *testing.T) *wsfs.LocalFS {
	t.Helper()
	fs, err := wsfs.NewLocal(t.TempDir())
	require.NoError(t, err)
	return fs
}

func TestLocalFS_CreateAndOpen(t *testing.T) {
	ctx := context.Background()
	lfs := newLocalFS(t)

	content := "hello workspace"

	// Create
	w, err := lfs.Create(ctx, "subdir/file.txt")
	require.NoError(t, err)
	_, err = io.WriteString(w, content)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	// Open
	r, err := lfs.Open(ctx, "subdir/file.txt")
	require.NoError(t, err)
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())

	assert.Equal(t, content, string(got))
}

func TestLocalFS_Stat(t *testing.T) {
	ctx := context.Background()
	lfs := newLocalFS(t)

	// Write a file
	w, err := lfs.Create(ctx, "data/payload.bin")
	require.NoError(t, err)
	_, err = io.WriteString(w, strings.Repeat("x", 1024))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	info, err := lfs.Stat(ctx, "data/payload.bin")
	require.NoError(t, err)
	assert.Equal(t, "payload.bin", info.Name)
	assert.Equal(t, int64(1024), info.Size)
	assert.False(t, info.IsDir)
	assert.False(t, info.ModTime.IsZero())
}

func TestLocalFS_Stat_NotExist(t *testing.T) {
	ctx := context.Background()
	lfs := newLocalFS(t)

	_, err := lfs.Stat(ctx, "does/not/exist.txt")
	assert.True(t, wsfs.IsNotExist(err), "expected ErrNotExist, got %v", err)
}

func TestLocalFS_ReadDir(t *testing.T) {
	ctx := context.Background()
	lfs := newLocalFS(t)

	for _, name := range []string{"a.txt", "b.txt", "sub/c.txt"} {
		w, err := lfs.Create(ctx, name)
		require.NoError(t, err)
		require.NoError(t, w.Close())
	}

	entries, err := lfs.ReadDir(ctx, ".")
	require.NoError(t, err)

	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name] = true
	}
	assert.True(t, names["a.txt"])
	assert.True(t, names["b.txt"])
	assert.True(t, names["sub"])
}

func TestLocalFS_MkdirAll(t *testing.T) {
	ctx := context.Background()
	lfs := newLocalFS(t)

	require.NoError(t, lfs.MkdirAll(ctx, "deep/nested/dir"))

	info, err := lfs.Stat(ctx, "deep/nested/dir")
	require.NoError(t, err)
	assert.True(t, info.IsDir)

	// Idempotent — calling again must not error.
	require.NoError(t, lfs.MkdirAll(ctx, "deep/nested/dir"))
}

func TestLocalFS_Remove(t *testing.T) {
	ctx := context.Background()
	lfs := newLocalFS(t)

	w, err := lfs.Create(ctx, "to-delete.txt")
	require.NoError(t, err)
	require.NoError(t, w.Close())

	require.NoError(t, lfs.Remove(ctx, "to-delete.txt"))

	_, err = lfs.Stat(ctx, "to-delete.txt")
	assert.True(t, wsfs.IsNotExist(err))
}

func TestLocalFS_Remove_NotExist(t *testing.T) {
	ctx := context.Background()
	lfs := newLocalFS(t)

	err := lfs.Remove(ctx, "ghost.txt")
	assert.True(t, wsfs.IsNotExist(err))
}

// TestLocalFS_PathTraversal verifies that traversal attacks are blocked.
func TestLocalFS_PathTraversal(t *testing.T) {
	ctx := context.Background()
	lfs := newLocalFS(t)

	attacks := []string{
		"../escape",
		"../../etc/passwd",
		"subdir/../../escape",
		"/absolute/path",
	}

	for _, path := range attacks {
		t.Run(path, func(t *testing.T) {
			_, err := lfs.Open(ctx, path)
			assert.True(t, wsfs.IsPermission(err) || wsfs.IsNotExist(err),
				"expected permission/not-exist error for %q, got %v", path, err)

			_, err = lfs.Create(ctx, path)
			assert.True(t, wsfs.IsPermission(err),
				"expected permission error on create for %q, got %v", path, err)

			_, err = lfs.Stat(ctx, path)
			assert.True(t, wsfs.IsPermission(err) || wsfs.IsNotExist(err),
				"expected permission/not-exist error on stat for %q, got %v", path, err)
		})
	}
}

func TestLocalFS_Create_TruncatesExisting(t *testing.T) {
	ctx := context.Background()
	lfs := newLocalFS(t)

	write := func(content string) {
		w, err := lfs.Create(ctx, "file.txt")
		require.NoError(t, err)
		_, err = io.WriteString(w, content)
		require.NoError(t, err)
		require.NoError(t, w.Close())
	}

	write("long original content that should be truncated")
	write("short")

	r, err := lfs.Open(ctx, "file.txt")
	require.NoError(t, err)
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	r.Close()

	assert.Equal(t, "short", string(got))
}
