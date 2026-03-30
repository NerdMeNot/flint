package agent

import (
	"context"
	"fmt"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/rs/zerolog/log"
)

// Clone clones the repository into the workspace directory and checks out
// the correct ref/SHA. Supports private repos via token authentication.
func Clone(ctx context.Context, cfg *Config) error {
	cloneURL := resolveCloneURL(cfg)

	log.Info().
		Str("repo", cfg.GitRepo).
		Str("ref", cfg.GitRef).
		Str("sha", cfg.GitSHA).
		Str("workspace", cfg.Workspace).
		Msg("cloning repository")

	cloneOpts := &git.CloneOptions{
		URL:   cloneURL,
		Depth: 1,
	}

	// Authenticate for private repos.
	// Token is passed via FLINT_GIT_TOKEN env var (set by the worker from forge_connections).
	if token := os.Getenv("FLINT_GIT_TOKEN"); token != "" {
		cloneOpts.Auth = &http.BasicAuth{
			Username: "x-access-token", // works for GitHub, GitLab, Bitbucket
			Password: token,
		}
		log.Info().Msg("using authenticated clone")
	}

	if cfg.GitRef != "" {
		cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(cfg.GitRef)
		cloneOpts.SingleBranch = true
	}

	repo, err := git.PlainCloneContext(ctx, cfg.Workspace, false, cloneOpts)
	if err != nil {
		return fmt.Errorf("agent: clone failed: %w", err)
	}

	// If a specific SHA is requested and differs from HEAD, checkout that commit.
	if cfg.GitSHA != "" {
		head, err := repo.Head()
		if err != nil {
			return fmt.Errorf("agent: failed to get HEAD: %w", err)
		}

		if head.Hash().String() != cfg.GitSHA {
			wt, err := repo.Worktree()
			if err != nil {
				return fmt.Errorf("agent: failed to get worktree: %w", err)
			}

			log.Info().Str("sha", cfg.GitSHA).Msg("checking out specific SHA")
			err = wt.Checkout(&git.CheckoutOptions{
				Hash: plumbing.NewHash(cfg.GitSHA),
			})
			if err != nil {
				// SHA not in shallow clone — retry with full fetch.
				log.Warn().Str("sha", cfg.GitSHA).Msg("SHA not in shallow clone, fetching full history")
				fetchOpts := &git.FetchOptions{Depth: 0}
				if token := os.Getenv("FLINT_GIT_TOKEN"); token != "" {
					fetchOpts.Auth = &http.BasicAuth{
						Username: "x-access-token",
						Password: token,
					}
				}
				err = repo.FetchContext(ctx, fetchOpts)
				if err != nil && err != git.NoErrAlreadyUpToDate {
					return fmt.Errorf("agent: fetch for SHA failed: %w", err)
				}

				err = wt.Checkout(&git.CheckoutOptions{
					Hash: plumbing.NewHash(cfg.GitSHA),
				})
				if err != nil {
					return fmt.Errorf("agent: checkout SHA %s failed: %w", cfg.GitSHA, err)
				}
			}
		}
	}

	log.Info().Str("workspace", cfg.Workspace).Msg("clone complete")
	return nil
}

// resolveCloneURL determines the clone URL based on repo and forge type.
func resolveCloneURL(cfg *Config) string {
	// If FLINT_CLONE_URL is set explicitly (by the worker), use it.
	if url := os.Getenv("FLINT_CLONE_URL"); url != "" {
		return url
	}

	// Default to GitHub HTTPS.
	return fmt.Sprintf("https://github.com/%s.git", cfg.GitRepo)
}
