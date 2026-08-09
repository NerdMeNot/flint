package server

import (
	"encoding/json"
	"math"
	"time"

	pgtype "github.com/jackc/pgx/v5/pgtype"
)

type lastRunResponse struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Branch      string `json:"branch"`
	Duration    string `json:"duration"`
	TriggeredBy string `json:"triggeredBy"`
	StartedAt   string `json:"startedAt"`
}

type projectResponse struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	Repo           string           `json:"repo"`
	Workspace      string           `json:"workspace"`
	Colour         string           `json:"colour"`
	Tags           []string         `json:"tags"`
	PipelineCount  int              `json:"pipelineCount"`
	PipelineErrors int              `json:"pipelineErrors"`
	LastRun        *lastRunResponse `json:"lastRun,omitempty"`
	Health         *projectHealth   `json:"health,omitempty"`
	CreatedAt      string           `json:"createdAt"`
	Inferred       bool             `json:"inferred"`
}

// projectHealth summarizes a project's recent run outcomes for the dashboard
// health bars / "needs attention" (UI ProjectHealth). passRate is a percent.
type projectHealth struct {
	RecentRuns []string `json:"recentRuns"`
	PassRate   int      `json:"passRate"`
	FailingNow bool     `json:"failingNow"`
	TotalRuns  int      `json:"totalRuns"`
}

// buildProjectHealth assembles a projectHealth from recent statuses (newest
// first) and totals. Returns nil when the project has no runs.
func buildProjectHealth(recent []string, total, succeeded int64) *projectHealth {
	if total == 0 {
		return nil
	}
	if recent == nil {
		recent = []string{}
	}
	h := &projectHealth{
		RecentRuns: recent,
		PassRate:   int(math.Round(float64(succeeded) / float64(total) * 100)),
		TotalRuns:  int(total),
	}
	if len(recent) > 0 {
		h.FailingNow = recent[0] == "failed"
	}
	return h
}

type runResponse struct {
	ID            string  `json:"id"`
	ProjectID     string  `json:"projectId"`
	ProjectName   string  `json:"projectName"`
	ProjectColour string  `json:"projectColour"`
	Repo          string  `json:"repo"`
	Status        string  `json:"status"`
	TriggerType   string  `json:"triggerType"`
	Branch        string  `json:"branch"`
	CommitSha     string  `json:"commitSha"`
	CommitMessage string  `json:"commitMessage"`
	TriggeredBy   string  `json:"triggeredBy"`
	WorkflowFile  string  `json:"workflowFile"`
	Duration      string  `json:"duration"`
	StartedAt     string  `json:"startedAt"`
	FinishedAt    *string `json:"finishedAt,omitempty"`
	// Epoch-ms variants the UI uses for time-range filtering and adaptive
	// timestamp rendering (alongside the RFC3339 strings above).
	StartedAtTs  int64            `json:"startedAtTs"`
	FinishedAtTs *int64           `json:"finishedAtTs,omitempty"`
	Environment  *string          `json:"environment,omitempty"`
	ErrorMessage *string          `json:"errorMessage,omitempty"`
	Steps        []runStepSummary `json:"steps,omitempty"`
}

// runStepSummary is the compact per-step shape the run feed renders as stage
// pips (UI RunStepSummary).
type runStepSummary struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type runnerResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Provider    string  `json:"provider"`
	CPU         string  `json:"cpu"`
	Memory      string  `json:"memory"`
	Disk        *string `json:"disk,omitempty"`
	Arch        string  `json:"arch"`
	GPUVendor   *string `json:"gpuVendor,omitempty"`
	GPUModel    *string `json:"gpuModel,omitempty"`
	GPUCount    *int32  `json:"gpuCount,omitempty"`
	// Provider allow-lists narrowing what Quote may offer (elastic pools).
	InstanceTypes []string `json:"instanceTypes,omitempty"`
	Regions       []string `json:"regions,omitempty"`
	// Economics policy — surfaced so the pool editor can round-trip it and the
	// UI can show the speed/cost tradeoff the pool encodes.
	CapacityType   string          `json:"capacityType"`
	Objective      string          `json:"objective"`
	MinWarm        int32           `json:"minWarm"`
	MaxMachines    int32           `json:"maxMachines"`
	IdleTTLSeconds int32           `json:"idleTtlSeconds"`
	Overrides      json.RawMessage `json:"overrides,omitempty"`
	HourlyCost     *float64        `json:"hourlyCost,omitempty"`
	IsDefault      bool            `json:"isDefault"`
	Ready          bool            `json:"ready"`
	CreatedAt      string          `json:"createdAt"`
}

type teamMemberResponse struct {
	ID    string  `json:"id"`
	Email string  `json:"email"`
	Name  *string `json:"name,omitempty"`
}

type teamWithMembersResponse struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Slug        string               `json:"slug"`
	Source      string               `json:"source"`
	IDPGroup    *string              `json:"idpGroup,omitempty"`
	MemberCount int                  `json:"memberCount"`
	Members     []teamMemberResponse `json:"members"`
}

// decodeStepSummaries parses the json_agg step array from the run queries.
func decodeStepSummaries(b []byte) []runStepSummary {
	if len(b) == 0 {
		return nil
	}
	var out []runStepSummary
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

// durationFromInt4 formats a nullable duration_ms column as a human string.
func durationFromInt4(d pgtype.Int4) string {
	if !d.Valid {
		return "0s"
	}
	return formatDuration(d.Int32)
}

func isYAMLFile(name string) bool {
	return len(name) > 5 && (name[len(name)-5:] == ".yaml" || name[len(name)-4:] == ".yml")
}

// ── Runs ──────────────────────────────────────────────────────

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func formatDuration(ms int32) string {
	d := time.Duration(ms) * time.Millisecond
	if d < time.Second {
		return "0s"
	}
	if d < time.Minute {
		return d.Truncate(time.Second).String()
	}
	return d.Truncate(time.Second).String()
}

func formatTimePtrOpt(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}
