package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func listEnvs(srv *Server, query string) *ut.ResponseRecorder {
	return ut.PerformRequest(srv.Engine(), "GET", "/api/v1/environments"+query, nil,
		ut.Header{Key: "Authorization", Value: "Bearer test-jwt-token"})
}

// TestPagination_OffsetCursor proves limit is honoured, a full page yields a
// nextCursor, and passing that cursor advances the query offset.
func TestPagination_OffsetCursor(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)
	m.Querier.On("GetOrg", mock.Anything).Return(db.GetOrgRow{ID: "org-1"}, nil).Maybe()

	// First page: limit=2, offset=0 → return a full page of 2.
	m.Querier.On("ListEnvironments", mock.Anything, db.ListEnvironmentsParams{
		OrgID: "org-1", Limit: 2, Offset: 0,
	}).Return([]db.ListEnvironmentsRow{
		{ID: "e1", Name: "alpha", Slug: "alpha", CreatedAt: time.Now()},
		{ID: "e2", Name: "beta", Slug: "beta", CreatedAt: time.Now()},
	}, nil)

	w := listEnvs(srv, "?limit=2")
	require.Equal(t, 200, w.Code)
	var page1 struct {
		Items      []map[string]any `json:"items"`
		NextCursor string           `json:"nextCursor"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page1))
	require.Len(t, page1.Items, 2)
	require.NotEmpty(t, page1.NextCursor, "a full page yields a next cursor")

	// Second page: the cursor must advance the offset to 2 and return a short
	// page → no further cursor.
	m.Querier.On("ListEnvironments", mock.Anything, db.ListEnvironmentsParams{
		OrgID: "org-1", Limit: 2, Offset: 2,
	}).Return([]db.ListEnvironmentsRow{
		{ID: "e3", Name: "gamma", Slug: "gamma", CreatedAt: time.Now()},
	}, nil)

	w2 := listEnvs(srv, "?limit=2&cursor="+page1.NextCursor)
	require.Equal(t, 200, w2.Code)
	var page2 struct {
		Items      []map[string]any `json:"items"`
		NextCursor string           `json:"nextCursor"`
	}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &page2))
	require.Len(t, page2.Items, 1)
	assert.Empty(t, page2.NextCursor, "a short page is the last page")
}

func TestNextOffsetCursor(t *testing.T) {
	// Full page → cursor present and decodes to the next offset.
	cur := nextOffsetCursor(0, 25, 25)
	require.NotEmpty(t, cur)
	id, sv, err := decodeCursor(cur)
	require.NoError(t, err)
	assert.Equal(t, "offset", id)
	assert.Equal(t, "25", sv)

	// Short page → no cursor.
	assert.Empty(t, nextOffsetCursor(0, 25, 10))
}
