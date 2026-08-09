package workflows

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/testutil/mocks"
)

func TestNextCron(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	next, err := NextCron("*/5 * * * *", base)
	require.NoError(t, err)
	assert.Equal(t, base.Add(5*time.Minute), next)

	_, err = NextCron("not-a-cron", base)
	require.Error(t, err)
}

func dueSchedule() db.ListDueWorkflowSchedulesRow {
	return db.ListDueWorkflowSchedulesRow{
		ID:         "sched-1",
		OrgID:      "org-1",
		Name:       "nightly",
		Cron:       "*/5 * * * *",
		Definition: "name: w\nsteps:\n  - name: a\n    run: echo a\n",
	}
}

func TestScheduler_FiresDueSchedule(t *testing.T) {
	q := new(mocks.Querier)
	eng := new(mocks.Engine)

	q.On("ListDueWorkflowSchedules", mock.Anything).Return([]db.ListDueWorkflowSchedulesRow{dueSchedule()}, nil)
	q.On("AdvanceWorkflowScheduleIfDue", mock.Anything, mock.MatchedBy(func(p db.AdvanceWorkflowScheduleIfDueParams) bool {
		return p.ID == "sched-1" && p.NextRunAt.After(time.Now())
	})).Return(int64(1), nil) // we won the claim
	q.On("InsertWorkflowRun", mock.Anything, mock.MatchedBy(func(p db.InsertWorkflowRunParams) bool {
		return p.OrgID == "org-1" && p.TriggerType == "schedule"
	})).Return(nil)
	eng.On("StartWorkflowWithWaves", mock.Anything,
		mock.MatchedBy(func(in engine.StartWorkflowInput) bool { return in.OrgID == "org-1" && in.Kind == "workflow" }),
		mock.Anything,
	).Return("wf-1", nil)

	NewScheduler(q, eng).tick(context.Background())

	q.AssertExpectations(t)
	eng.AssertExpectations(t)
}

func TestScheduler_SkipsWhenClaimLost(t *testing.T) {
	q := new(mocks.Querier)
	eng := new(mocks.Engine)

	q.On("ListDueWorkflowSchedules", mock.Anything).Return([]db.ListDueWorkflowSchedulesRow{dueSchedule()}, nil)
	q.On("AdvanceWorkflowScheduleIfDue", mock.Anything, mock.Anything).Return(int64(0), nil) // another worker claimed it

	NewScheduler(q, eng).tick(context.Background())

	// Claim lost → no run created, engine never called.
	q.AssertNotCalled(t, "InsertWorkflowRun", mock.Anything, mock.Anything)
	eng.AssertNotCalled(t, "StartWorkflowWithWaves", mock.Anything, mock.Anything, mock.Anything)
}
