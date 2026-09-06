package server

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/mock"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/NerdMeNot/flint/internal/products/ci"
	"github.com/NerdMeNot/flint/internal/testutil"
)

// testServer wires a server with a REAL (in-memory) Casbin enforcer granting the
// test subject full access.
//
// It used to leave Enforcer nil, and requirePermission called straight through
// when it was — so every handler test ran with authorization disabled and none
// of them exercised the middleware they nominally sat behind. Now that a missing
// enforcer denies, the tests need a real one, which is the honest arrangement:
// the handler under test is reached the same way a request reaches it in
// production.
// testUserEmail is the subject every handler test authenticates as.
const testUserEmail = "test@example.com"

func testServer(t *testing.T) (*Server, *testutil.Mocks) {
	t.Helper()
	m := testutil.NewMocks(t)
	enforcer, err := auth.NewMemoryEnforcer()
	require.NoError(t, err)
	if _, err := enforcer.AddPolicy(testUserEmail, testOrgID, "*", "*", "*", "*"); err != nil {
		require.NoError(t, err)
	}
	// Scope resolution now actually runs for run/gate routes — with the enforcer
	// missing it was skipped entirely, so these tests never reached it. Permissive
	// by default; tests that care about scoping override it.
	m.Querier.On("GetRunScope", mock.Anything, mock.Anything).
		Return(db.GetRunScopeRow{WorkspaceSlug: "*", Environment: "*"}, nil).Maybe()

	deps := Deps{
		Enforcer:     enforcer,
		Config:       testutil.TestConfig(),
		DB:           m.Pool,
		Q:            m.Querier,
		Engine:       m.Engine,
		Forge:        m.Forge,
		Secrets:      m.Secrets,
		Logs:         m.Logs,
		LogBroadcast: m.LogStream,
		Sessions:     m.Sessions,
		// Wire the CI product as the composition root would, so handlers that
		// create runs (webhook / manual / retry) exercise the real service.
		Runs: ci.NewService(m.Engine, m.Forge, m.Querier),
	}
	srv := New(deps)
	return srv, m
}

func jsonBody(t *testing.T, v any) *ut.Body {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return &ut.Body{Body: bytes.NewReader(b), Len: len(b)}
}

func parseJSON(t *testing.T, rec *ut.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	return result
}
