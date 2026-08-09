package server

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/platform/auth"
)

// handleRefresh exchanges a refresh token for a new access JWT + rotated refresh token.
func (s *Server) handleRefresh(ctx context.Context, c *app.RequestContext) {
	var req struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := c.BindJSON(&req); err != nil || req.RefreshToken == "" {
		apiBadRequest(ctx, c, "refreshToken is required")
		return
	}

	// Look up session by token hash.
	tokenHash := auth.HashToken(req.RefreshToken)
	session, err := s.deps.Q.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		apiUnauthorized(ctx, c, "invalid refresh token")
		return
	}

	// Check revocation.
	if session.RevokedAt != nil {
		apiUnauthorized(ctx, c, "session revoked")
		return
	}

	// Check absolute expiry.
	now := time.Now()
	if now.After(session.ExpiresAt) {
		apiUnauthorized(ctx, c, "session expired")
		return
	}

	// Check idle expiry.
	if now.After(session.IdleExpiresAt) {
		apiUnauthorized(ctx, c, "session idle timeout")
		return
	}

	// Look up user for claims — also verify they're still active.
	user, err := s.deps.Q.GetUserByID(ctx, session.UserID)
	if err != nil {
		apiUnauthorized(ctx, c, "user not found")
		return
	}
	if !user.IsActive {
		// User deprovisioned — revoke all their sessions.
		_ = s.deps.Q.RevokeUserSessions(ctx, session.UserID)
		apiUnauthorized(ctx, c, "account is deactivated")
		return
	}

	org, _ := s.deps.Q.GetOrg(ctx)

	claims := &auth.Claims{
		Subject:    user.ExternalID,
		Email:      user.Email,
		Name:       derefString(user.Name),
		OrgID:      org.ID,
		Provider:   "session",
		ExternalID: user.ExternalID,
	}

	// Create new access JWT.
	accessToken, err := s.deps.Sessions.CreateSession(claims)
	if err != nil {
		apiInternal(ctx, c, "failed to create access token")
		return
	}

	// Rotate refresh token.
	newRaw, newHash, err := auth.GenerateRefreshToken()
	if err != nil {
		apiInternal(ctx, c, "failed to generate refresh token")
		return
	}
	_ = s.deps.Q.RotateSessionToken(ctx, db.RotateSessionTokenParams{
		ID:               session.ID,
		NewHash:          newHash,
		IdleLifetimeSecs: 7 * 24 * 3600, // 7 days
	})

	c.JSON(consts.StatusOK, utils.H{
		"accessToken":  accessToken,
		"refreshToken": newRaw,
		"expiresIn":    900,
	})
}

// handleLogout revokes the current session.
func (s *Server) handleLogout(ctx context.Context, c *app.RequestContext) {
	var req struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := c.BindJSON(&req); err != nil || req.RefreshToken == "" {
		apiBadRequest(ctx, c, "refreshToken is required")
		return
	}

	tokenHash := auth.HashToken(req.RefreshToken)

	// Look up the session (for audit + Single Logout material) before revoking.
	sess, _ := s.deps.Q.GetSessionByTokenHash(ctx, tokenHash)

	_ = s.deps.Q.RevokeSessionByHash(ctx, tokenHash)

	// Audit: logout event.
	if sess.UserID != "" {
		org, _ := s.deps.Q.GetOrg(ctx)
		_ = s.deps.Q.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
			OrgID:        org.ID,
			UserID:       &sess.UserID,
			Action:       "auth.logout",
			ResourceType: "session",
			ResourceID:   &sess.ID,
		})
	}

	// Single Logout: if this session was established via SSO and the IdP
	// supports it, hand the SPA the IdP logout URL to redirect the browser to,
	// terminating the upstream session too. Local revoke already happened, so a
	// missing/unsupported IdP just degrades to a local-only logout.
	if st, ok := s.decodeLogoutState(sess.LogoutStateEnc); ok {
		logoutResponse(c, s.ssoLogoutURL(st))
		return
	}

	logoutResponse(c, "")
}

// handleListSessions returns the current user's active sessions.
func (s *Server) handleListSessions(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	if claims == nil {
		apiUnauthorized(ctx, c, "not authenticated")
		return
	}

	user, err := s.deps.Q.GetUserByEmail(ctx, db.GetUserByEmailParams{
		OrgID: claims.OrgID, Email: claims.Email,
	})
	if err != nil {
		apiNotFound(ctx, c, "user not found")
		return
	}

	sessions, err := s.deps.Q.ListUserSessions(ctx, user.ID)
	if err != nil {
		apiInternal(ctx, c, "failed to list sessions")
		return
	}

	c.JSON(consts.StatusOK, utils.H{"items": sessions})
}

// handleRevokeSession revokes a specific session by ID.
func (s *Server) handleRevokeSession(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	if id == "" {
		apiBadRequest(ctx, c, "session id required")
		return
	}

	_ = s.deps.Q.RevokeSession(ctx, id)
	c.JSON(consts.StatusOK, utils.H{"success": true})
}

// handleAuthMe returns the current user's claims and permissions.
func (s *Server) handleAuthMe(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)

	if claims == nil {
		apiUnauthorized(ctx, c, "not authenticated")
		return
	}

	// Derive permissions from Casbin policies for this user.
	var permissions []string
	role := ""
	if s.deps.Enforcer != nil {
		policies, _ := s.deps.Enforcer.GetImplicitPermissionsForUser(claims.Email)
		seen := make(map[string]bool, len(policies))
		for _, p := range policies {
			// p = [sub, ws, env, obj, act]
			if len(p) >= 5 {
				key := p[3] + ":" + p[4]
				if !seen[key] {
					seen[key] = true
					permissions = append(permissions, key)
				}
			}
		}
		// Derive role from Casbin grouping policies.
		if roles, _ := s.deps.Enforcer.GetRolesForUser(claims.Email); len(roles) > 0 {
			role = roles[0]
		}
	}

	// Persisted profile fields (display name override, avatar, appearance prefs)
	// live on the user row. Best-effort: fall back to JWT claims if absent.
	name := claims.Name
	var avatarURL, themeMode, colorTheme string
	var mfaEnabled bool
	if user, err := s.deps.Q.GetUserByEmail(ctx, db.GetUserByEmailParams{
		OrgID: claims.OrgID, Email: claims.Email,
	}); err == nil {
		if user.Name != nil && *user.Name != "" {
			name = *user.Name
		}
		avatarURL = derefString(user.AvatarUrl)
		themeMode = derefString(user.ThemeMode)
		colorTheme = derefString(user.ColorTheme)
		mfaEnabled = user.TotpVerified
	}

	// The client types permissions/groups as string[]; serialize empty as [] not
	// null so consumers (e.g. the profile page) can read .length safely.
	if permissions == nil {
		permissions = []string{}
	}
	groups := claims.Groups
	if groups == nil {
		groups = []string{}
	}

	c.JSON(consts.StatusOK, utils.H{
		"userId":      claims.Subject,
		"email":       claims.Email,
		"name":        name,
		"avatarUrl":   avatarURL,
		"orgId":       claims.OrgID,
		"role":        role,
		"permissions": permissions,
		"provider":    claims.Provider,
		"groups":      groups,
		"themeMode":   themeMode,
		"colorTheme":  colorTheme,
		"mfaEnabled":  mfaEnabled,
	})
}

// handleUpdateProfile is the self-service profile patch: display name, avatar,
// and appearance preferences (theme mode + color palette). Any omitted field is
// left unchanged. Persists against the current user's row so appearance follows
// them across devices.
func (s *Server) handleUpdateProfile(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	if claims == nil {
		apiUnauthorized(ctx, c, "not authenticated")
		return
	}

	var req struct {
		Name       *string `json:"name"`
		AvatarURL  *string `json:"avatarUrl"`
		ThemeMode  *string `json:"themeMode"`
		ColorTheme *string `json:"colorTheme"`
	}
	if err := c.BindJSON(&req); err != nil {
		apiBadRequest(ctx, c, "invalid request body")
		return
	}

	if req.ThemeMode != nil {
		switch *req.ThemeMode {
		case "light", "dark", "auto":
		default:
			apiBadRequest(ctx, c, "themeMode must be light, dark, or auto")
			return
		}
	}

	user, err := s.deps.Q.GetUserByEmail(ctx, db.GetUserByEmailParams{
		OrgID: claims.OrgID, Email: claims.Email,
	})
	if err != nil {
		apiNotFound(ctx, c, "user not found")
		return
	}

	if err := s.deps.Q.UpdateUserProfile(ctx, db.UpdateUserProfileParams{
		ID:         user.ID,
		Name:       req.Name,
		AvatarUrl:  req.AvatarURL,
		ThemeMode:  req.ThemeMode,
		ColorTheme: req.ColorTheme,
	}); err != nil {
		apiInternal(ctx, c, "failed to update profile")
		return
	}

	c.JSON(consts.StatusOK, utils.H{"success": true})
}
