package server

import (
	"encoding/json"
	"testing"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestHandleStats_RealNumbers proves the stats endpoint reports computed values
// (it was previously a hardcoded all-zeros stub).
func TestHandleStats_RealNumbers(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	m.Querier.On("GetOrg", mock.Anything).Return(db.GetOrgRow{ID: "org-1"}, nil)
	m.Querier.On("GetRunStats", mock.Anything, "org-1").Return(db.GetRunStatsRow{
		TotalRuns: 10, SuccessRuns: 7, RunsToday: 3, AvgDurationMs: 1500,
	}, nil)
	m.Querier.On("CountActiveProjects", mock.Anything).Return(int64(4), nil)
	m.Querier.On("CountPendingGates", mock.Anything).Return(int64(2), nil)

	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/stats", nil,
		ut.Header{Key: "Authorization", Value: "Bearer test-jwt-token"})
	require.Equal(t, 200, w.Code)

	var resp struct {
		TotalRuns      int64   `json:"totalRuns"`
		SuccessRate    float64 `json:"successRate"`
		PendingGates   int64   `json:"pendingGates"`
		ActiveProjects int64   `json:"activeProjects"`
		RunsToday      int64   `json:"runsToday"`
		AvgDuration    string  `json:"avgDuration"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(10), resp.TotalRuns)
	assert.InDelta(t, 0.7, resp.SuccessRate, 0.001)
	assert.Equal(t, int64(2), resp.PendingGates)
	assert.Equal(t, int64(4), resp.ActiveProjects)
	assert.Equal(t, int64(3), resp.RunsToday)
	assert.Equal(t, "1.5s", resp.AvgDuration)
}
