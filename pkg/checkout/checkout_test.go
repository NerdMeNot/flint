package checkout

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromEnvAndInputs_Defaults(t *testing.T) {
	t.Setenv("FLINT_GIT_REPO", "acme/app")
	t.Setenv("FLINT_GIT_REF", "main")
	t.Setenv("FLINT_GIT_SHA", "abc123")
	t.Setenv("FLINT_WORKSPACE", "/workspace")

	opts := FromEnvAndInputs(nil)

	assert.Equal(t, "acme/app", opts.Repo)
	assert.Equal(t, "main", opts.Ref)
	assert.Equal(t, "abc123", opts.SHA)
	assert.Equal(t, 1, opts.Depth)
	assert.Equal(t, "/workspace", opts.Path)
	assert.False(t, opts.Submodules)
	assert.False(t, opts.LFS)
}

func TestFromEnvAndInputs_Overrides(t *testing.T) {
	t.Setenv("FLINT_GIT_REPO", "acme/app")
	t.Setenv("FLINT_GIT_REF", "main")
	t.Setenv("FLINT_GIT_SHA", "abc123")
	t.Setenv("FLINT_WORKSPACE", "/workspace")

	inputs := map[string]string{
		"repo":       "acme/k8s-manifests",
		"ref":        "release/v2",
		"depth":      "0",
		"submodules": "true",
		"path":       "/workspace/manifests",
	}

	opts := FromEnvAndInputs(inputs)

	assert.Equal(t, "acme/k8s-manifests", opts.Repo, "repo override")
	assert.Equal(t, "release/v2", opts.Ref, "ref override")
	assert.Empty(t, opts.SHA, "cross-repo: SHA should be cleared")
	assert.Equal(t, 0, opts.Depth, "depth override")
	assert.True(t, opts.Submodules, "submodules override")
	assert.Equal(t, "/workspace/manifests", opts.Path, "path override")
}

func TestFromEnvAndInputs_DefaultPath(t *testing.T) {
	// No FLINT_WORKSPACE set — should default to /workspace.
	opts := FromEnvAndInputs(nil)
	assert.Equal(t, "/workspace", opts.Path)
}

// createBareRepo creates a bare git repo with a single commit containing
// a file, and returns the bare repo path and the commit SHA.
func createBareRepo(t *testing.T) (bareDir string, sha string) {
	t.Helper()

	// Create a working repo first to generate a commit.
	workDir := filepath.Join(t.TempDir(), "work")
	require.NoError(t, os.MkdirAll(workDir, 0o755))

	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "command %v failed: %s", args, out)
		return strings.TrimSpace(string(out))
	}

	run(workDir, "git", "init", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "README.md"), []byte("# Test Repo"), 0o644))
	run(workDir, "git", "add", ".")
	run(workDir, "git", "commit", "-m", "initial commit")
	commitSHA := run(workDir, "git", "rev-parse", "HEAD")

	// Clone to a bare repo for use as a "remote".
	bareDir = filepath.Join(t.TempDir(), "bare.git")
	run(t.TempDir(), "git", "clone", "--bare", workDir, bareDir)

	return bareDir, commitSHA
}

// createBareRepoWithTwoCommits creates a bare git repo with two commits and
// returns the bare repo path, first commit SHA, and second commit SHA.
func createBareRepoWithTwoCommits(t *testing.T) (bareDir, sha1, sha2 string) {
	t.Helper()

	workDir := filepath.Join(t.TempDir(), "work")
	require.NoError(t, os.MkdirAll(workDir, 0o755))

	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "command %v failed: %s", args, out)
		return strings.TrimSpace(string(out))
	}

	run(workDir, "git", "init", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "file1.txt"), []byte("first"), 0o644))
	run(workDir, "git", "add", ".")
	run(workDir, "git", "commit", "-m", "first commit")
	firstSHA := run(workDir, "git", "rev-parse", "HEAD")

	require.NoError(t, os.WriteFile(filepath.Join(workDir, "file2.txt"), []byte("second"), 0o644))
	run(workDir, "git", "add", ".")
	run(workDir, "git", "commit", "-m", "second commit")
	secondSHA := run(workDir, "git", "rev-parse", "HEAD")

	bareDir = filepath.Join(t.TempDir(), "bare.git")
	run(t.TempDir(), "git", "clone", "--bare", workDir, bareDir)

	return bareDir, firstSHA, secondSHA
}

func TestRun_ClonesLocalBareRepo(t *testing.T) {
	bareDir, _ := createBareRepo(t)
	destDir := filepath.Join(t.TempDir(), "clone")

	opts := Options{
		CloneURL: bareDir,
		Ref:      "main",
		Depth:    0, // full history to avoid shallow clone issues with bare repos
		Path:     destDir,
	}

	err := Run(context.Background(), opts)
	require.NoError(t, err)

	// Verify the cloned file exists with the expected content.
	got, err := os.ReadFile(filepath.Join(destDir, "README.md"))
	require.NoError(t, err)
	assert.Equal(t, "# Test Repo", string(got))
}

func TestRun_InvalidRepoURL(t *testing.T) {
	destDir := filepath.Join(t.TempDir(), "clone")

	opts := Options{
		CloneURL: "/nonexistent/repo.git",
		Repo:     "bogus/repo",
		Ref:      "main",
		Depth:    1,
		Path:     destDir,
	}

	err := Run(context.Background(), opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "checkout: clone")
}

func TestRun_CheckoutSpecificSHA(t *testing.T) {
	bareDir, firstSHA, _ := createBareRepoWithTwoCommits(t)
	destDir := filepath.Join(t.TempDir(), "clone")

	opts := Options{
		CloneURL: bareDir,
		Ref:      "main",
		Depth:    0, // full clone so SHA lookup works
		SHA:      firstSHA,
		Path:     destDir,
	}

	err := Run(context.Background(), opts)
	require.NoError(t, err)

	// At the first commit, only file1.txt should exist, not file2.txt.
	got, err := os.ReadFile(filepath.Join(destDir, "file1.txt"))
	require.NoError(t, err)
	assert.Equal(t, "first", string(got))

	_, err = os.Stat(filepath.Join(destDir, "file2.txt"))
	assert.True(t, os.IsNotExist(err), "file2.txt should not exist at the first commit")
}
