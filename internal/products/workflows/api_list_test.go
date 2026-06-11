package workflows

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/internal/testutil/mocks"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestTriggerRun_AcceptsJSONEnvelope proves the trigger accepts the web client's
// {"definition": "<yaml>"} envelope, not only a raw YAML body.
func TestTriggerRun_AcceptsJSONEnvelope(t *testing.T) {
	eng := new(mocks.Engine)
	q := new(mocks.Querier)
	q.On("InsertWorkflowRun", mock.Anything, mock.Anything).Return(nil)
	eng.On("StartWorkflowWithWaves", mock.Anything, mock.Anything, mock.Anything).
		Return("wf-1", nil)

	api := NewAPI(eng, q)
	c := app.NewContext(0)
	c.Request.SetBody([]byte(`{"definition":"name: demo\nsteps:\n  - name: a\n    run: echo a\n"}`))

	api.triggerRun(observe.WithOrgID(context.Background(), "org-1"), c)

	assert.Equal(t, 202, c.Response.StatusCode())
}

func TestListRuns_PaginatesAndMaps(t *testing.T) {
	q := new(mocks.Querier)
	started := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	triggeredBy := "api"
	q.On("ListWorkflowRuns", mock.Anything, mock.MatchedBy(func(p db.ListWorkflowRunsParams) bool {
		return p.OrgID == "org-1" && p.Lim == 2
	})).Return([]db.ListWorkflowRunsRow{
		{ID: "r1", Status: "running", TriggerType: "manual", TriggeredBy: &triggeredBy, StartedAt: started},
		{ID: "r2", Status: "succeeded", TriggerType: "manual", StartedAt: started, DurationMs: pgtype.Int4{Int32: 1200, Valid: true}},
	}, nil)

	api := NewAPI(new(mocks.Engine), q)
	c := app.NewContext(0)
	c.Request.SetRequestURI("/api/v1/workflows/runs?limit=2")

	api.listRuns(observe.WithOrgID(context.Background(), "org-1"), c)

	require.Equal(t, 200, c.Response.StatusCode())
	var resp struct {
		Items []struct {
			RunID  string `json:"runId"`
			Status string `json:"status"`
		} `json:"items"`
		NextCursor string `json:"nextCursor"`
	}
	require.NoError(t, json.Unmarshal(c.Response.Body(), &resp))
	require.Len(t, resp.Items, 2)
	assert.Equal(t, "r1", resp.Items[0].RunID)
	// A full page (len == limit) yields a next cursor.
	assert.NotEmpty(t, resp.NextCursor)
}

func TestListRuns_RequiresOrg(t *testing.T) {
	api := NewAPI(new(mocks.Engine), new(mocks.Querier))
	c := app.NewContext(0)
	api.listRuns(context.Background(), c)
	assert.Equal(t, 401, c.Response.StatusCode())
}

func TestExtractDefinition(t *testing.T) {
	raw := []byte("name: demo\nsteps: []\n")
	assert.Equal(t, raw, extractDefinition(raw), "raw YAML passes through")
	assert.Equal(t, []byte("name: demo"),
		extractDefinition([]byte(`{"definition":"name: demo"}`)), "JSON envelope is unwrapped")
}

func TestRunCursorRoundTrip(t *testing.T) {
	ts, id := "2026-06-01T10:00:00Z", "r-123"
	gotTs, gotID := decodeRunCursor(encodeRunCursor(ts, id))
	assert.Equal(t, ts, gotTs)
	assert.Equal(t, id, gotID)
	// Garbage decodes to empty (first page).
	e, f := decodeRunCursor("not-base64!!")
	assert.Empty(t, e)
	assert.Empty(t, f)
}
