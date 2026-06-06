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
//
// SecretMapping maps env var names to secret store names. For example,
// {"API_KEY": "api-key-name"} means: fetch the secret named "api-key-name"
// and expose it as the environment variable API_KEY.
func FetchSecrets(ctx context.Context, cfg *Config) error {
	if len(cfg.SecretMapping) == 0 {
		return nil
	}

	if cfg.ServerURL == "" {
		log.Warn().Msg("agent: no server URL configured, skipping secret fetching")
		return nil
	}

	// Collect unique secret store names to request.
	storeNames := make([]string, 0, len(cfg.SecretMapping))
	seen := make(map[string]bool)
	for _, storeName := range cfg.SecretMapping {
		if !seen[storeName] {
			storeNames = append(storeNames, storeName)
			seen[storeName] = true
		}
	}

	namesJSON, _ := json.Marshal(storeNames)

	url := strings.TrimRight(cfg.ServerURL, "/") + "/internal/secrets"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("agent: failed to create secrets request: %w", err)
	}

	// The signed task token determines the secret scope server-side; org/project/
	// environment headers are no longer sent (they were spoofable and are ignored).
	req.Header.Set("X-Flint-Task-Token", cfg.TaskToken)
	req.Header.Set("X-Flint-Internal-Token", cfg.InternalToken)
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

	// Re-map: inject as the env var name from the mapping, not the store name.
	injected := 0
	for envVarName, storeName := range cfg.SecretMapping {
		if val, ok := result.Secrets[storeName]; ok {
			if err := os.Setenv(envVarName, val); err != nil {
				log.Warn().Str("secret", envVarName).Err(err).Msg("agent: failed to set secret env var")
			} else {
				injected++
			}
		}
	}

	log.Info().Int("count", injected).Msg("agent: secrets injected")
	return nil
}
