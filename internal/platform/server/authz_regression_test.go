package server

import (
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/platform/auth"
)

// A workspace-scoped subject must not be able to reach another workspace's run
// by naming a workspace they *do* hold in the query string.
//
// resolveScope used to apply ?workspace= unconditionally, after deriving the
// real workspace from the run. The derived value describes the resource the
// handler is about to act on; the query param is whatever the caller typed.
// Letting the caller's value win meant the check passed against a workspace the
// request was not touching, and the handler then served the run anyway.
func TestScopeOverride_CannotReachAnotherWorkspace(t *testing.T) {
	e, err := auth.NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy("alice@x.dev", testOrgID, "team-a", "*", auth.ObjRun, auth.ActRead)
	require.NoError(t, err)

	srv, m := enforcerServer(t, e)
	sessionFor(m, "alice-token", "alice@x.dev")
	runScope(m, "run-b", "team-b", "")

	w := ut.PerformRequest(srv.Engine(), "GET",
		"/api/v1/runs/run-b/steps?workspace=team-a", nil,
		ut.Header{Key: "Authorization", Value: "Bearer alice-token"})

	require.Equal(t, 403, w.Code,
		"a query param must not replace the workspace derived from the run")
}

// The environment dimension has the same hole and the same fix.
func TestScopeOverride_CannotReachAnotherEnvironment(t *testing.T) {
	e, err := auth.NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy("bob@x.dev", testOrgID, "*", "staging", auth.ObjRun, auth.ActRead)
	require.NoError(t, err)

	srv, m := enforcerServer(t, e)
	sessionFor(m, "bob-token", "bob@x.dev")
	runScope(m, "run-prod", "team-a", "production")

	w := ut.PerformRequest(srv.Engine(), "GET",
		"/api/v1/runs/run-prod/steps?environment=staging", nil,
		ut.Header{Key: "Authorization", Value: "Bearer bob-token"})

	require.Equal(t, 403, w.Code,
		"a query param must not replace the environment derived from the run")
}

// An API key's policies are written against "apikey:<id>". Enforcement used to
// run against claims.Email, which a key does not have — so the check compared
// the empty string against every policy and denied every request a key made,
// however privileged the key was.
func TestAPIKey_IsAuthorizedAsItsPrincipal(t *testing.T) {
	e, err := auth.NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy(auth.APIKeyPrincipal("key-1"), testOrgID, "*", "*", "*", "*")
	require.NoError(t, err)

	srv, m := enforcerServer(t, e)

	raw := "flint_k_secret"
	m.Querier.On("GetAPIKeyByHash", mock.Anything, auth.HashToken(raw)).
		Return(db.GetAPIKeyByHashRow{
			ID: "key-1", OrgID: testOrgID, Name: "ci-bot", Scopes: []string{},
		}, nil)
	m.Querier.On("TouchAPIKey", mock.Anything, "key-1").Return(nil).Maybe()
	runScope(m, "run-a", "team-a", "")
	passingRunSteps(m, "run-a")

	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/runs/run-a/steps", nil,
		ut.Header{Key: "X-API-Key", Value: raw})

	require.Equal(t, 200, w.Code, "an all-access API key must be authorized")
}

// A scoped key must still be scoped: the principal fix must not turn every key
// into an admin key.
func TestAPIKey_ScopeIsStillEnforced(t *testing.T) {
	e, err := auth.NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy(auth.APIKeyPrincipal("key-2"), testOrgID, "team-a", "*", auth.ObjRun, auth.ActRead)
	require.NoError(t, err)

	srv, m := enforcerServer(t, e)

	raw := "flint_k_scoped"
	m.Querier.On("GetAPIKeyByHash", mock.Anything, auth.HashToken(raw)).
		Return(db.GetAPIKeyByHashRow{
			ID: "key-2", OrgID: testOrgID, Name: "scoped-bot", Scopes: []string{},
		}, nil)
	m.Querier.On("TouchAPIKey", mock.Anything, "key-2").Return(nil).Maybe()
	runScope(m, "run-b", "team-b", "")

	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/runs/run-b/steps", nil,
		ut.Header{Key: "X-API-Key", Value: raw})

	require.Equal(t, 403, w.Code, "a team-a key must not read a team-b run")
}

// A revoked session must stop working immediately. The access token is a
// self-validating JWT, so without the sid binding, logout / back-channel logout
// / IdP deprovisioning all set revoked_at while the token kept working until it
// expired.
func TestSession_RevokedTokenIsRejected(t *testing.T) {
	e, err := auth.NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy("carol@x.dev", testOrgID, "*", "*", "*", "*")
	require.NoError(t, err)

	srv, m := enforcerServer(t, e)
	m.Sessions.On("ValidateSession", "carol-token").Return(&auth.Claims{
		Subject: "carol@x.dev", Email: "carol@x.dev", OrgID: testOrgID,
		Provider: "oidc", Principal: "carol@x.dev", SessionID: "sess-revoked",
	}, nil)

	revoked := time.Now().Add(-time.Minute)
	m.Querier.On("GetSessionForAuth", mock.Anything, "sess-revoked").
		Return(db.GetSessionForAuthRow{
			ID: "sess-revoked", UserID: "user-c", RevokedAt: &revoked,
			ExpiresAt: time.Now().Add(24 * time.Hour), IdleExpiresAt: time.Now().Add(time.Hour),
			IsActive: true,
		}, nil)

	require.Equal(t, 401, getSteps(srv, "run-a", "carol-token"),
		"a revoked session must not authenticate")
}

// Deactivating a user has to take effect on the next request, not at token
// expiry — the same reasoning as revocation.
func TestSession_DeactivatedUserIsRejected(t *testing.T) {
	e, err := auth.NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy("dan@x.dev", testOrgID, "*", "*", "*", "*")
	require.NoError(t, err)

	srv, m := enforcerServer(t, e)
	m.Sessions.On("ValidateSession", "dan-token").Return(&auth.Claims{
		Subject: "dan@x.dev", Email: "dan@x.dev", OrgID: testOrgID,
		Provider: "oidc", Principal: "dan@x.dev", SessionID: "sess-inactive",
	}, nil)
	m.Querier.On("GetSessionForAuth", mock.Anything, "sess-inactive").
		Return(db.GetSessionForAuthRow{
			ID: "sess-inactive", UserID: "user-d",
			ExpiresAt: time.Now().Add(24 * time.Hour), IdleExpiresAt: time.Now().Add(time.Hour),
			IsActive: false,
		}, nil)

	require.Equal(t, 401, getSteps(srv, "run-a", "dan-token"),
		"a deactivated user's session must not authenticate")
}

// A grant in one org must not authorize the same email in another. Subjects are
// bare emails and roles are per-org rows, so without the org dimension the two
// were indistinguishable.
func TestOrgDimension_GrantDoesNotCrossOrgs(t *testing.T) {
	e, err := auth.NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy("eve@x.dev", "org-other", "*", "*", "*", "*")
	require.NoError(t, err)

	srv, m := enforcerServer(t, e)
	sessionFor(m, "eve-token", "eve@x.dev") // authenticates into testOrgID
	runScope(m, "run-a", "team-a", "")

	require.Equal(t, 403, getSteps(srv, "run-a", "eve-token"),
		"a grant scoped to another org must not authorize here")
}

// An authenticated request that names no principal must fail closed. This is
// the guard that turns "a new auth path forgot to set Principal" into a logged
// error rather than a silent, permanent 403 that reads like a role problem.
func TestPrincipal_MissingPrincipalDenies(t *testing.T) {
	e, err := auth.NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy("", testOrgID, "*", "*", "*", "*") // even an explicit grant
	require.NoError(t, err)

	srv, m := enforcerServer(t, e)
	m.Sessions.On("ValidateSession", "no-principal").Return(&auth.Claims{
		Subject: "x", Email: "x@x.dev", OrgID: testOrgID, Provider: "oidc",
		SessionID: "sess-np", // Principal deliberately unset
	}, nil)
	liveSession(m, "sess-np")

	require.Equal(t, 403, getSteps(srv, "run-a", "no-principal"),
		"no principal means no authorization decision can be made")
}
