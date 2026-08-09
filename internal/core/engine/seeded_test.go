package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// TestStartWorkflowSeeded verifies that seeded steps are inserted already-succeeded
// with their carried-over outputs, that those outputs land in the workflow's
// step_outputs, and that only the non-seeded steps run.
func TestStartWorkflowSeeded(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)
	eng := New(pool, nil)
	defer eng.Close()
	q := db.New(pool)

	waves := [][]pipeline.Step{
		{{Name: "build", Image: "alpine:3.19", Run: pipeline.Cmd("echo")}},
		{{Name: "test", Image: "alpine:3.19", Run: pipeline.Cmd("echo"), DependsOn: []string{"build"}}},
		{{Name: "deploy", Image: "alpine:3.19", Run: pipeline.Cmd("echo"), DependsOn: []string{"test"}}},
	}
	// Seed build as already-succeeded with an output.
	seed := map[string]StepResult{
		"build": {StepName: "build", Success: true, Outputs: map[string]string{"artifact": "app.tar"}},
	}
	wfID, err := eng.StartWorkflowSeeded(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID, TriggerType: "retry",
	}, waves, seed)
	require.NoError(t, err)

	status := func(name string) string {
		var s string
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT status FROM steps WHERE workflow_id=$1 AND name=$2 ORDER BY attempt DESC LIMIT 1",
			wfID, name).Scan(&s))
		return s
	}

	// build seeded succeeded; test released (dep satisfied); deploy still pending.
	assert.Equal(t, "succeeded", status("build"), "seeded step is pre-succeeded")
	assert.Equal(t, "queued", status("test"), "downstream of seeded step is released")
	assert.Equal(t, "pending", status("deploy"))

	// Carried-over outputs are in the workflow's step_outputs.
	raw, err := q.GetWorkflowStepOutputs(ctx, wfID)
	require.NoError(t, err)
	var outs map[string]StepResult
	require.NoError(t, json.Unmarshal(raw, &outs))
	assert.Equal(t, "app.tar", outs["build"].Outputs["artifact"])

	// A 'seeded' history event was recorded for build.
	events, err := q.ListEngineEventsByStep(ctx, db.ListEngineEventsByStepParams{
		WorkflowID: wfID, StepName: strptr("build"),
	})
	require.NoError(t, err)
	var sawSeeded bool
	for _, e := range events {
		if e.EventType == "seeded" {
			sawSeeded = true
		}
	}
	assert.True(t, sawSeeded, "expected a 'seeded' event for build")
}

func strptr(s string) *string { return &s }
