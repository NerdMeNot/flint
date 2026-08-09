package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/platform/auth"
)

// scimCtxKey is the context key under which the authenticated SCIM org id is stored.
type scimCtxKey struct{}

// scimAuthMiddleware authenticates SCIM requests via a bearer token and stashes
// the resolved org id in the context. It emits SCIM-formatted errors.
func (s *Server) scimAuthMiddleware() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		const prefix = "Bearer "
		header := string(c.GetHeader("Authorization"))
		if !strings.HasPrefix(header, prefix) {
			writeSCIMError(c, consts.StatusUnauthorized, "missing or invalid Authorization header")
			c.Abort()
			return
		}
		token := strings.TrimSpace(header[len(prefix):])
		if token == "" {
			writeSCIMError(c, consts.StatusUnauthorized, "empty bearer token")
			c.Abort()
			return
		}
		orgID, err := s.deps.Q.GetScimTokenOrg(ctx, auth.HashToken(token))
		if err != nil {
			writeSCIMError(c, consts.StatusUnauthorized, "invalid SCIM token")
			c.Abort()
			return
		}
		_ = s.deps.Q.TouchScimToken(ctx, auth.HashToken(token)) // best-effort last-used
		c.Next(context.WithValue(ctx, scimCtxKey{}, orgID))
	}
}

func scimOrgFromCtx(ctx context.Context) string {
	id, _ := ctx.Value(scimCtxKey{}).(string)
	return id
}

// scimBaseURL returns the externally-reachable base URL for SCIM resource locations.
func (s *Server) scimBaseURL() string {
	return strings.TrimRight(s.deps.Config.Server.BaseURL, "/")
}

// ── Token management API (JWT-authenticated, role:manage) ──────────────────

// handleGetScimStatus reports whether a SCIM token is configured and the base URL.
func (s *Server) handleGetScimStatus(ctx context.Context, c *app.RequestContext) {
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to load org")
		return
	}
	n, err := s.deps.Q.CountScimTokensForOrg(ctx, org.ID)
	if err != nil {
		apiInternal(ctx, c, "failed to query scim tokens")
		return
	}
	c.JSON(consts.StatusOK, utils.H{
		"configured": n > 0,
		"baseUrl":    s.scimBaseURL() + "/scim/v2",
	})
}

// handleGenerateScimToken creates a new SCIM token (replacing any existing one)
// and returns the plaintext value exactly once.
func (s *Server) handleGenerateScimToken(ctx context.Context, c *app.RequestContext) {
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to load org")
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		apiInternal(ctx, c, "failed to generate token")
		return
	}
	token := "scim_" + hex.EncodeToString(raw)

	if err := s.deps.Q.DeleteScimTokensForOrg(ctx, org.ID); err != nil {
		apiInternal(ctx, c, "failed to clear existing token")
		return
	}
	if err := s.deps.Q.InsertScimToken(ctx, db.InsertScimTokenParams{OrgID: org.ID, TokenHash: auth.HashToken(token)}); err != nil {
		apiInternal(ctx, c, "failed to store token")
		return
	}
	s.recordAudit(ctx, "auth.scim_token.generated", "auth_provider")
	c.JSON(consts.StatusOK, utils.H{
		"token":   token,
		"baseUrl": s.scimBaseURL() + "/scim/v2",
	})
}

// handleRevokeScimToken removes the org's SCIM token, disabling provisioning.
func (s *Server) handleRevokeScimToken(ctx context.Context, c *app.RequestContext) {
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to load org")
		return
	}
	if err := s.deps.Q.DeleteScimTokensForOrg(ctx, org.ID); err != nil {
		apiInternal(ctx, c, "failed to revoke token")
		return
	}
	s.recordAudit(ctx, "auth.scim_token.revoked", "auth_provider")
	c.JSON(consts.StatusOK, utils.H{"status": "revoked"})
}
