package engine

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// TaskToken identifies a specific step execution for async completion.
// Encoded as base64(JSON) and passed to the agent via FLINT_TASK_TOKEN env var.
// No crypto — travels only on internal network. Validated by UNIQUE(workflow_id, name, attempt).
type TaskToken struct {
	WorkflowID string `json:"w"`
	StepName   string `json:"s"`
	Attempt    int    `json:"a"`
}

// EncodeTaskToken serializes a task token to a base64 string.
func EncodeTaskToken(t TaskToken) string {
	b, _ := json.Marshal(t)
	return base64.StdEncoding.EncodeToString(b)
}

// DecodeTaskToken deserializes a base64 task token string.
func DecodeTaskToken(s string) (TaskToken, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return TaskToken{}, fmt.Errorf("engine: invalid task token encoding: %w", err)
	}
	var t TaskToken
	if err := json.Unmarshal(b, &t); err != nil {
		return TaskToken{}, fmt.Errorf("engine: invalid task token payload: %w", err)
	}
	if t.WorkflowID == "" || t.StepName == "" {
		return TaskToken{}, fmt.Errorf("engine: task token missing required fields")
	}
	return t, nil
}
