package server

import (
	"context"
	"errors"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/jackc/pgx/v5"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/platform/auth"
)

// handlePasswordLogin authenticates a user with email + password.
// If MFA is required, returns a temporary mfaToken for the second phase.
func (s *Server) handlePasswordLogin(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.BindJSON(&req); err != nil || req.Email == "" || req.Password == "" {
		apiBadRequest(ctx, c, "email and password are required")
		return
	}

	ipAddr := extractClientIP(c)

	// Rate limit check.
	failCount, _ := s.deps.Q.CountRecentFailuresByEmail(ctx, req.Email)
	if failCount >= 5 {
		apiError(ctx, c, consts.StatusTooManyRequests, "RATE_LIMITED", "too many failed attempts, try again later")
		return
	}

	// Look up user. Only a genuinely empty orgs table means "not configured";
	// every other error here is the database being unreachable or erroring, and
	// reporting that as a configuration problem sends people to look in entirely
	// the wrong place. (It did: a stopped Postgres surfaced on the sign-in screen
	// as "org not configured", which reads like a broken install.)
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			apiInternal(ctx, c, "org not configured")
		} else {
			apiError(ctx, c, consts.StatusServiceUnavailable, "DATABASE_UNAVAILABLE",
				"database unavailable — the API can't reach its datastore")
		}
		return
	}

	user, err := s.deps.Q.GetUserForAuth(ctx, db.GetUserForAuthParams{
		OrgID: org.ID, Email: req.Email,
	})
	if err != nil || user.PasswordHash == nil {
		// Record failure (timing-safe: always do the same work).
		_ = s.deps.Q.RecordLoginAttempt(ctx, db.RecordLoginAttemptParams{
			Email: req.Email, IpAddress: ipAddr, Success: false,
		})
		recordLoginMetric(ctx, "local", "failure")
		apiUnauthorized(ctx, c, "invalid credentials")
		return
	}

	if !user.IsActive {
		recordLoginMetric(ctx, "local", "failure")
		apiUnauthorized(ctx, c, "account is deactivated")
		return
	}

	// Require-SSO: when enabled, IdP-provisioned users must sign in via SSO.
	// Local/manual accounts (external_id == email, e.g. the bootstrap admin and
	// any admin-created user) are the break-glass path and are never blocked.
	if org.RequireSso && user.ExternalID != user.Email {
		s.auditLoginFailure(ctx, c, "require_sso", req.Email)
		apiError(ctx, c, consts.StatusForbidden, "SSO_REQUIRED",
			"your organization requires single sign-on; use \"Sign in with SSO\"")
		return
	}

	// Verify password (Argon2id).
	if !auth.VerifyPassword(req.Password, *user.PasswordHash) {
		_ = s.deps.Q.RecordLoginAttempt(ctx, db.RecordLoginAttemptParams{
			Email: req.Email, IpAddress: ipAddr, Success: false,
		})
		recordLoginMetric(ctx, "local", "failure")
		apiUnauthorized(ctx, c, "invalid credentials")
		return
	}

	// Record successful attempt.
	_ = s.deps.Q.RecordLoginAttempt(ctx, db.RecordLoginAttemptParams{
		Email: req.Email, IpAddress: ipAddr, Success: true,
	})

	// Check if MFA is required.
	mfaRequired := user.TotpVerified
	if !mfaRequired {
		// Check if any assigned role requires MFA.
		roleRequires, _ := s.deps.Q.CheckMFARequiredForUser(ctx, req.Email)
		if roleRequires {
			if !user.TotpVerified {
				// MFA required by role but not set up — block with clear message.
				apiError(ctx, c, consts.StatusForbidden, "MFA_SETUP_REQUIRED",
					"your role requires MFA — please set up MFA before logging in")
				return
			}
			mfaRequired = true
		}
	}

	if mfaRequired {
		// Issue temporary MFA token (5 min).
		token := generateSecureCode(32)
		if err := s.deps.Q.InsertMFAPendingToken(ctx, db.InsertMFAPendingTokenParams{
			Token:     token,
			UserID:    user.ID,
			Email:     user.Email,
			OrgID:     org.ID,
			ExpiresAt: time.Now().Add(5 * time.Minute),
		}); err != nil {
			apiInternal(ctx, c, "failed to start MFA challenge")
			return
		}

		c.JSON(consts.StatusOK, utils.H{
			"mfaRequired": true,
			"mfaToken":    token,
		})
		return
	}

	// No MFA — issue tokens directly.
	s.issueLocalAuthTokens(ctx, c, user.ID, user.Email, org.ID, user.ForcePasswordChange)
}

// issueLocalAuthTokens creates a session and returns access + refresh tokens.
func (s *Server) issueLocalAuthTokens(ctx context.Context, c *app.RequestContext,
	userID, email, orgID string, forcePasswordChange bool) {

	claims := &auth.Claims{
		Subject:    email,
		Email:      email,
		OrgID:      orgID,
		Provider:   "local",
		ExternalID: email,
	}

	accessToken, err := s.deps.Sessions.CreateSession(claims)
	if err != nil {
		apiInternal(ctx, c, "failed to create access token")
		return
	}

	refreshRaw, refreshHash, err := auth.GenerateRefreshToken()
	if err != nil {
		apiInternal(ctx, c, "failed to generate refresh token")
		return
	}

	_, err = s.deps.Q.CreateSession(ctx, db.CreateSessionParams{
		UserID:           userID,
		TokenHash:        refreshHash,
		AbsLifetimeSecs:  30 * 24 * 3600,
		IdleLifetimeSecs: 7 * 24 * 3600,
	})
	if err != nil {
		apiInternal(ctx, c, "failed to create session")
		return
	}

	// Audit + metric: a local login (password, possibly after MFA) completed.
	_ = s.deps.Q.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		OrgID: orgID, UserID: &userID,
		Action: "auth.login", ResourceType: "session",
	})
	recordLoginMetric(ctx, "local", "success")

	resp := utils.H{
		"accessToken":  accessToken,
		"refreshToken": refreshRaw,
		"expiresIn":    900,
	}
	if forcePasswordChange {
		resp["forcePasswordChange"] = true
	}
	c.JSON(consts.StatusOK, resp)
}

// handleChangePassword allows authenticated users to change their password.
func (s *Server) handleChangePassword(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	if claims == nil {
		apiUnauthorized(ctx, c, "not authenticated")
		return
	}

	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := c.BindJSON(&req); err != nil || req.CurrentPassword == "" || req.NewPassword == "" {
		apiBadRequest(ctx, c, "currentPassword and newPassword are required")
		return
	}

	if len(req.NewPassword) < 8 {
		apiBadRequest(ctx, c, "password must be at least 8 characters")
		return
	}

	user, err := s.deps.Q.GetUserForAuth(ctx, db.GetUserForAuthParams{
		OrgID: claims.OrgID, Email: claims.Email,
	})
	if err != nil || user.PasswordHash == nil {
		apiNotFound(ctx, c, "local auth not configured for this user")
		return
	}

	if !auth.VerifyPassword(req.CurrentPassword, *user.PasswordHash) {
		apiUnauthorized(ctx, c, "current password is incorrect")
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		apiInternal(ctx, c, "failed to hash password")
		return
	}

	_ = s.deps.Q.UpdateUserPassword(ctx, db.UpdateUserPasswordParams{
		ID: user.ID, PasswordHash: &hash,
	})
	_ = s.deps.Q.ClearForcePasswordChange(ctx, user.ID)

	// Audit.
	org, _ := s.deps.Q.GetOrg(ctx)
	_ = s.deps.Q.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		OrgID: org.ID, UserID: &user.ID,
		Action: "auth.password.changed", ResourceType: "user",
	})

	c.JSON(consts.StatusOK, utils.H{"status": "password_changed"})
}
