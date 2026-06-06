package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setRequiredEnv sets the minimum required env vars for LoadFromEnv to succeed.
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("FLINT_TASK_TOKEN", "tok-abc123")
	t.Setenv("FLINT_RUN_ID", "run-001")
	t.Setenv("FLINT_STEP_NAME", "build")
	t.Setenv("FLINT_GIT_REPO", "acme/app")
}

func TestLoadFromEnv_AllRequiredFields(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("FLINT_SERVER_URL", "http://server:8080")
	t.Setenv("FLINT_ORG_ID", "org-1")
	t.Setenv("FLINT_GIT_REF", "refs/heads/main")
	t.Setenv("FLINT_GIT_SHA", "abc123def456")

	cfg, err := LoadFromEnv()
	require.NoError(t, err)

	assert.Equal(t, "tok-abc123", cfg.TaskToken)
	assert.Equal(t, "run-001", cfg.RunID)
	assert.Equal(t, "build", cfg.StepName)
	assert.Equal(t, "acme/app", cfg.GitRepo)
	assert.Equal(t, "http://server:8080", cfg.ServerURL)
	assert.Equal(t, "org-1", cfg.OrgID)
	assert.Equal(t, "refs/heads/main", cfg.GitRef)
	assert.Equal(t, "abc123def456", cfg.GitSHA)
}

func TestLoadFromEnv_MissingTaskToken(t *testing.T) {
	// Set everything except FLINT_TASK_TOKEN.
	t.Setenv("FLINT_RUN_ID", "run-001")
	t.Setenv("FLINT_STEP_NAME", "build")
	t.Setenv("FLINT_GIT_REPO", "acme/app")

	_, err := LoadFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FLINT_TASK_TOKEN")
}

func TestLoadFromEnv_MissingRunID(t *testing.T) {
	t.Setenv("FLINT_TASK_TOKEN", "tok-abc123")
	t.Setenv("FLINT_STEP_NAME", "build")
	t.Setenv("FLINT_GIT_REPO", "acme/app")

	_, err := LoadFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FLINT_RUN_ID")
}

func TestLoadFromEnv_MissingMultipleFields(t *testing.T) {
	// Only set one required field.
	t.Setenv("FLINT_TASK_TOKEN", "tok-abc123")

	_, err := LoadFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FLINT_RUN_ID")
	assert.Contains(t, err.Error(), "FLINT_STEP_NAME")
	assert.Contains(t, err.Error(), "FLINT_GIT_REPO")
}

func TestLoadFromEnv_DefaultWorkspace(t *testing.T) {
	setRequiredEnv(t)
	// FLINT_WORKSPACE not set — should default to /workspace.

	cfg, err := LoadFromEnv()
	require.NoError(t, err)
	assert.Equal(t, "/workspace", cfg.Workspace)
}

func TestLoadFromEnv_CustomWorkspace(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("FLINT_WORKSPACE", "/custom/path")

	cfg, err := LoadFromEnv()
	require.NoError(t, err)
	assert.Equal(t, "/custom/path", cfg.Workspace)
}

func TestLoadFromEnv_SecretMapping(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("FLINT_SECRET_MAPPING", `{"API_KEY":"api-key-prod","DB_PASS":"db-password"}`)

	cfg, err := LoadFromEnv()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"API_KEY": "api-key-prod",
		"DB_PASS": "db-password",
	}, cfg.SecretMapping)
}

func TestLoadFromEnv_SecretMappingInvalidJSON(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("FLINT_SECRET_MAPPING", `{not valid json}`)

	_, err := LoadFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FLINT_SECRET_MAPPING")
}

func TestLoadFromEnv_SecretNamesBackwardCompat(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("FLINT_SECRET_NAMES", `["SECRET_A","SECRET_B"]`)

	cfg, err := LoadFromEnv()
	require.NoError(t, err)

	// Legacy SecretNames should be converted to identity mapping.
	assert.Equal(t, []string{"SECRET_A", "SECRET_B"}, cfg.SecretNames)
	assert.Equal(t, map[string]string{
		"SECRET_A": "SECRET_A",
		"SECRET_B": "SECRET_B",
	}, cfg.SecretMapping)
}

func TestLoadFromEnv_SecretMappingTakesPrecedenceOverNames(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("FLINT_SECRET_NAMES", `["SECRET_A"]`)
	t.Setenv("FLINT_SECRET_MAPPING", `{"API_KEY":"api-key-prod"}`)

	cfg, err := LoadFromEnv()
	require.NoError(t, err)

	// SecretMapping is present, so SecretNames should NOT override it.
	assert.Equal(t, map[string]string{"API_KEY": "api-key-prod"}, cfg.SecretMapping)
}

func TestLoadFromEnv_SecretNamesInvalidJSON(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("FLINT_SECRET_NAMES", `not json`)

	_, err := LoadFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FLINT_SECRET_NAMES")
}

func TestLoadFromEnv_DefaultTimeouts(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := LoadFromEnv()
	require.NoError(t, err)
	assert.Equal(t, 2*time.Hour, cfg.StepTimeout)
	assert.Equal(t, 10*time.Minute, cfg.CloneTimeout)
}

func TestLoadFromEnv_CustomTimeouts(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("FLINT_STEP_TIMEOUT", "30m")
	t.Setenv("FLINT_CLONE_TIMEOUT", "5m")

	cfg, err := LoadFromEnv()
	require.NoError(t, err)
	assert.Equal(t, 30*time.Minute, cfg.StepTimeout)
	assert.Equal(t, 5*time.Minute, cfg.CloneTimeout)
}

func TestLoadFromEnv_InvalidTimeoutUsesDefault(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("FLINT_STEP_TIMEOUT", "not-a-duration")

	cfg, err := LoadFromEnv()
	require.NoError(t, err)
	assert.Equal(t, 2*time.Hour, cfg.StepTimeout)
}

func TestLoadFromEnv_ArtifactInputsOutputs(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("FLINT_ARTIFACT_INPUTS", `[{"from":"build","path":"dist/app.tar.gz"}]`)
	t.Setenv("FLINT_ARTIFACT_OUTPUTS", `[{"path":"coverage/report.html"}]`)

	cfg, err := LoadFromEnv()
	require.NoError(t, err)

	require.Len(t, cfg.ArtifactInputs, 1)
	assert.Equal(t, "build", cfg.ArtifactInputs[0].From)
	assert.Equal(t, "dist/app.tar.gz", cfg.ArtifactInputs[0].Path)

	require.Len(t, cfg.ArtifactOutputs, 1)
	assert.Equal(t, "coverage/report.html", cfg.ArtifactOutputs[0].Path)
}

func TestLoadFromEnv_CacheConfig(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("FLINT_CACHE_KEY", "npm-${{ hashFiles('package-lock.json') }}")
	t.Setenv("FLINT_CACHE_PATHS", `["/workspace/node_modules","/workspace/.cache"]`)

	cfg, err := LoadFromEnv()
	require.NoError(t, err)
	assert.Equal(t, "npm-${{ hashFiles('package-lock.json') }}", cfg.CacheKey)
	assert.Equal(t, []string{"/workspace/node_modules", "/workspace/.cache"}, cfg.CachePaths)
}
