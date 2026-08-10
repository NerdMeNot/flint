package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/NerdMeNot/flint/internal/testutil"
)

// enforcerServer builds a test server wired with a real (in-memory) Casbin
// enforcer so requirePermission actually evaluates policies instead of falling
// back to allow-all.
func enforcerServer(t *testing.T, e casbin.IEnforcer) (*Server, *testutil.Mocks) {
	t.Helper()
	m := testutil.NewMocks(t)
	deps := Deps{
		Config:   testutil.TestConfig(),
		DB:       m.Pool,
		Q:        m.Querier,
		Engine:   m.Engine,
		Forge:    m.Forge,
		Secrets:  m.Secrets,
		Logs:     m.Logs,
		Sessions: m.Sessions,
		Enforcer: e,
	}
	return New(deps), m
}

// sessionFor makes the mock sessions resolve a bearer token to the given email.
func sessionFor(m *testutil.Mocks, token, email string) {
	m.Sessions.On("ValidateSession", token).Return(&auth.Claims{
		Subject: email, Email: email, OrgID: "org-1", Provider: "oidc",
	}, nil)
}

// runScope makes GetRunScope resolve a run to a (workspace, environment) pair.
func runScope(m *testutil.Mocks, runID, ws, env string) {
	m.Querier.On("GetRunScope", mock.Anything, runID).
		Return(db.GetRunScopeRow{WorkspaceSlug: ws, Environment: env}, nil)
}

// passingRunSteps wires the minimal mocks so GET /runs/:id/steps returns 200,
// letting an authorized request reach (and pass) the handler.
func passingRunSteps(m *testutil.Mocks, runID string) {
	wfID := "wf-" + runID
	m.Querier.On("GetRunWorkflowID", mock.Anything, runID).Return(&wfID, nil)
	now := time.Now()
	m.Engine.On("QueryWorkflow", mock.Anything, wfID).Return(&engine.WorkflowState{
		WorkflowID: wfID, RunID: runID, Status: "running", StartedAt: &now,
		Steps: []engine.StepState{{Name: "build", Status: "succeeded", ExecType: "run", Wave: 1}},
	}, nil)
	m.Querier.On("GetWorkflowDAGWaves", mock.Anything, wfID).
		Return([]byte(json.RawMessage(`[[1]]`)), nil)
}

func getSteps(srv *Server, runID, token string) int {
	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/runs/"+runID+"/steps", nil,
		ut.Header{Key: "Authorization", Value: "Bearer " + token})
	return w.Code
}

// TestRequirePermission_WorkspaceScope proves a workspace-scoped role is allowed
// on runs in its workspace and denied on runs in another — previously broken
// because resolveWorkspace never resolved a workspace for /runs/:id.
func TestRequirePermission_WorkspaceScope(t *testing.T) {
	e, err := auth.NewMemoryEnforcer()
	require.NoError(t, err)
	// alice may read runs only in workspace "team-a" (any environment).
	_, err = e.AddPolicy("alice@x.dev", "team-a", "*", auth.ObjRun, auth.ActRead)
	require.NoError(t, err)

	srv, m := enforcerServer(t, e)
	sessionFor(m, "alice-token", "alice@x.dev")
	runScope(m, "run-a", "team-a", "")
	runScope(m, "run-b", "team-b", "")
	passingRunSteps(m, "run-a")

	require.Equal(t, 200, getSteps(srv, "run-a", "alice-token"), "run in team-a should be allowed")
	require.Equal(t, 403, getSteps(srv, "run-b", "alice-token"), "run in team-b should be forbidden")
}

// TestRequirePermission_EnvironmentScope proves the environment dimension is
// actually enforced — previously dead because the middleware passed a hardcoded
// "*" for env, so an env-scoped grant could never be matched or violated.
func TestRequirePermission_EnvironmentScope(t *testing.T) {
	e, err := auth.NewMemoryEnforcer()
	require.NoError(t, err)
	// bob may read runs only in the "production" environment (any workspace).
	_, err = e.AddPolicy("bob@x.dev", "*", "production", auth.ObjRun, auth.ActRead)
	require.NoError(t, err)

	srv, m := enforcerServer(t, e)
	sessionFor(m, "bob-token", "bob@x.dev")
	runScope(m, "run-prod", "team-a", "production")
	runScope(m, "run-stg", "team-a", "staging")
	passingRunSteps(m, "run-prod")

	require.Equal(t, 200, getSteps(srv, "run-prod", "bob-token"), "production run should be allowed")
	require.Equal(t, 403, getSteps(srv, "run-stg", "bob-token"), "staging run should be forbidden")
}

// A server with no enforcer must deny, not call through.
//
// requirePermission used to return c.Next(ctx) when Enforcer was nil, under a
// comment about falling back to a legacy role check. There is no such fallback:
// every permission check on every route passed. Boot fails hard if the enforcer
// cannot be built, so this was not reachable in a real server — but it was
// reachable in the test suite, which is why every handler test ran with
// authorization silently disabled.
func TestRequirePermission_DeniesWithoutAnEnforcer(t *testing.T) {
	srv, m := enforcerServer(t, nil)
	sessionFor(m, "tok", "anyone@example.com")

	code := getSteps(srv, "run-1", "tok")

	require.Equal(t, 403, code, "no enforcer means no authorization, so the request is denied")
}
