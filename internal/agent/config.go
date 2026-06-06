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

	// Project/environment context (for scoped secret lookups)
	ProjectID   string // project owning this pipeline
	Environment string // target environment (e.g. "production")

	// Matrix
	MatrixKey string // e.g. "node=16,os=ubuntu" (empty for non-matrix steps)

	// Secrets
	SecretNames   []string          // legacy: flat list of secret names
	SecretMapping map[string]string // env var name → secret store name

	// Artifacts (JSON deserialized from FLINT_ARTIFACT_INPUTS/OUTPUTS env vars)
	ArtifactInputs  []ArtifactInput  `json:"artifactInputs"`
	ArtifactOutputs []ArtifactOutput `json:"artifactOutputs"`

	// Cache
	CacheKey   string   // unevaluated expression (e.g. "npm-${{ hashFiles('package-lock.json') }}")
	CachePaths []string // paths to cache (e.g. ["/workspace/node_modules"])

	// Workspace configuration.
	// WorkspaceMode controls how workspace sync works:
	//   "agent" (default) — gRPC workspace agent pod
	//   "s3"              — S3-backed sync (bucket/region from S3Bucket/S3Region)
	//   "pvc"             — shared PVC, no sync needed
	WorkspaceMode  string
	WorkspaceAddr  string // gRPC address (agent mode only)
	WorkspaceToken string // per-run auth token (agent mode only)

	// Internal auth — shared secret for /internal endpoints.
	InternalToken string

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
// ArtifactInput declares an artifact to download before step execution.
type ArtifactInput struct {
	From string `json:"from"` // source step name
	Path string `json:"path"` // local path / artifact name
}

// ArtifactOutput declares an artifact to upload after step execution.
type ArtifactOutput struct {
	Path string `json:"path"` // local path to compress
}

func LoadFromEnv() (*Config, error) {
	cfg := &Config{
		TaskToken:      os.Getenv("FLINT_TASK_TOKEN"),
		RunID:          os.Getenv("FLINT_RUN_ID"),
		StepName:       os.Getenv("FLINT_STEP_NAME"),
		OrgID:          os.Getenv("FLINT_ORG_ID"),
		GitRepo:        os.Getenv("FLINT_GIT_REPO"),
		GitRef:         os.Getenv("FLINT_GIT_REF"),
		GitSHA:         os.Getenv("FLINT_GIT_SHA"),
		Workspace:      os.Getenv("FLINT_WORKSPACE"),
		ServerURL:      os.Getenv("FLINT_SERVER_URL"),
		ProjectID:      os.Getenv("FLINT_PROJECT_ID"),
		Environment:    os.Getenv("FLINT_ENVIRONMENT"),
		MatrixKey:      os.Getenv("FLINT_MATRIX_KEY"),
		WorkspaceMode:  os.Getenv("FLINT_WS_MODE"), // "agent", "s3", "pvc", or empty (= agent)
		WorkspaceAddr:  os.Getenv("FLINT_WS_ADDR"),
		WorkspaceToken: os.Getenv("FLINT_WS_TOKEN"),
		InternalToken:  os.Getenv("FLINT_INTERNAL_TOKEN"),
		LogSinkMode:    os.Getenv("FLINT_LOG_SINK"),
		S3Bucket:       os.Getenv("FLINT_S3_BUCKET"),
		S3Region:       os.Getenv("FLINT_S3_REGION"),
		FSLogPath:      os.Getenv("FLINT_FS_LOG_PATH"),
	}

	// SecretNames are JSON-encoded by the worker (e.g., ["SECRET_A","SECRET_B"]).
	if raw := os.Getenv("FLINT_SECRET_NAMES"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg.SecretNames); err != nil {
			return nil, fmt.Errorf("agent: FLINT_SECRET_NAMES is not valid JSON: %w", err)
		}
	}

	// SecretMapping maps env var names to secret store names (e.g., {"API_KEY":"api-key-name"}).
	if raw := os.Getenv("FLINT_SECRET_MAPPING"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg.SecretMapping); err != nil {
			return nil, fmt.Errorf("agent: FLINT_SECRET_MAPPING is not valid JSON: %w", err)
		}
	}

	// Backward compat: convert legacy FLINT_SECRET_NAMES to identity mapping.
	if len(cfg.SecretMapping) == 0 && len(cfg.SecretNames) > 0 {
		cfg.SecretMapping = make(map[string]string, len(cfg.SecretNames))
		for _, name := range cfg.SecretNames {
			cfg.SecretMapping[name] = name
		}
	}

	// Artifact inputs/outputs (JSON arrays set by dispatch).
	if raw := os.Getenv("FLINT_ARTIFACT_INPUTS"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &cfg.ArtifactInputs)
	}
	if raw := os.Getenv("FLINT_ARTIFACT_OUTPUTS"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &cfg.ArtifactOutputs)
	}

	// Cache config.
	cfg.CacheKey = os.Getenv("FLINT_CACHE_KEY")
	if raw := os.Getenv("FLINT_CACHE_PATHS"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &cfg.CachePaths)
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
