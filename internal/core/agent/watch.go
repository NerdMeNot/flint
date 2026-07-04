package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NerdMeNot/flint/pkg/artifact"
	pkgcache "github.com/NerdMeNot/flint/pkg/cache"
	"github.com/NerdMeNot/flint/pkg/logsink"
	"github.com/NerdMeNot/flint/pkg/workspace"
	"github.com/rs/zerolog/log"
)

// Watch is the sidecar mode. It:
//  1. Streams stdout/stderr to the LogSink in real-time as the step runs
//  2. Waits for the step container to complete (polls exit marker file)
//  3. Reads emit outputs
//  4. Reports completion to the server via HTTP POST /internal/complete
func Watch(ctx context.Context, cfg *Config, sink logsink.LogSink) error {
	log.Info().
		Str("runID", cfg.RunID).
		Str("step", cfg.StepName).
		Dur("timeout", cfg.StepTimeout).
		Msg("agent watch started")

	ctx, cancel := context.WithTimeout(ctx, cfg.StepTimeout)
	defer cancel()

	logRef := logsink.LogRef{
		OrgID:    cfg.OrgID,
		RunID:    cfg.RunID,
		StepName: cfg.StepName,
	}

	logDone := make(chan struct{})
	go func() {
		defer close(logDone)
		streamLogsRealTime(ctx, sink, logRef, cfg.Workspace)
	}()

	exitCode, err := waitForCompletion(ctx, cfg.Workspace)
	if err != nil {
		if ctx.Err() != nil {
			log.Error().Dur("timeout", cfg.StepTimeout).Str("step", cfg.StepName).Msg("step timed out")
			completeWithError(cfg, fmt.Errorf("step %s timed out after %s", cfg.StepName, cfg.StepTimeout))
			return err
		}
		log.Error().Err(err).Msg("failed to wait for step completion")
		completeWithError(cfg, fmt.Errorf("agent: failed to detect step completion: %w", err))
		return err
	}

	cancel()
	select {
	case <-logDone:
	case <-time.After(2 * time.Second):
		log.Warn().Msg("log streaming did not finish in time")
	}

	log.Info().Int("exitCode", exitCode).Str("step", cfg.StepName).Msg("step completed")

	outputs, err := ReadEmits(cfg.Workspace)
	if err != nil {
		log.Warn().Err(err).Msg("failed to read emit outputs")
	}

	// Clean up agent internal files so they don't leak into the next step
	// when using a shared volume (PVC mode).
	cleanupAgentFiles(cfg.Workspace)

	result := &StepResult{
		StepName: cfg.StepName,
		Success:  exitCode == 0,
		ExitCode: exitCode,
		Outputs:  outputs,
	}
	if exitCode != 0 {
		result.Error = fmt.Sprintf("step exited with code %d", exitCode)
	}

	// On success: sync workspace out, upload artifacts, save cache.
	if exitCode == 0 {
		// Workspace sync.
		ws, wsErr := workspace.New(
			cfg.WorkspaceMode, cfg.WorkspaceAddr, cfg.WorkspaceToken,
			cfg.S3Bucket, cfg.S3Region, cfg.RunID,
		)
		if wsErr != nil {
			log.Warn().Err(wsErr).Msg("agent: workspace init failed (skipping sync)")
		}
		if ws != nil {
			defer ws.Close()
			if syncErr := ws.SyncOut(ctx, cfg.Workspace); syncErr != nil {
				log.Warn().Err(syncErr).Msg("agent: workspace SyncOut failed (continuing)")
			}
		}

		// Upload artifacts.
		if cfg.S3Bucket != "" && len(cfg.ArtifactOutputs) > 0 {
			store := artifact.NewS3Store(cfg.S3Bucket, cfg.S3Region)
			for _, output := range cfg.ArtifactOutputs {
				ref := artifact.Ref{
					OrgID: cfg.OrgID, RunID: cfg.RunID,
					StepName: cfg.StepName, Name: output.Path,
				}
				if err := store.Upload(ctx, ref, output.Path); err != nil {
					log.Warn().Err(err).Str("path", output.Path).Msg("agent: failed to upload artifact")
				}
			}
		}

		// Save cache.
		if cfg.S3Bucket != "" && cfg.CacheKey != "" && len(cfg.CachePaths) > 0 {
			cacheStore := pkgcache.NewS3(cfg.OrgID, cfg.ProjectID, cfg.S3Bucket, cfg.S3Region)
			key, keyErr := EvaluateCacheKey(cfg)
			if keyErr != nil {
				log.Warn().Err(keyErr).Msg("agent: failed to evaluate cache key (skipping save)")
			} else if err := cacheStore.Save(ctx, cfg.Workspace, key, cfg.CachePaths); err != nil {
				log.Warn().Err(err).Msg("agent: failed to save cache")
			}
		}
	}

	// Report to server via HTTP — no Temporal.
	if err := completeActivity(cfg, result); err != nil {
		log.Error().Err(err).Msg("failed to complete step")
		return err
	}

	log.Info().
		Str("step", cfg.StepName).
		Bool("success", result.Success).
		Int("outputs", len(outputs)).
		Msg("agent watch completed")
	return nil
}

func streamLogsRealTime(ctx context.Context, sink logsink.LogSink, ref logsink.LogRef, workspace string) {
	logPath := fmt.Sprintf("%s/.flint-step.log", workspace)

	var f *os.File
	for {
		var err error
		f, err = os.Open(logPath)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var batch []logsink.LogLine

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		for scanner.Scan() {
			batch = append(batch, logsink.LogLine{
				Timestamp: time.Now(),
				Stream:    "stdout",
				Content:   scanner.Text(),
			})
		}

		if len(batch) > 0 {
			if err := sink.Write(ctx, ref, batch); err != nil {
				log.Warn().Err(err).Int("lines", len(batch)).Msg("failed to write log batch")
			}
			batch = batch[:0]
		}

		select {
		case <-ctx.Done():
			for scanner.Scan() {
				batch = append(batch, logsink.LogLine{
					Timestamp: time.Now(),
					Stream:    "stdout",
					Content:   scanner.Text(),
				})
			}
			if len(batch) > 0 {
				_ = sink.Write(context.Background(), ref, batch)
			}
			return
		case <-ticker.C:
		}
	}
}

func waitForCompletion(ctx context.Context, workspace string) (int, error) {
	exitFile := fmt.Sprintf("%s/.flint-exit", workspace)

	// A stat every 100ms is ~free and shaves ~400ms average off every step's
	// completion latency vs the old 500ms tick.
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return -1, fmt.Errorf("timed out waiting for step completion: %w", ctx.Err())
		case <-ticker.C:
			data, err := os.ReadFile(exitFile)
			if err != nil {
				continue
			}
			var code int
			if _, err := fmt.Sscanf(string(data), "%d", &code); err != nil {
				log.Warn().Str("content", string(data)).Err(err).Msg("invalid exit code, treating as failure")
				return 1, nil
			}
			return code, nil
		}
	}
}

// cleanupAgentFiles removes internal agent files from the workspace so they
// don't leak into the next step (shared PVC) or into workspace sync-out —
// including the steps-driver binary and any per-sub-step output files.
func cleanupAgentFiles(workspace string) {
	for _, name := range []string{".flint-emit", ".flint-exit", ".flint-step.log", InitDoneFile} {
		_ = os.Remove(fmt.Sprintf("%s/%s", workspace, name))
	}
	_ = os.RemoveAll(fmt.Sprintf("%s/.flint-bin", workspace))
	if matches, err := filepath.Glob(fmt.Sprintf("%s/.flint-output-*", workspace)); err == nil {
		for _, m := range matches {
			_ = os.Remove(m)
		}
	}
}
