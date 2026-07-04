package checkout

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeSourceRepo builds a local "remote" with one commit and returns its path
// and head SHA.
func makeSourceRepo(t *testing.T) (path, sha string) {
	t.Helper()
	path = t.TempDir()
	repo, err := git.PlainInit(path, false)
	require.NoError(t, err)
	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(path, "README.md"), []byte("hello flint"), 0o600))
	_, err = wt.Add("README.md")
	require.NoError(t, err)
	commit, err := wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "t@example.com"},
	})
	require.NoError(t, err)
	return path, commit.String()
}

func TestMirrorStore_EnsureCloneThenFetch(t *testing.T) {
	src, _ := makeSourceRepo(t)
	store := &MirrorStore{Root: t.TempDir()}
	ctx := context.Background()

	// First Ensure: full mirror clone.
	mirror1, err := store.Ensure(ctx, "acme/app", src, "")
	require.NoError(t, err)
	assert.DirExists(t, mirror1)

	// New commit lands upstream.
	repo, err := git.PlainOpen(src)
	require.NoError(t, err)
	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(src, "new.txt"), []byte("more"), 0o600))
	_, err = wt.Add("new.txt")
	require.NoError(t, err)
	newSHA, err := wt.Commit("second", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "t@example.com"},
	})
	require.NoError(t, err)

	// Second Ensure: fetch delta; the mirror sees the new commit.
	mirror2, err := store.Ensure(ctx, "acme/app", src, "")
	require.NoError(t, err)
	assert.Equal(t, mirror1, mirror2)
	bare, err := git.PlainOpen(mirror2)
	require.NoError(t, err)
	_, err = bare.CommitObject(newSHA)
	assert.NoError(t, err, "mirror should have fetched the new commit")
}

func TestRunWithMirror_ChecksOutFromLocalMirror(t *testing.T) {
	src, sha := makeSourceRepo(t)
	store := &MirrorStore{Root: t.TempDir()}
	workspace := t.TempDir()

	err := RunWithMirror(context.Background(), store, Options{
		Repo: "acme/app", CloneURL: src, SHA: sha,
		Path: filepath.Join(workspace, "checkout"),
	})
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(workspace, "checkout", "README.md"))
}

func TestRunWithMirror_FallsBackWhenMirrorRootBroken(t *testing.T) {
	src, sha := makeSourceRepo(t)
	// A file where the mirror root should be forces Ensure to fail.
	brokenRoot := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(brokenRoot, []byte("x"), 0o600))
	store := &MirrorStore{Root: brokenRoot}
	workspace := t.TempDir()

	err := RunWithMirror(context.Background(), store, Options{
		Repo: "acme/app", CloneURL: src, SHA: sha,
		Path: filepath.Join(workspace, "checkout"),
	})
	require.NoError(t, err, "mirror failure must fall back to the network clone path")
	assert.FileExists(t, filepath.Join(workspace, "checkout", "README.md"))
}
