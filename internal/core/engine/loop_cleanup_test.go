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

	// One cleanup pass tears down the run's resources. cleanupFinishedRuns is
	// global (it cleans every terminal run needing cleanup), so scope assertions
	// to THIS run's id rather than the whole slice — the shared test DB holds
	// other tests' runs too.
	cleaner := &recordingCleaner{}
	loop := NewLoop(eng, ExecutorRegistry{"run": cleaner}, LoopConfig{})
	countRunID := func() int {
		n := 0
		for _, id := range cleaner.cleaned {
			if id == runID {
				n++
			}
		}
		return n
	}

	loop.cleanupFinishedRuns(ctx)
	assert.Equal(t, 1, countRunID(), "executor CleanupRun should be called once for the cancelled run")

	// cleaned_at is now set → a second pass is a no-op for this run (exactly-once).
	var cleanedAt *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT cleaned_at::text FROM pipeline_runs WHERE id = $1`, runID).Scan(&cleanedAt))
	assert.NotNil(t, cleanedAt, "cleaned_at should be stamped")

	loop.cleanupFinishedRuns(ctx)
	assert.Equal(t, 1, countRunID(), "cleanup must not repeat for this run once cleaned_at is set")
}
