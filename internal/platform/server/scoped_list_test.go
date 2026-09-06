package server

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/platform/auth"
)

// A workspace-scoped subject must be able to list without naming a workspace.
//
// A policy scoped to "team-a" does not match a request scoped to "*", so
// GET /projects used to 403 for every scope-restricted user unless they passed
// ?workspace= by hand. That pressure is what the query-param override existed
// to relieve — and the override was an escalation. The route now asks whether
// the subject holds the permission anywhere, and narrows the rows instead.
func TestListProjects_ScopedUserGetsTheirWorkspaces(t *testing.T) {
	e, err := auth.NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy("alice@x.dev", testOrgID, "team-a", "*", auth.ObjProject, auth.ActRead)
	require.NoError(t, err)

	srv, m := enforcerServer(t, e)
	sessionFor(m, "alice-token", "alice@x.dev")

	// The handler must ask for exactly the workspaces alice holds.
	m.Querier.On("ListProjectsWithLastRun", mock.Anything, mock.MatchedBy(
		func(p db.ListProjectsWithLastRunParams) bool {
			return len(p.Workspaces) == 1 && p.Workspaces[0] == "team-a"
		})).Return([]db.ListProjectsWithLastRunRow{}, nil)
	m.Querier.On("GetOrg", mock.Anything).Return(db.GetOrgRow{ID: testOrgID}, nil).Maybe()
	m.Querier.On("ProjectHealthByOrg", mock.Anything, mock.Anything).
		Return([]db.ProjectHealthByOrgRow{}, nil).Maybe()

	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/projects", nil,
		ut.Header{Key: "Authorization", Value: "Bearer alice-token"})

	require.Equal(t, 200, w.Code, "a scoped user must be able to list their projects")
	m.Querier.AssertExpectations(t)
}

// The permitted set is a floor: an explicit ?workspace= can narrow it but never
// reach outside it.
//
// Asking for a workspace you do not hold is a filter that matches nothing, so
// it returns an empty page rather than 403 — the same answer every collection
// route gives when its filter excludes everything. What matters is the property
// underneath: no query runs, so none of team-b's rows can be returned.
func TestListProjects_QueryParamCannotWidenScope(t *testing.T) {
	e, err := auth.NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy("alice@x.dev", testOrgID, "team-a", "*", auth.ObjProject, auth.ActRead)
	require.NoError(t, err)

	srv, m := enforcerServer(t, e)
	sessionFor(m, "alice-token", "alice@x.dev")

	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/projects?workspace=team-b", nil,
		ut.Header{Key: "Authorization", Value: "Bearer alice-token"})

	require.Equal(t, 200, w.Code)
	require.NotContains(t, w.Body.String(), "team-b")
	m.Querier.AssertNotCalled(t, "ListProjectsWithLastRun", mock.Anything, mock.Anything)
}

// An unrestricted subject must not have any workspace filter imposed.
func TestListProjects_UnscopedUserIsUnfiltered(t *testing.T) {
	e, err := auth.NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy("root@x.dev", testOrgID, "*", "*", "*", "*")
	require.NoError(t, err)

	srv, m := enforcerServer(t, e)
	sessionFor(m, "root-token", "root@x.dev")

	m.Querier.On("ListProjectsWithLastRun", mock.Anything, mock.MatchedBy(
		func(p db.ListProjectsWithLastRunParams) bool {
			return len(p.Workspaces) == 0
		})).Return([]db.ListProjectsWithLastRunRow{}, nil)
	m.Querier.On("GetOrg", mock.Anything).Return(db.GetOrgRow{ID: testOrgID}, nil).Maybe()
	m.Querier.On("ProjectHealthByOrg", mock.Anything, mock.Anything).
		Return([]db.ProjectHealthByOrgRow{}, nil).Maybe()

	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/projects", nil,
		ut.Header{Key: "Authorization", Value: "Bearer root-token"})

	require.Equal(t, 200, w.Code)
	m.Querier.AssertExpectations(t)
}

// ?archived=true shares the /projects route, and therefore its authorization —
// which only established that the caller holds project:read *somewhere*. The
// archived branch has to apply the same narrowing, or authorizing scoped users
// for the list (which is the point of requireAnyScope) hands them every
// workspace's archived projects.
func TestListArchivedProjects_IsScopedToo(t *testing.T) {
	e, err := auth.NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy("alice@x.dev", testOrgID, "team-a", "*", auth.ObjProject, auth.ActRead)
	require.NoError(t, err)

	srv, m := enforcerServer(t, e)
	sessionFor(m, "alice-token", "alice@x.dev")

	m.Querier.On("ListArchivedProjects", mock.Anything, mock.MatchedBy(
		func(ws []string) bool {
			return len(ws) == 1 && ws[0] == "team-a"
		})).Return([]db.ListArchivedProjectsRow{}, nil)

	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/projects?archived=true", nil,
		ut.Header{Key: "Authorization", Value: "Bearer alice-token"})

	require.Equal(t, 200, w.Code)
	m.Querier.AssertExpectations(t)
}

// The runs list gets the same treatment.
func TestListRuns_ScopedUserGetsTheirWorkspaces(t *testing.T) {
	e, err := auth.NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy("alice@x.dev", testOrgID, "team-a", "*", auth.ObjRun, auth.ActRead)
	require.NoError(t, err)

	srv, m := enforcerServer(t, e)
	sessionFor(m, "alice-token", "alice@x.dev")

	m.Querier.On("ListRunsFiltered", mock.Anything, mock.MatchedBy(
		func(p db.ListRunsFilteredParams) bool {
			return len(p.Workspaces) == 1 && p.Workspaces[0] == "team-a"
		})).Return([]db.ListRunsFilteredRow{}, nil)

	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/runs", nil,
		ut.Header{Key: "Authorization", Value: "Bearer alice-token"})

	require.Equal(t, 200, w.Code)
	m.Querier.AssertExpectations(t)
}

// Holding the permission in no workspace at all is still a denial — the "any"
// check must not degrade into "allow everyone".
func TestListRuns_NoGrantIsStillDenied(t *testing.T) {
	e, err := auth.NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy("alice@x.dev", testOrgID, "team-a", "*", auth.ObjProject, auth.ActRead)
	require.NoError(t, err) // project:read, not run:read

	srv, m := enforcerServer(t, e)
	sessionFor(m, "alice-token", "alice@x.dev")

	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/runs", nil,
		ut.Header{Key: "Authorization", Value: "Bearer alice-token"})

	require.Equal(t, 403, w.Code)
	m.Querier.AssertNotCalled(t, "ListRunsFiltered", mock.Anything, mock.Anything)
}

// Gates are actions on runs, so the gate list is narrowed the same way. This
// route previously checked against "*" and simply 403'd every scoped user.
func TestListGates_IsScopedToo(t *testing.T) {
	e, err := auth.NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy("alice@x.dev", testOrgID, "team-a", "*", auth.ObjRun, auth.ActRead)
	require.NoError(t, err)

	srv, m := enforcerServer(t, e)
	sessionFor(m, "alice-token", "alice@x.dev")

	m.Querier.On("ListGatesByStatus", mock.Anything, mock.MatchedBy(
		func(p db.ListGatesByStatusParams) bool {
			return len(p.Workspaces) == 1 && p.Workspaces[0] == "team-a"
		})).Return([]db.ListGatesByStatusRow{}, nil)

	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/gates", nil,
		ut.Header{Key: "Authorization", Value: "Bearer alice-token"})

	require.Equal(t, 200, w.Code, "a scoped user must be able to see their gates")
	m.Querier.AssertExpectations(t)
}

// Search spans the same rows the lists serve. Without the restriction it would
// be a way to discover projects and runs the lists deliberately hide.
func TestSearch_IsScopedToo(t *testing.T) {
	e, err := auth.NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy("alice@x.dev", testOrgID, "team-a", "*", auth.ObjProject, auth.ActRead)
	require.NoError(t, err)

	srv, m := enforcerServer(t, e)
	sessionFor(m, "alice-token", "alice@x.dev")

	scopedTo := func(ws []string) bool { return len(ws) == 1 && ws[0] == "team-a" }
	m.Querier.On("SearchProjects", mock.Anything, mock.MatchedBy(
		func(p db.SearchProjectsParams) bool { return scopedTo(p.Workspaces) })).
		Return([]db.SearchProjectsRow{}, nil)
	m.Querier.On("SearchRuns", mock.Anything, mock.MatchedBy(
		func(p db.SearchRunsParams) bool { return scopedTo(p.Workspaces) })).
		Return([]db.SearchRunsRow{}, nil)

	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/search?q=pay", nil,
		ut.Header{Key: "Authorization", Value: "Bearer alice-token"})

	require.Equal(t, 200, w.Code)
	m.Querier.AssertExpectations(t)
}

// WorkspaceScope is the whole safety mechanism for collection routes, so its
// arithmetic is pinned directly.
func TestWorkspaceScope_Filter(t *testing.T) {
	unrestricted := WorkspaceScope{}
	require.Nil(t, unrestricted.Filter(nil), "no restriction means no filter")
	require.Equal(t, []string{"team-b"}, unrestricted.Filter([]string{"team-b"}),
		"an unrestricted caller still gets the filter they asked for")
	require.False(t, unrestricted.Empty(nil))

	restricted := WorkspaceScope{Restricted: true, Slugs: []string{"team-a", "team-c"}}
	require.Equal(t, []string{"team-a", "team-c"}, restricted.Filter(nil),
		"no explicit filter means the full permitted set")
	require.Equal(t, []string{"team-a"}, restricted.Filter([]string{"team-a", "team-b"}),
		"an explicit filter is intersected, never unioned")
	require.True(t, restricted.Empty([]string{"team-b"}),
		"asking only for a workspace outside the grant selects nothing")
	require.False(t, restricted.Empty(nil))
}
