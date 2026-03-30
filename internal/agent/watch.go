package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/NerdMeNot/flint/pkg/logsink"
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

	result := &StepResult{
		StepName: cfg.StepName,
		Success:  exitCode == 0,
		ExitCode: exitCode,
		Outputs:  outputs,
	}
	if exitCode != 0 {
		result.Error = fmt.Sprintf("step exited with code %d", exitCode)
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

	ticker := time.NewTicker(500 * time.Millisecond)
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
