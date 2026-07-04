package engine

import (
	"strings"
)

// claimedStep holds the data needed to dispatch a step after claiming.
type claimedStep struct {
	id            string
	workflowID    string
	name          string
	execType      string
	attempt       int
	taskToken     string
	stepDef       []byte
	runID         string
	orgID         string
	projectID     string
	repo          string
	ref           string
	commitSHA     string
	triggerType   string
	environment   string            // target environment for secret scoping
	pipelineImage string            // pipeline-level default image (fallback)
	env           map[string]string // merged env vars (org env_vars + step.env + input.Env)
	secretMapping map[string]string // env var name → secret store name (from step YAML secrets:)
	// needsOutputs carries the outputs of this step's direct dependencies
	// (base job name → outputs) for the in-container needs.* expression context.
	needsOutputs map[string]map[string]string
}

// extractMatrixKey returns the matrix key portion from an expanded step name.
// "test[node=16,os=ubuntu]" → "node=16,os=ubuntu". Returns "" for non-matrix steps.
func extractMatrixKey(name string) string {
	start := strings.Index(name, "[")
	end := strings.LastIndex(name, "]")
	if start >= 0 && end > start {
		return name[start+1 : end]
	}
	return ""
}
