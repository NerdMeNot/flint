package server

import (
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/db"
)

func rawBody(s string) *ut.Body {
	return &ut.Body{Body: strings.NewReader(s), Len: len(s)}
}

func createRole(srv *Server, body string) *ut.ResponseRecorder {
	return ut.PerformRequest(srv.Engine(), "POST", "/api/v1/roles",
		rawBody(body),
		ut.Header{Key: "Authorization", Value: "Bearer test-jwt-token"},
		ut.Header{Key: "Content-Type", Value: "application/json"})
}

// A role that restricts itself to a workspace but grants admin permissions is
// refused at definition time.
//
// Admin permissions are emitted platform-wide because the resources behind them
// (secrets, runners, connections, roles) have no workspace column — there is
// nothing to scope them against. Accepting the definition produced a role that
// reads as "admin of team-a" and behaves as "admin of everything"; refusing it
// is the only version that does not lie.
func TestCreateRole_RejectsScopedAdminPermissions(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	w := createRole(srv, `{
		"name": "Team A Admin", "slug": "team-a-admin",
		"permissions": [{"object":"secret","action":"manage"}],
		"workspaces": ["team-a"]
	}`)

	require.Equal(t, 400, w.Code, "a workspace-scoped admin role must be refused")
	require.Contains(t, w.Body.String(), "secret")
	m.Querier.AssertNotCalled(t, "CreateRole", mock.Anything, mock.Anything)
}

// The CI half of the model genuinely is scopable, so scoping it is allowed.
func TestCreateRole_AllowsScopedCIPermissions(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	m.Querier.On("GetOrg", mock.Anything).Return(db.GetOrgRow{ID: testOrgID}, nil)
	m.Querier.On("CreateRole", mock.Anything, mock.Anything).Return("role-1", nil)
	m.Querier.On("InsertRolePermission", mock.Anything, mock.Anything).Return(nil)
	m.Querier.On("InsertRoleWorkspaceScope", mock.Anything, mock.Anything).Return(nil)

	w := createRole(srv, `{
		"name": "Team A Dev", "slug": "team-a-dev",
		"permissions": [{"object":"project","action":"read"}],
		"workspaces": ["team-a"]
	}`)

	require.Equal(t, 201, w.Code)
}

// An unscoped admin role is the supported way to express it, and stays legal.
func TestCreateRole_AllowsUnscopedAdminPermissions(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	m.Querier.On("GetOrg", mock.Anything).Return(db.GetOrgRow{ID: testOrgID}, nil)
	m.Querier.On("CreateRole", mock.Anything, mock.Anything).Return("role-2", nil)
	m.Querier.On("InsertRolePermission", mock.Anything, mock.Anything).Return(nil)

	w := createRole(srv, `{
		"name": "Secrets Admin", "slug": "secrets-admin",
		"permissions": [{"object":"secret","action":"manage"}]
	}`)

	require.Equal(t, 201, w.Code)
}

// The same rule has to hold on update, evaluated against the merged result —
// adding a scope to a role that already carries admin permissions is the same
// mistake, arriving one request later.
func TestUpdateRole_RejectsAddingScopeToAnAdminRole(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	m.Querier.On("GetRoleByID", mock.Anything, "role-3").
		Return(db.GetRoleByIDRow{ID: "role-3", Slug: "ops", IsSystem: false}, nil)
	// Stored permissions include an admin object; the request only sends a scope.
	m.Querier.On("ListRolePermissions", mock.Anything, "role-3").
		Return([]db.ListRolePermissionsRow{{Object: "runner", Action: "manage"}}, nil)
	m.Querier.On("ListRoleEnvironmentNames", mock.Anything, "role-3").
		Return([]string{}, nil).Maybe()

	body := `{"workspaces": ["team-a"]}`
	w := ut.PerformRequest(srv.Engine(), "PATCH", "/api/v1/roles/role-3",
		rawBody(body),
		ut.Header{Key: "Authorization", Value: "Bearer test-jwt-token"},
		ut.Header{Key: "Content-Type", Value: "application/json"})

	require.Equal(t, 400, w.Code, "the merged definition must be validated, not just the sent fields")
	m.Querier.AssertNotCalled(t, "DeleteRoleWorkspaceScopes", mock.Anything, mock.Anything)
}
