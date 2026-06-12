package engine

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingCleaner is a StepExecutor that also implements stepCleaner, recording
// the runIDs it was asked to clean up.
type recordingCleaner struct{ cleaned []string }

func (*recordingCleaner) Kind() string                                          { return "recording" }
func (*recordingCleaner) Dispatch(context.Context, claimedStep) (string, error) { return "", nil }
func (r *recordingCleaner) CleanupRun(_ context.Context, runID string) error {
	r.cleaned = append(r.cleaned, runID)
	return nil
}

// TestLoop_CancelTriggersCleanup is the regression guard for cancel-doesn't-stop-
// pods: cancelling a workflow must mark the run cancelled and make it eligible
// for prompt, exactly-once executor cleanup (workspace pod + Jobs torn down).
func TestLoop_CancelTriggersCleanup(t *testing.T) {
	pool := internalTestDB(t)
	ctx := context.Background()
	runID := uuid.NewString()
	orgID, projectID := seedRun(t, pool, runID)

	eng := New(pool, nil)
	defer eng.Close()

	waves := wavesSingle("a")
	wfID, err := eng.StartWorkflowWithWaves(ctx, StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projectID,
		Repo: "acme/test", Ref: "main", CommitSHA: "abc123",
		TriggerType: "manual", TriggeredBy: "test",
	}, waves)
	require.NoError(t, err)

	require.NoError(t, eng.CancelWorkflow(ctx, wfID))

	// Run status must be cancelled (previously left stale).
	var runStatus string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status FROM pipeline_runs WHERE id = $1`, runID).Scan(&runStatus))
	assert.Equal(t, "cancelled", runStatus)

	// One cleanup pass tears down the run's resources exactly once.
	cleaner := &recordingCleaner{}
	loop := NewLoop(eng, ExecutorRegistry{"run": cleaner}, LoopConfig{})
	loop.cleanupFinishedRuns(ctx)

	assert.Equal(t, []string{runID}, cleaner.cleaned, "executor CleanupRun should be called for the cancelled run")

	// cleaned_at is now set → a second pass is a no-op (exactly-once).
	var cleanedAt *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT cleaned_at::text FROM pipeline_runs WHERE id = $1`, runID).Scan(&cleanedAt))
	assert.NotNil(t, cleanedAt, "cleaned_at should be stamped")

	loop.cleanupFinishedRuns(ctx)
	assert.Equal(t, []string{runID}, cleaner.cleaned, "cleanup must not repeat once cleaned_at is set")
}
