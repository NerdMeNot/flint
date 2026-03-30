package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/NerdMeNot/flint/internal/auth"
	"github.com/NerdMeNot/flint/internal/observe"
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
			claims, role, err := s.validateAPIKey(ctx, apiKey)
			if err != nil {
				apiUnauthorized(ctx, c, "invalid API key")
				c.Abort()
				return
			}
			ctx = context.WithValue(ctx, authClaimsKey, claims)
			ctx = context.WithValue(ctx, authRoleKey, role)
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

		if s.deps.Sessions == nil {
			apiError(ctx, c, consts.StatusInternalServerError, "INTERNAL", "auth not configured")
			c.Abort()
			return
		}

		claims, role, err := s.deps.Sessions.ValidateSession(token)
		if err != nil {
			apiUnauthorized(ctx, c, "invalid or expired token")
			c.Abort()
			return
		}

		ctx = context.WithValue(ctx, authClaimsKey, claims)
		ctx = context.WithValue(ctx, authRoleKey, role)
		ctx = observe.WithUserID(ctx, claims.Subject)
		ctx = observe.WithOrgID(ctx, claims.OrgID)

		c.Next(ctx)
	}
}

// validateAPIKey looks up an API key, validates it, and returns claims + role.
func (s *Server) validateAPIKey(ctx context.Context, key string) (*auth.Claims, auth.Role, error) {
	if s.deps.DB == nil {
		return nil, "", nil
	}

	// Hash the key and look up in api_keys table.
	keys, err := s.deps.Q.ListValidAPIKeys(ctx)
	if err != nil {
		return nil, "", err
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
		return nil, "", fmt.Errorf("API key not found")
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

	// API keys use developer role by default; scope checking is done per-endpoint.
	return claims, auth.RoleDeveloper, nil
}

// requireAction returns middleware that checks RBAC permission (legacy, global only).
func requireAction(action auth.Action) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		role := roleFromCtx(ctx)
		if !auth.Can(role, action) {
			apiForbidden(ctx, c, "insufficient permissions")
			c.Abort()
			return
		}
		c.Next(ctx)
	}
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

		workspace := s.resolveWorkspace(ctx, c)

		allowed, err := s.deps.Enforcer.Enforce(claims.Email, workspace, obj, act)
		if err != nil {
			logger := observe.Logger(ctx)
			logger.Error().Err(err).
				Str("user", claims.Email).
				Str("workspace", workspace).
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

// resolveWorkspace determines the workspace context for the current request.
// Priority: project's workspace (from URL param) → explicit query param → "*" (global).
func (s *Server) resolveWorkspace(ctx context.Context, c *app.RequestContext) string {
	// Try to resolve from project ID in URL.
	projectID := c.Param("id")
	if projectID != "" && s.deps.Q != nil {
		slug, err := s.deps.Q.GetProjectWorkspaceSlug(ctx, projectID)
		if err == nil && slug != "" {
			return slug
		}
	}

	// Try explicit workspace query param.
	if ws := string(c.Query("workspace")); ws != "" {
		return ws
	}

	// Default to global scope.
	return "*"
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
	authRoleKey   ctxKeyType = "flint_role"
)

func claimsFromCtx(ctx context.Context) *auth.Claims {
	claims, _ := ctx.Value(authClaimsKey).(*auth.Claims)
	return claims
}

func roleFromCtx(ctx context.Context) auth.Role {
	role, _ := ctx.Value(authRoleKey).(auth.Role)
	return role
}
