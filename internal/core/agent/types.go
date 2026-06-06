package agent

// StepResult is the result reported back to Temporal via CompleteActivity.
// This mirrors activities.StepResult — Temporal serialization handles the mapping.
type StepResult struct {
	StepName string            `json:"stepName"`
	Success  bool              `json:"success"`
	ExitCode int               `json:"exitCode"`
	Outputs  map[string]string `json:"outputs,omitempty"`
	Error    string            `json:"error,omitempty"`
}
