package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/NerdMeNot/flint/pkg/artifact"
	pkgcache "github.com/NerdMeNot/flint/pkg/cache"
	"github.com/NerdMeNot/flint/pkg/workspace"
	"github.com/rs/zerolog/log"
)

// InitDoneFile is the marker the sidecar writes once workspace preparation is
// complete. The step container's start is gated on it via a startup probe, so
// a single agent container covers both the init phase and the watch phase —
// one fewer container (and one fewer resource request) per step pod.
const InitDoneFile = ".flint-init-done"

// Init prepares the workspace before the step container runs: driver install
// (group steps), secrets, workspace sync-in, artifact downloads, cache restore.
func Init(ctx context.Context, cfg *Config) error {
	// Group ("steps") jobs run the flint-agent steps driver INSIDE the user's
	// image — copy this (static) binary into the shared workspace so the step
	// container can exec it.
	if os.Getenv("FLINT_EXEC_TYPE") == "steps" {
		if err := InstallDriver(cfg.Workspace); err != nil {
			return fmt.Errorf("install steps driver: %w", err)
		}
	}

	// Fetch secrets (needed before checkout for git tokens, AWS creds, etc).
	if err := FetchSecrets(ctx, cfg); err != nil {
		log.Warn().Err(err).Msg("failed to fetch secrets (continuing)")
	}

	// Workspace sync: pull previous step's state if available.
	ws, wsErr := workspace.New(
		cfg.WorkspaceMode, cfg.WorkspaceAddr, cfg.WorkspaceToken,
		cfg.S3Bucket, cfg.S3Region, cfg.RunID,
	)
	if wsErr != nil {
		log.Warn().Err(wsErr).Msg("agent: failed to init workspace (skipping sync)")
	}
	if ws != nil {
		defer ws.Close()
		if _, syncErr := ws.SyncIn(ctx, cfg.Workspace); syncErr != nil {
			log.Warn().Err(syncErr).Msg("agent: SyncIn failed (continuing)")
		}
	}

	// Download artifact inputs from upstream steps.
	if cfg.S3Bucket != "" && len(cfg.ArtifactInputs) > 0 {
		store := artifact.NewS3Store(cfg.S3Bucket, cfg.S3Region)
		for _, input := range cfg.ArtifactInputs {
			ref := artifact.Ref{
				OrgID: cfg.OrgID, RunID: cfg.RunID,
				StepName: input.From, Name: input.Path,
			}
			if err := store.Download(ctx, ref, input.Path); err != nil {
				log.Warn().Err(err).Str("from", input.From).Msg("failed to download artifact (continuing)")
			}
		}
	}

	// Restore cache (always S3-backed for cross-run persistence). On an
	// exact-key miss, restoreKeys prefixes fall back to the newest close match.
	if cfg.S3Bucket != "" && cfg.CacheKey != "" {
		cacheStore := pkgcache.NewS3(cfg.OrgID, cfg.ProjectID, cfg.S3Bucket, cfg.S3Region)
		key, keyErr := EvaluateCacheKey(cfg)
		if keyErr != nil {
			log.Warn().Err(keyErr).Msg("agent: failed to evaluate cache key (skipping)")
		} else {
			if _, err := cacheStore.RestoreWithFallback(ctx, cfg.Workspace, key, cfg.CacheRestoreKeys, cfg.CachePaths); err != nil {
				log.Warn().Err(err).Msg("failed to restore cache (continuing)")
			}
		}
	}

	return nil
}

// MarkInitDone writes the marker file the step container's startup probe waits
// on.
func MarkInitDone(workspace string) error {
	return os.WriteFile(filepath.Join(workspace, InitDoneFile), []byte("ok"), 0o644)
}
