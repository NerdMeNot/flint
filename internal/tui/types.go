// Package tui provides the interactive terminal interface for Flint.
package tui

import "time"

// Project represents a Flint project from the API.
type Project struct {
	ID            string   `json:"id"`
	RepoPath      string   `json:"repoPath"`
	DisplayName   string   `json:"displayName"`
	Description   *string  `json:"description"`
	Colour        string   `json:"colour"`
	Tags          []string `json:"tags"`
	DefaultBranch string   `json:"defaultBranch"`
}

// Run represents a pipeline run from the API.
type Run struct {
	ID            string  `json:"id"`
	ProjectID     string  `json:"projectID,omitempty"`
	WorkflowFile  string  `json:"workflowFile"`
	TriggerType   string  `json:"triggerType"`
	TriggerRef    *string `json:"triggerRef"`
	CommitSHA     *string `json:"commitSHA"`
	CommitMessage *string `json:"commitMessage"`
	TriggeredBy   *string `json:"triggeredBy"`
	Status        string  `json:"status"`
	StartedAt     string  `json:"startedAt"`
	FinishedAt    *string `json:"finishedAt"`
	DurationMs    *int    `json:"durationMs"`
	WorkflowID    *string `json:"workflowID,omitempty"`
}

// StepState represents a step within a workflow.
type StepState struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	ExecType string `json:"execType"`
	Wave     int    `json:"wave"`
	Attempt  int    `json:"attempt"`
	ExitCode *int   `json:"exitCode,omitempty"`
	Error    string `json:"error,omitempty"`
}

// WorkflowState is the full state of a workflow execution.
// JSON tags match the server's /runs/:id/steps envelope (workflowId, runId).
type WorkflowState struct {
	WorkflowID string      `json:"workflowId"`
	RunID      string      `json:"runId"`
	Status     string      `json:"status"`
	Steps      []StepState `json:"steps"`
	StartedAt  *time.Time  `json:"startedAt"`
	FinishedAt *time.Time  `json:"finishedAt"`
}

// Runner represents an available runner pool.
type Runner struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	CPU         string  `json:"cpu"`
	Memory      string  `json:"memory"`
	Arch        string  `json:"arch"`
	Spot        bool    `json:"spot"`
	GPU         *GPU    `json:"gpu,omitempty"`
}

// GPU represents GPU requirements.
type GPU struct {
	Vendor string `json:"vendor"`
	Model  string `json:"model"`
	Count  *int   `json:"count"`
}

// Org represents a Flint organization.
type Org struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// PendingGate represents a gate step waiting for approval. JSON tags match the
// server's GET /gates items.
type PendingGate struct {
	RunID    string `json:"runId"`
	StepName string `json:"stepName"`
	Message  string `json:"message"`
	Project  string `json:"projectName"`
	Branch   string `json:"branch"`
}
