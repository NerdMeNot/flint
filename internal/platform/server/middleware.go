package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"golang.org/x/crypto/bcrypt"
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

		ctx = context.WithValue(ctx, authClaimsKey, claims)
		ctx = observe.WithUserID(ctx, claims.Subject)
		ctx = observe.WithOrgID(ctx, claims.OrgID)

		c.Next(ctx)
	}
}

// validateAPIKey looks up an API key, validates it, and returns claims.
func (s *Server) validateAPIKey(ctx context.Context, key string) (*auth.Claims, error) {
	if s.deps.DB == nil {
		return nil, nil
	}

	// Hash the key and look up in api_keys table.
	keys, err := s.deps.Q.ListValidAPIKeys(ctx)
	if err != nil {
		return nil, err
	}

	var found bool
	var keyID, orgID, name string
	var userID *string
	for _, k := range keys {
		if bcrypt.CompareHashAndPassword([]byte(k.KeyHash), []byte(key)) == nil {
			found = true
			keyID = k.ID
			orgID = k.OrgID
			userID = k.UserID
			name = k.Name
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("API key not found")
	}

	// Update last_used_at.
	_ = s.deps.Q.TouchAPIKey(ctx, keyID)

	claims := &auth.Claims{
		Subject:    name,
		OrgID:      orgID,
		Provider:   "api_key",
		ExternalID: keyID,
	}
	if userID != nil {
		claims.Subject = *userID
	}

	// API key authorization is handled by Casbin — policies are generated
	// from the api_keys table during RegeneratePolicies.
	return claims, nil
}

// validatePersonalToken looks up a personal access token, validates the hash,
// and returns claims as the token's owner. The user's email becomes the Casbin
// subject — same as a browser session.
func (s *Server) validatePersonalToken(ctx context.Context, token string) (*auth.Claims, error) {
	if s.deps.DB == nil {
		return nil, fmt.Errorf("database not configured")
	}

	tokens, err := s.deps.Q.ListValidPersonalTokensWithUser(ctx)
	if err != nil {
		return nil, err
	}

	for _, t := range tokens {
		if bcrypt.CompareHashAndPassword([]byte(t.TokenHash), []byte(token)) == nil {
			_ = s.deps.Q.TouchPersonalToken(ctx, t.ID) // best-effort last_used_at
			return &auth.Claims{
				Subject:    t.UserID,
				Email:      t.Email,
				Name:       t.Name,
				OrgID:      t.OrgID,
				Provider:   "personal_token",
				ExternalID: t.ID,
			}, nil
		}
	}

	return nil, fmt.Errorf("personal token not found or expired")
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
			// Casbin not initialized — fall back to legacy role check.
			c.Next(ctx)
			return
		}

		workspace, environment := s.resolveScope(ctx, c, obj)

		allowed, err := s.deps.Enforcer.Enforce(claims.Email, workspace, environment, obj, act)
		if err != nil {
			logger := observe.Logger(ctx)
			logger.Error().Err(err).
				Str("user", claims.Email).
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
// An explicit ?workspace / ?environment query param overrides — used by list and
// trigger routes that carry no resource id in the path. "*" means "all" and
// matches platform-wide policies.
func (s *Server) resolveScope(ctx context.Context, c *app.RequestContext, obj string) (workspace, environment string) {
	workspace, environment = "*", "*"

	switch obj {
	case auth.ObjRun, auth.ObjGate:
		if runID := c.Param("id"); runID != "" && s.deps.Q != nil {
			if sc, err := s.deps.Q.GetRunScope(ctx, runID); err == nil {
				if sc.WorkspaceSlug != "" {
					workspace = sc.WorkspaceSlug
				}
				if sc.Environment != "" {
					environment = sc.Environment
				}
			}
		}
	case auth.ObjProject:
		if projectID := c.Param("id"); projectID != "" && s.deps.Q != nil {
			if slug, err := s.deps.Q.GetProjectWorkspaceSlug(ctx, projectID); err == nil && slug != "" {
				workspace = slug
			}
		}
	}

	if ws := string(c.Query("workspace")); ws != "" {
		workspace = ws
	}
	if env := string(c.Query("environment")); env != "" {
		environment = env
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
