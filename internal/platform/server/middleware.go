package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/internal/platform/auth"
)

// authMiddleware extracts and validates auth from Bearer token or X-API-Key header.
func (s *Server) authMiddleware() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		// Try API key first.
		apiKey := string(c.GetHeader("X-API-Key"))
		if apiKey != "" {
			claims, err := s.validateAPIKey(ctx, apiKey)
			if err != nil {
				apiUnauthorized(ctx, c, "invalid API key")
				c.Abort()
				return
			}
			ctx = context.WithValue(ctx, authClaimsKey, claims)
			ctx = observe.WithUserID(ctx, claims.Subject)
			ctx = observe.WithOrgID(ctx, claims.OrgID)
			c.Next(ctx)
			return
		}

		// Try Bearer token.
		header := string(c.GetHeader("Authorization"))
		if header == "" {
			// Browser EventSource (SSE) can't set an Authorization header, so for
			// streaming endpoints accept the token as an ?access_token= query param.
			if qt := string(c.Query("access_token")); qt != "" {
				header = "Bearer " + qt
			}
		}
		if header == "" {
			apiUnauthorized(ctx, c, "missing authorization header")
			c.Abort()
			return
		}

		token := strings.TrimPrefix(header, "Bearer ")
		if token == header {
			apiUnauthorized(ctx, c, "invalid authorization format, expected Bearer token")
			c.Abort()
			return
		}

		// Try personal token (flint_pat_ prefix).
		if strings.HasPrefix(token, "flint_pat_") {
			claims, err := s.validatePersonalToken(ctx, token)
			if err != nil {
				apiUnauthorized(ctx, c, "invalid or expired personal token")
				c.Abort()
				return
			}
			ctx = context.WithValue(ctx, authClaimsKey, claims)
			ctx = observe.WithUserID(ctx, claims.Subject)
			ctx = observe.WithOrgID(ctx, claims.OrgID)
			c.Next(ctx)
			return
		}

		if s.deps.Sessions == nil {
			apiError(ctx, c, consts.StatusInternalServerError, "INTERNAL", "auth not configured")
			c.Abort()
			return
		}

		claims, err := s.deps.Sessions.ValidateSession(token)
		if err != nil {
			apiUnauthorized(ctx, c, "invalid or expired token")
			c.Abort()
			return
		}

		// A JWT validates itself, which means it also outlives any attempt to
		// take it away: logout, OIDC back-channel logout and the IdP sync
		// daemon all set sessions.revoked_at, and none of it had any effect on
		// a token already issued. Bind the request to the session row.
		if err := s.checkSessionLive(ctx, claims); err != nil {
			apiUnauthorized(ctx, c, err.Error())
			c.Abort()
			return
		}

		ctx = context.WithValue(ctx, authClaimsKey, claims)
		ctx = observe.WithUserID(ctx, claims.Subject)
		ctx = observe.WithOrgID(ctx, claims.OrgID)

		c.Next(ctx)
	}
}

// checkSessionLive rejects a token whose session has been revoked or has run
// past its absolute/idle expiry, or whose owner has been deactivated.
//
// Tokens minted before `sid` existed carry no session to check. They are
// rejected rather than waved through: the whole point of the check is that a
// token cannot vouch for its own liveness, and "no sid" is not evidence of
// anything. The cost is that sessions issued before this deploy have to log in
// again once.
func (s *Server) checkSessionLive(ctx context.Context, claims *auth.Claims) error {
	if s.deps.Q == nil {
		return nil // no store wired (tests, degraded boot) — nothing to check against
	}
	if claims.SessionID == "" {
		return fmt.Errorf("session token predates revocation checking, please sign in again")
	}

	sess, err := s.deps.Q.GetSessionForAuth(ctx, claims.SessionID)
	if err != nil {
		return fmt.Errorf("session no longer exists")
	}
	if sess.RevokedAt != nil {
		return fmt.Errorf("session revoked")
	}
	if !sess.IsActive {
		return fmt.Errorf("account is deactivated")
	}
	now := time.Now()
	if now.After(sess.ExpiresAt) {
		return fmt.Errorf("session expired")
	}
	if now.After(sess.IdleExpiresAt) {
		return fmt.Errorf("session idle timeout")
	}
	return nil
}

// validateAPIKey looks up an API key, validates it, and returns claims.
//
// The key is located by the SHA-256 digest of its raw value. This used to load
// every live key and bcrypt-compare them one at a time — deliberately-slow
// hashing repeated per candidate per request, which is both a latency cliff as
// key count grows and an easy way to burn a server's CPU from unauthenticated
// requests. API keys are 32 bytes of entropy we generated ourselves, so they
// need a fast digest and a unique index, not a password KDF.
func (s *Server) validateAPIKey(ctx context.Context, key string) (*auth.Claims, error) {
	if s.deps.DB == nil || s.deps.Q == nil {
		return nil, fmt.Errorf("database not configured")
	}

	k, err := s.deps.Q.GetAPIKeyByHash(ctx, auth.HashToken(key))
	if err != nil {
		return nil, fmt.Errorf("API key not found")
	}

	// Update last_used_at.
	_ = s.deps.Q.TouchAPIKey(ctx, k.ID)

	claims := &auth.Claims{
		Subject:    k.Name,
		OrgID:      k.OrgID,
		Provider:   "api_key",
		ExternalID: k.ID,
		// Policies for a key are written against "apikey:<id>"; enforcing on
		// anything else (Email, which a key does not have) matches nothing and
		// denies every request the key makes.
		Principal: auth.APIKeyPrincipal(k.ID),
	}
	if k.UserID != nil {
		claims.Subject = *k.UserID
	}

	return claims, nil
}

// validatePersonalToken looks up a personal access token, validates the hash,
// and returns claims as the token's owner. The user's email becomes the RBAC
// principal — same as a browser session.
func (s *Server) validatePersonalToken(ctx context.Context, token string) (*auth.Claims, error) {
	if s.deps.DB == nil || s.deps.Q == nil {
		return nil, fmt.Errorf("database not configured")
	}

	t, err := s.deps.Q.GetPersonalTokenByHash(ctx, auth.HashToken(token))
	if err != nil {
		return nil, fmt.Errorf("personal token not found or expired")
	}

	_ = s.deps.Q.TouchPersonalToken(ctx, t.ID) // best-effort last_used_at

	return &auth.Claims{
		Subject:    t.UserID,
		Email:      t.Email,
		Name:       t.Name,
		OrgID:      t.OrgID,
		Provider:   "personal_token",
		ExternalID: t.ID,
		Principal:  t.Email,
	}, nil
}

// requirePermission returns Casbin-backed middleware that checks whether the
// authenticated user can perform (obj, act) in the resolved workspace.
// The workspace is resolved from: project's workspace → query param → "*" (global).
func (s *Server) requirePermission(obj, act string) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		claims := claimsFromCtx(ctx)
		if claims == nil {
			apiUnauthorized(ctx, c, "not authenticated")
			c.Abort()
			return
		}

		if s.deps.Enforcer == nil {
			// No enforcer means no authorization, so this denies. It used to call
			// through with a comment about falling back to a legacy role check —
			// there is no such fallback, and the comment is what would stop anyone
			// reading this from worrying: without an enforcer, every permission
			// check on every route passed.
			//
			// Not reachable from a real server (boot fails hard if NewEnforcer
			// errors), but "unreachable" is a property of today's wiring, and the
			// failure mode if it ever changes is a fully open API.
			logger := observe.Logger(ctx)
			logger.Error().Str("obj", obj).Str("act", act).
				Msg("authorization unavailable: no enforcer configured")
			apiForbidden(ctx, c, "authorization unavailable")
			c.Abort()
			return
		}

		// Every authenticated path has to name the subject policies are keyed
		// on. Failing closed and loudly here is what turns "a new auth method
		// forgot to set Principal" into an obvious error in the logs instead of
		// a puzzling 403 that looks like a misconfigured role.
		if claims.Principal == "" {
			logger := observe.Logger(ctx)
			logger.Error().Str("provider", claims.Provider).Str("obj", obj).Str("act", act).
				Msg("authorization: authenticated request has no principal")
			apiForbidden(ctx, c, "authorization unavailable")
			c.Abort()
			return
		}

		workspace, environment := s.resolveScope(ctx, c, obj)

		allowed, err := s.deps.Enforcer.Enforce(claims.Principal, claims.OrgID, workspace, environment, obj, act)
		if err != nil {
			logger := observe.Logger(ctx)
			logger.Error().Err(err).
				Str("principal", claims.Principal).
				Str("org", claims.OrgID).
				Str("workspace", workspace).
				Str("environment", environment).
				Str("obj", obj).
				Str("act", act).
				Msg("casbin enforcement error")
			apiForbidden(ctx, c, "authorization check failed")
			c.Abort()
			return
		}

		if !allowed {
			apiForbidden(ctx, c, "insufficient permissions")
			c.Abort()
			return
		}

		c.Next(ctx)
	}
}

// WorkspaceScope is the RBAC workspace restriction a collection route must
// apply to its results.
//
// It is passed to the handler as an argument rather than left on the context
// because a collection route's authorization is deliberately weak: it
// establishes only that the caller holds the permission *somewhere*, and the
// narrowing that makes it safe happens inside the handler. An ambient context
// value made that narrowing easy to omit, and omitting it is silent — a
// forgotten filter looks exactly like an unrestricted user. That is not
// hypothetical: GET /projects?archived=true shared the /projects route, did not
// read the context value, and served every workspace's rows to scope-limited
// callers. As a parameter, the restriction is in the signature of every handler
// that needs one and cannot be missed by omission.
type WorkspaceScope struct {
	// Restricted reports whether any narrowing applies. False means the subject
	// holds the permission platform-wide and the result set is not filtered.
	Restricted bool
	// Slugs is the permitted set; meaningful only when Restricted.
	Slugs []string
}

// Filter returns the workspace slugs to pass to a query, given whatever the
// caller explicitly asked for. The permitted set is a floor: an explicit
// ?workspace= can narrow it, never reach outside it. An empty result means the
// caller asked for nothing they are allowed to see.
func (w WorkspaceScope) Filter(requested []string) []string {
	if !w.Restricted {
		return requested
	}
	if len(requested) == 0 {
		return w.Slugs
	}
	return intersectSlugs(w.Slugs, requested)
}

// Empty reports whether the effective filter selects nothing, so the handler
// can return an empty page instead of running a query that matches everything.
func (w WorkspaceScope) Empty(requested []string) bool {
	return w.Restricted && len(w.Filter(requested)) == 0
}

// scopedListHandler is the signature of a collection route behind
// requireAnyScope.
type scopedListHandler func(ctx context.Context, c *app.RequestContext, scope WorkspaceScope)

// requireAnyScope authorizes a collection route and hands the resulting
// workspace restriction to the handler: it passes when the subject holds
// (obj, act) in *any* workspace, and the handler narrows its rows to the
// permitted set.
//
// Collection routes act on no single resource, so there is no scope to derive
// and nothing honest to check them against. Checking them against "*" meant a
// workspace-scoped user got a 403 from GET /projects; the previous escape hatch
// was to let them pass ?workspace=, which is attacker-controlled input standing
// in for an access decision. Authorizing the question that can actually be
// answered — "anywhere?" — and then filtering is the version that holds.
func (s *Server) requireAnyScope(obj, act string, handler scopedListHandler) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		claims := claimsFromCtx(ctx)
		if claims == nil {
			apiUnauthorized(ctx, c, "not authenticated")
			c.Abort()
			return
		}
		if s.deps.Enforcer == nil {
			logger := observe.Logger(ctx)
			logger.Error().Str("obj", obj).Str("act", act).
				Msg("authorization unavailable: no enforcer configured")
			apiForbidden(ctx, c, "authorization unavailable")
			c.Abort()
			return
		}
		if claims.Principal == "" {
			logger := observe.Logger(ctx)
			logger.Error().Str("provider", claims.Provider).
				Msg("authorization: authenticated request has no principal")
			apiForbidden(ctx, c, "authorization unavailable")
			c.Abort()
			return
		}

		allowed, err := s.deps.Enforcer.Enforce(
			claims.Principal, claims.OrgID, auth.ScopeAny, auth.ScopeAny, obj, act)
		if err != nil {
			logger := observe.Logger(ctx)
			logger.Error().Err(err).
				Str("principal", claims.Principal).Str("obj", obj).Str("act", act).
				Msg("casbin enforcement error")
			apiForbidden(ctx, c, "authorization check failed")
			c.Abort()
			return
		}
		if !allowed {
			apiForbidden(ctx, c, "insufficient permissions")
			c.Abort()
			return
		}

		slugs, all, err := auth.PermittedWorkspaces(s.deps.Enforcer, claims.Principal, claims.OrgID, obj, act)
		if err != nil {
			logger := observe.Logger(ctx)
			logger.Error().Err(err).Str("principal", claims.Principal).
				Msg("resolving permitted workspaces")
			apiForbidden(ctx, c, "authorization check failed")
			c.Abort()
			return
		}

		scope := WorkspaceScope{Restricted: !all, Slugs: slugs}
		if !all && len(slugs) == 0 {
			// Enforce said yes but no workspace carries the grant. Treat that as
			// a denial rather than an unfiltered list.
			apiForbidden(ctx, c, "insufficient permissions")
			c.Abort()
			return
		}

		handler(ctx, c, scope)
	}
}

func intersectSlugs(a, b []string) []string {
	set := make(map[string]bool, len(a))
	for _, s := range a {
		set[s] = true
	}
	var out []string
	for _, s := range b {
		if set[s] {
			out = append(out, s)
		}
	}
	return out
}

// resolveScope determines the (workspace, environment) the request acts in. These
// become the ws/env dimensions of the Casbin check, so scope-restricted roles are
// enforced against the resource the request actually touches. Resolution depends
// on the object type:
//   - run/gate routes: derived from the run (run → project → workspace) plus the
//     run's environment; workflow runs have neither, so both stay "*".
//   - project routes: workspace from the project in the URL.
//   - admin objects (team, role, workspace, …): always global — their policies are
//     platform-wide ("*","*"), which match any ws/env.
//
// A ?workspace / ?environment query param is only consulted for a dimension
// nothing could be derived for — routes that carry no resource id in the path.
// It must never replace a derived value: the derived value describes the
// resource the handler is about to act on, while the query param is whatever
// the caller typed. Letting the caller's value win meant a user scoped to
// team-a could read a team-b run by appending ?workspace=team-a — the check
// passed against a workspace the request was not touching. "*" means "all" and
// matches platform-wide policies.
func (s *Server) resolveScope(ctx context.Context, c *app.RequestContext, obj string) (workspace, environment string) {
	workspace, environment = "*", "*"
	derivedWS, derivedEnv := false, false

	switch obj {
	case auth.ObjRun, auth.ObjGate:
		if runID := c.Param("id"); runID != "" && s.deps.Q != nil {
			if sc, err := s.deps.Q.GetRunScope(ctx, runID); err == nil {
				if sc.WorkspaceSlug != "" {
					workspace, derivedWS = sc.WorkspaceSlug, true
				}
				if sc.Environment != "" {
					environment, derivedEnv = sc.Environment, true
				}
			}
		}
	case auth.ObjProject:
		if projectID := c.Param("id"); projectID != "" && s.deps.Q != nil {
			if slug, err := s.deps.Q.GetProjectWorkspaceSlug(ctx, projectID); err == nil && slug != "" {
				workspace, derivedWS = slug, true
			}
		}
	}

	if !derivedWS {
		if ws := string(c.Query("workspace")); ws != "" {
			workspace = ws
		}
	}
	if !derivedEnv {
		if env := string(c.Query("environment")); env != "" {
			environment = env
		}
	}
	return workspace, environment
}

// requestIDMiddleware generates a unique request ID.
func (s *Server) requestIDMiddleware() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		ctx = observe.WithRequestID(ctx, "")
		c.Next(ctx)
	}
}

// maxBodyMiddleware limits request body size.
func maxBodyMiddleware(maxBytes int) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if c.Request.Header.ContentLength() > maxBytes {
			apiError(ctx, c, consts.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "request body too large")
			c.Abort()
			return
		}
		c.Next(ctx)
	}
}

type ctxKeyType string

const (
	authClaimsKey ctxKeyType = "flint_claims"
)

func claimsFromCtx(ctx context.Context) *auth.Claims {
	claims, _ := ctx.Value(authClaimsKey).(*auth.Claims)
	return claims
}

// optionalAuth runs the normal authentication chain when the request carries
// credentials, and calls through with no claims when it does not.
//
// It exists for the MFA enrolment endpoints, which have two legitimate callers:
// a signed-in user adding a second factor, and a user who cannot sign in *until*
// they add one. Making auth mandatory there is what created the lockout;
// dropping it entirely would let anyone reset a stranger's second factor.
// Neither caller gets in without proving something first — a session, or a
// short-lived enrolment token minted against a correct password.
func (s *Server) optionalAuth() app.HandlerFunc {
	authenticate := s.authMiddleware()
	return func(ctx context.Context, c *app.RequestContext) {
		if len(c.GetHeader("Authorization")) == 0 && len(c.GetHeader("X-API-Key")) == 0 {
			c.Next(ctx)
			return
		}
		authenticate(ctx, c)
	}
}
