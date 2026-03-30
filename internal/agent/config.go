// Package agent implements the flint-agent lifecycle — the sidecar binary
// injected into every pipeline step K8s Job.
//
// The agent has two modes:
//   - init: clone repo into shared workspace (runs as init container)
//   - watch: monitor step container, stream logs, report completion (runs as sidecar)
//
// All configuration comes from environment variables injected by the worker
// at Job creation time — the agent never reads a config file.
package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Config holds agent configuration, sourced entirely from environment variables.
type Config struct {
	// Engine
	TaskToken string // base64-encoded engine task token

	// Identity
	RunID    string
	StepName string
	OrgID    string

	// Git
	GitRepo string // e.g., "acme/skills-service"
	GitRef  string // branch or tag
	GitSHA  string // full commit SHA

	// Paths
	Workspace string // mount path, typically /workspace

	// Server
	ServerURL string // internal flint-server URL for secret fetching

	// Secrets
	SecretNames []string // secrets to fetch and inject as env vars

	// Log sink
	LogSinkMode string // "filesystem" or "s3"
	S3Bucket    string
	S3Region    string
	FSLogPath   string

	// Timeouts
	StepTimeout  time.Duration // max time to wait for step completion
	CloneTimeout time.Duration // max time for git clone
}

// LoadFromEnv reads all agent config from environment variables.
func LoadFromEnv() (*Config, error) {
	cfg := &Config{
		TaskToken:         os.Getenv("FLINT_TASK_TOKEN"),
		RunID:             os.Getenv("FLINT_RUN_ID"),
		StepName:          os.Getenv("FLINT_STEP_NAME"),
		OrgID:             os.Getenv("FLINT_ORG_ID"),
		GitRepo:           os.Getenv("FLINT_GIT_REPO"),
		GitRef:            os.Getenv("FLINT_GIT_REF"),
		GitSHA:            os.Getenv("FLINT_GIT_SHA"),
		Workspace:         os.Getenv("FLINT_WORKSPACE"),
		ServerURL:         os.Getenv("FLINT_SERVER_URL"),
		LogSinkMode:       os.Getenv("FLINT_LOG_SINK"),
		S3Bucket:          os.Getenv("FLINT_S3_BUCKET"),
		S3Region:          os.Getenv("FLINT_S3_REGION"),
		FSLogPath:         os.Getenv("FLINT_FS_LOG_PATH"),
	}

	// SecretNames are JSON-encoded by the worker (e.g., ["SECRET_A","SECRET_B"]).
	if raw := os.Getenv("FLINT_SECRET_NAMES"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg.SecretNames); err != nil {
			return nil, fmt.Errorf("agent: FLINT_SECRET_NAMES is not valid JSON: %w", err)
		}
	}

	// Parse timeouts with defaults.
	cfg.StepTimeout = parseDurationEnv("FLINT_STEP_TIMEOUT", 2*time.Hour)
	cfg.CloneTimeout = parseDurationEnv("FLINT_CLONE_TIMEOUT", 10*time.Minute)

	if cfg.Workspace == "" {
		cfg.Workspace = "/workspace"
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	var missing []string
	if c.TaskToken == "" {
		missing = append(missing, "FLINT_TASK_TOKEN")
	}
	if c.RunID == "" {
		missing = append(missing, "FLINT_RUN_ID")
	}
	if c.StepName == "" {
		missing = append(missing, "FLINT_STEP_NAME")
	}
	if c.GitRepo == "" {
		missing = append(missing, "FLINT_GIT_REPO")
	}
	if len(missing) > 0 {
		return fmt.Errorf("agent: missing required env vars: %s", strings.Join(missing, ", "))
	}
	return nil
}

// Logger returns a zerolog.Logger pre-configured with the agent's correlation fields.
// Use this instead of bare log.Info() for consistent structured logging.
func (c *Config) Logger() zerolog.Logger {
	return log.With().
		Str("runID", c.RunID).
		Str("step", c.StepName).
		Str("orgID", c.OrgID).
		Logger()
}

func parseDurationEnv(key string, defaultVal time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return defaultVal
	}
	return d
}
