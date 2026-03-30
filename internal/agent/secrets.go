package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/rs/zerolog/log"
)

// FetchSecrets fetches declared secrets from the Flint server and injects
// them as environment variables into the current process (for the step container).
func FetchSecrets(ctx context.Context, cfg *Config) error {
	if len(cfg.SecretNames) == 0 {
		return nil
	}

	if cfg.ServerURL == "" {
		log.Warn().Msg("agent: no server URL configured, skipping secret fetching")
		return nil
	}

	namesJSON, _ := json.Marshal(cfg.SecretNames)

	url := strings.TrimRight(cfg.ServerURL, "/") + "/internal/secrets"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("agent: failed to create secrets request: %w", err)
	}

	req.Header.Set("X-Flint-Org-ID", cfg.OrgID)
	req.Header.Set("X-Flint-Secret-Names", string(namesJSON))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("agent: failed to fetch secrets: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agent: secrets endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Secrets map[string]string `json:"secrets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("agent: failed to decode secrets response: %w", err)
	}

	// Inject secrets as environment variables.
	for name, value := range result.Secrets {
		if err := os.Setenv(name, value); err != nil {
			log.Warn().Str("secret", name).Err(err).Msg("agent: failed to set secret env var")
		}
	}

	log.Info().Int("count", len(result.Secrets)).Msg("agent: secrets injected")
	return nil
}
