package checkout

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/gofrs/flock"
	"github.com/rs/zerolog/log"
)

// MirrorStore maintains per-repo bare mirrors on a persistent machine: the
// first checkout of a repo pays a full clone once; every later checkout pays
// only the fetch delta plus a local clone — the single biggest repeat-build
// win of persistent machines over ephemeral pods. Concurrent steps
// coordinating on the same repo serialize on a per-mirror file lock.
type MirrorStore struct {
	// Root is the mirrors directory (e.g. /var/lib/flint-agent/git-mirrors).
	Root string
}

// Ensure clones the mirror on first use, otherwise fetches updates, and
// returns the bare repo path. Safe for concurrent callers (flock-serialized).
func (m *MirrorStore) Ensure(ctx context.Context, repo, cloneURL, token string) (string, error) {
	if m == nil || m.Root == "" {
		return "", errors.New("checkout: mirror store not configured")
	}
	path := m.mirrorPath(repo)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}

	lock := flock.New(path + ".lock")
	// Bound the wait: a wedged fetch must not stall every step on the machine
	// forever — fall back to a network clone instead.
	lockCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	ok, err := lock.TryLockContext(lockCtx, 250*time.Millisecond)
	if err != nil || !ok {
		return "", fmt.Errorf("checkout: mirror lock for %s: %w", repo, err)
	}
	defer lock.Unlock() //nolint:errcheck

	var auth *http.BasicAuth
	if token != "" {
		auth = &http.BasicAuth{Username: "x-access-token", Password: token}
	}

	bare, err := git.PlainOpen(path)
	switch {
	case err == nil:
		// Existing mirror: fetch the delta.
		start := time.Now()
		fetchErr := bare.FetchContext(ctx, &git.FetchOptions{
			RemoteName: "origin", Auth: auth, Force: true, Prune: true, Tags: git.AllTags,
		})
		if fetchErr != nil && !errors.Is(fetchErr, git.NoErrAlreadyUpToDate) {
			return "", fmt.Errorf("checkout: mirror fetch %s: %w", repo, fetchErr)
		}
		log.Debug().Str("repo", repo).Dur("took", time.Since(start)).Msg("checkout: mirror fetched")
		return path, nil

	case errors.Is(err, git.ErrRepositoryNotExists):
		// First use: full mirror clone.
		start := time.Now()
		if _, err := git.PlainCloneContext(ctx, path, true, &git.CloneOptions{
			URL: cloneURL, Auth: auth, Mirror: true,
		}); err != nil {
			_ = os.RemoveAll(path) // never leave a half-clone behind the lock
			return "", fmt.Errorf("checkout: mirror clone %s: %w", repo, err)
		}
		log.Info().Str("repo", repo).Dur("took", time.Since(start)).Msg("checkout: mirror created")
		return path, nil

	default:
		return "", fmt.Errorf("checkout: open mirror %s: %w", repo, err)
	}
}

// mirrorPath maps a repo to its mirror directory. The sha suffix guards
// against case-collisions and pathological names.
func (m *MirrorStore) mirrorPath(repo string) string {
	safe := strings.ReplaceAll(strings.ToLower(repo), "/", "__")
	sum := sha256.Sum256([]byte(repo))
	return filepath.Join(m.Root, safe+"-"+hex.EncodeToString(sum[:4])+".git")
}

// RunWithMirror performs a checkout using the mirror as the clone source: the
// workspace clone reads objects from local disk (near-zero network), then the
// requested SHA is checked out exactly as the network path does. Falls back to
// the plain network Run on any mirror failure — a broken mirror must never
// fail a build that a fresh clone could serve.
func RunWithMirror(ctx context.Context, store *MirrorStore, opts Options) error {
	cloneURL := opts.CloneURL
	if cloneURL == "" {
		cloneURL = defaultCloneURL(opts.Repo)
	}
	mirror, err := store.Ensure(ctx, opts.Repo, cloneURL, opts.Token)
	if err != nil {
		log.Warn().Err(err).Str("repo", opts.Repo).
			Msg("checkout: mirror unavailable — falling back to network clone")
		return Run(ctx, opts)
	}

	local := opts
	local.CloneURL = mirror // local-path clone: object copy from disk
	local.Token = ""        // no auth for a local path
	if err := Run(ctx, local); err != nil {
		log.Warn().Err(err).Str("repo", opts.Repo).
			Msg("checkout: clone from mirror failed — falling back to network clone")
		_ = os.RemoveAll(opts.Path)
		return Run(ctx, opts)
	}
	return nil
}
