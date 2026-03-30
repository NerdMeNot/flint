package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
)

// completeActivity reports step result to the server via HTTP POST.
// Replaces Temporal's CompleteActivity — the agent no longer dials Temporal.
func completeActivity(cfg *Config, result *StepResult) error {
	if cfg.ServerURL == "" {
		return fmt.Errorf("agent: FLINT_SERVER_URL not set, cannot report completion")
	}

	body, err := json.Marshal(map[string]any{
		"taskToken": cfg.TaskToken, // already base64-encoded
		"result":    result,
	})
	if err != nil {
		return fmt.Errorf("agent: marshal completion request: %w", err)
	}

	url := strings.TrimRight(cfg.ServerURL, "/") + "/internal/complete"
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("agent: POST /internal/complete failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agent: completion returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
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
			Msg("agent: failed to report error to server")
	}
}
