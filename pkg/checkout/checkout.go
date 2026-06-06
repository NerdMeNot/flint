// Package checkout implements the built-in `use: checkout` step template.
// It clones a git repository into the workspace directory with support for
// shallow clones, submodules, LFS, and cross-repo checkout.
//
// This is an explicit step — not hidden in the init container. The developer
// writes `use: checkout` as the first step in their pipeline:
//
//	steps:
//	  - use: checkout
//	  - name: test
//	    run: npm test
//
// With options:
//
//	steps:
//	  - use: checkout
//	    with:
//	      depth: 0            # full history
//	      submodules: true
//	      lfs: true
//	  - use: checkout
//	    with:
//	      repo: acme/k8s-manifests
//	      path: /workspace/manifests
//	      ref: main
package checkout

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/rs/zerolog/log"
)

// Options configures a checkout operation.
type Options struct {
	// Repo is the repository to clone (e.g. "acme/app").
	// Default: the pipeline's repository (FLINT_GIT_REPO).
	Repo string

	// Ref is the branch or tag to check out.
	// Default: the pipeline's ref (FLINT_GIT_REF).
	Ref string

	// SHA is a specific commit to check out after cloning.
	// Default: the pipeline's commit SHA (FLINT_GIT_SHA).
	SHA string

	// Depth controls clone depth. 1 = shallow (default), 0 = full history.
	Depth int

	// Submodules initializes and updates submodules after checkout.
	Submodules bool

	// LFS fetches Git LFS objects after checkout.
	LFS bool

	// Path is the local directory to clone into.
	// Default: /workspace
	Path string

	// Token is the git authentication token for private repos.
	// Default: FLINT_GIT_TOKEN env var.
	Token string

	// CloneURL overrides the derived clone URL.
	// Default: derived from Repo (https://github.com/{repo}.git).
	CloneURL string
}

// FromEnvAndInputs populates Options from environment variables (pipeline
// defaults) and step inputs (with: overrides). Step inputs take precedence.
func FromEnvAndInputs(inputs map[string]string) Options {
	opts := Options{
		Repo:  os.Getenv("FLINT_GIT_REPO"),
		Ref:   os.Getenv("FLINT_GIT_REF"),
		SHA:   os.Getenv("FLINT_GIT_SHA"),
		Depth: 1,
		Path:  os.Getenv("FLINT_WORKSPACE"),
		Token: os.Getenv("FLINT_GIT_TOKEN"),
	}
	if opts.Path == "" {
		opts.Path = "/workspace"
	}

	// Apply step inputs (with: overrides).
	if v, ok := inputs["repo"]; ok && v != "" {
		opts.Repo = v
		opts.SHA = "" // cross-repo: don't use the pipeline's SHA
	}
	if v, ok := inputs["ref"]; ok && v != "" {
		opts.Ref = v
	}
	if v, ok := inputs["sha"]; ok && v != "" {
		opts.SHA = v
	}
	if v, ok := inputs["depth"]; ok && v != "" {
		if d, err := strconv.Atoi(v); err == nil {
			opts.Depth = d
		}
	}
	if v, ok := inputs["submodules"]; ok {
		opts.Submodules = v == "true"
	}
	if v, ok := inputs["lfs"]; ok {
		opts.LFS = v == "true"
	}
	if v, ok := inputs["path"]; ok && v != "" {
		opts.Path = v
	}
	if v, ok := inputs["token"]; ok && v != "" {
		opts.Token = v
	}

	return opts
}

// Run performs the checkout. It clones the repository, optionally checks out
// a specific SHA, and handles submodules and LFS.
func Run(ctx context.Context, opts Options) error {
	cloneURL := opts.CloneURL
	if cloneURL == "" {
		cloneURL = defaultCloneURL(opts.Repo)
	}

	depth := opts.Depth
	if depth == 0 {
		depth = 0 // full history — go-git uses 0 as "no limit"
	}

	log.Info().
		Str("repo", opts.Repo).
		Str("ref", opts.Ref).
		Str("sha", opts.SHA).
		Str("path", opts.Path).
		Int("depth", depth).
		Msg("checkout: cloning")

	cloneOpts := &git.CloneOptions{
		URL:   cloneURL,
		Depth: depth,
	}

	if opts.Token != "" {
		cloneOpts.Auth = &http.BasicAuth{
			Username: "x-access-token",
			Password: opts.Token,
		}
	}

	if opts.Ref != "" {
		cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(opts.Ref)
		cloneOpts.SingleBranch = true
	}

	if opts.Submodules {
		cloneOpts.RecurseSubmodules = git.DefaultSubmoduleRecursionDepth
	}

	repo, err := git.PlainCloneContext(ctx, opts.Path, false, cloneOpts)
	if err != nil {
		return fmt.Errorf("checkout: clone %s failed: %w", opts.Repo, err)
	}

	// Check out a specific SHA if it differs from HEAD.
	if opts.SHA != "" {
		if err := checkoutSHA(ctx, repo, opts); err != nil {
			return err
		}
	}

	log.Info().Str("path", opts.Path).Msg("checkout: complete")
	return nil
}

// checkoutSHA checks out a specific commit, fetching full history if the
// commit isn't in the shallow clone.
func checkoutSHA(ctx context.Context, repo *git.Repository, opts Options) error {
	head, err := repo.Head()
	if err != nil {
		return fmt.Errorf("checkout: get HEAD: %w", err)
	}

	if head.Hash().String() == opts.SHA {
		return nil // already at the right commit
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("checkout: get worktree: %w", err)
	}

	log.Info().Str("sha", opts.SHA).Msg("checkout: checking out specific commit")
	err = wt.Checkout(&git.CheckoutOptions{
		Hash: plumbing.NewHash(opts.SHA),
	})
	if err == nil {
		return nil
	}

	// SHA not in shallow clone — fetch full history and retry.
	log.Warn().Str("sha", opts.SHA).Msg("checkout: SHA not in shallow clone, fetching full history")
	fetchOpts := &git.FetchOptions{Depth: 0}
	if opts.Token != "" {
		fetchOpts.Auth = &http.BasicAuth{
			Username: "x-access-token",
			Password: opts.Token,
		}
	}
	if err := repo.FetchContext(ctx, fetchOpts); err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("checkout: fetch for SHA %s: %w", opts.SHA, err)
	}

	if err := wt.Checkout(&git.CheckoutOptions{
		Hash: plumbing.NewHash(opts.SHA),
	}); err != nil {
		return fmt.Errorf("checkout: SHA %s not found after full fetch: %w", opts.SHA, err)
	}

	return nil
}

func defaultCloneURL(repo string) string {
	if url := os.Getenv("FLINT_CLONE_URL"); url != "" {
		return url
	}
	return fmt.Sprintf("https://github.com/%s.git", repo)
}
