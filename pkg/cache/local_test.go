package cache

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalStore_SaveRestoreRoundTrip(t *testing.T) {
	ws := t.TempDir()
	store, err := NewLocal(t.TempDir(), 0)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, os.MkdirAll(filepath.Join(ws, "node_modules"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, "node_modules", "pkg.js"), []byte("cached"), 0o644))

	require.NoError(t, store.Save(ctx, ws, "npm-abc123", []string{"node_modules"}))

	// Restore into a DIFFERENT workspace — archives are root-relative, so a
	// cache saved from one run's workspace restores into any other.
	ws2 := t.TempDir()
	hit, err := store.Restore(ctx, ws2, "npm-abc123", []string{"node_modules"})
	require.NoError(t, err)
	assert.True(t, hit)
	data, err := os.ReadFile(filepath.Join(ws2, "node_modules", "pkg.js"))
	require.NoError(t, err)
	assert.Equal(t, "cached", string(data))

	// Miss on unknown key.
	hit, err = store.Restore(ctx, ws2, "npm-zzz", nil)
	require.NoError(t, err)
	assert.False(t, hit)
}

func TestLocalStore_PrefixFallback(t *testing.T) {
	ws := t.TempDir()
	store, err := NewLocal(t.TempDir(), 0)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, os.WriteFile(filepath.Join(ws, "dep.txt"), []byte("v1"), 0o644))
	require.NoError(t, store.Save(ctx, ws, "npm-v1", []string{"dep.txt"}))
	require.NoError(t, os.WriteFile(filepath.Join(ws, "dep.txt"), []byte("v2"), 0o644))
	require.NoError(t, store.Save(ctx, ws, "npm-v2", []string{"dep.txt"}))

	require.NoError(t, os.Remove(filepath.Join(ws, "dep.txt")))
	match, err := store.RestoreWithFallback(ctx, ws, "npm-v9", []string{"npm-"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "npm-v2", match, "lexicographically last prefix match wins")
	data, _ := os.ReadFile(filepath.Join(ws, "dep.txt"))
	assert.Equal(t, "v2", string(data))
}

func TestLocalStore_LRUEviction(t *testing.T) {
	ws := t.TempDir()
	// Tiny watermark forces eviction of the older entry.
	store, err := NewLocal(t.TempDir(), 4096)
	require.NoError(t, err)
	ctx := context.Background()

	// Incompressible payload so the archives actually occupy the watermark.
	big := make([]byte, 3000)
	_, err = rand.Read(big)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(ws, "blob.bin"), big, 0o644))
	require.NoError(t, store.Save(ctx, ws, "old-key", []string{"blob.bin"}))
	require.NoError(t, store.Save(ctx, ws, "new-key", []string{"blob.bin"}))

	hit, err := store.Restore(ctx, ws, "old-key", nil)
	require.NoError(t, err)
	assert.False(t, hit, "older entry should have been evicted")
	hit, err = store.Restore(ctx, ws, "new-key", nil)
	require.NoError(t, err)
	assert.True(t, hit, "newest entry survives")
}

func TestLocalStore_IndexSurvivesRestart(t *testing.T) {
	ws := t.TempDir()
	dir := t.TempDir()
	ctx := context.Background()

	store1, err := NewLocal(dir, 0)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(ws, "f.txt"), []byte("warm"), 0o644))
	require.NoError(t, store1.Save(ctx, ws, "k1", []string{"f.txt"}))

	store2, err := NewLocal(dir, 0)
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(ws, "f.txt")))
	hit, err := store2.Restore(ctx, ws, "k1", nil)
	require.NoError(t, err)
	assert.True(t, hit, "a restarted agent keeps its warm cache")
}

func TestLayered_LocalFirstThenRemote(t *testing.T) {
	ws := t.TempDir()
	local, err := NewLocal(t.TempDir(), 0)
	require.NoError(t, err)
	remote := &fakeRemote{entries: map[string][]byte{}}
	layered := NewLayered(local, remote)
	ctx := context.Background()

	require.NoError(t, os.WriteFile(filepath.Join(ws, "a.txt"), []byte("x"), 0o644))
	require.NoError(t, layered.Save(ctx, ws, "key-a", []string{"a.txt"}))
	assert.Equal(t, 1, remote.saves, "save writes through to the remote tier")

	// Local hit does not touch the remote.
	require.NoError(t, os.Remove(filepath.Join(ws, "a.txt")))
	hit, err := layered.Restore(ctx, ws, "key-a", nil)
	require.NoError(t, err)
	assert.True(t, hit)
	assert.Zero(t, remote.restores, "local hit must not hit the remote")

	// Local miss falls through to the remote.
	_, err = layered.Restore(ctx, ws, "only-remote", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, remote.restores)
}

// fakeRemote counts calls; restores always miss.
type fakeRemote struct {
	entries  map[string][]byte
	saves    int
	restores int
}

func (f *fakeRemote) Restore(context.Context, string, string, []string) (bool, error) {
	f.restores++
	return false, nil
}
func (f *fakeRemote) RestoreWithFallback(context.Context, string, string, []string, []string) (string, error) {
	f.restores++
	return "", nil
}
func (f *fakeRemote) Save(_ context.Context, _ string, key string, _ []string) error {
	f.saves++
	f.entries[key] = []byte("saved")
	return nil
}
