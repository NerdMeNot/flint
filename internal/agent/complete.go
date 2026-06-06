package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	completeMaxRetries = 3
	completeBaseDelay  = 2 * time.Second
)

// completeActivity reports step result to the server via HTTP POST with
// exponential backoff retry. This is the most critical call in the agent —
// if it fails, the engine doesn't know the step finished and waits until
// the sweep timeout (2+ hours).
func completeActivity(cfg *Config, result *StepResult) error {
	if cfg.ServerURL == "" {
		return fmt.Errorf("agent: FLINT_SERVER_URL not set, cannot report completion")
	}

	body, err := json.Marshal(map[string]any{
		"taskToken": cfg.TaskToken,
		"result":    result,
	})
	if err != nil {
		return fmt.Errorf("agent: marshal completion request: %w", err)
	}

	url := strings.TrimRight(cfg.ServerURL, "/") + "/internal/complete"

	var lastErr error
	for attempt := 0; attempt <= completeMaxRetries; attempt++ {
		if attempt > 0 {
			delay := completeBaseDelay * time.Duration(1<<uint(attempt-1))
			log.Warn().Int("attempt", attempt+1).Dur("delay", delay).
				Msg("agent: retrying completion")
			time.Sleep(delay)
		}

		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("agent: build completion request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if cfg.InternalToken != "" {
			req.Header.Set("X-Flint-Internal-Token", cfg.InternalToken)
		}

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("agent: POST /internal/complete: %w", err)
			continue
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 200 {
			return nil
		}

		lastErr = fmt.Errorf("agent: completion returned %d: %s", resp.StatusCode, string(respBody))

		// Don't retry client errors (4xx) — they won't succeed on retry.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return lastErr
		}
	}

	return fmt.Errorf("agent: completion failed after %d attempts: %w", completeMaxRetries+1, lastErr)
}

// ReportError reports an agent-level error to the server.
func ReportError(cfg *Config, agentErr error) {
	completeWithError(cfg, agentErr)
}

// completeWithError reports an error back to the server.
func completeWithError(cfg *Config, agentErr error) {
	result := &StepResult{
		StepName: cfg.StepName,
		Success:  false,
		ExitCode: -1,
		Error:    agentErr.Error(),
	}

	if err := completeActivity(cfg, result); err != nil {
		log.Error().Err(err).Str("originalError", agentErr.Error()).
			Msg("agent: failed to report error to server after retries")
	}
}
