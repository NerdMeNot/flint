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

// Failure budgets over the last 15 minutes, counted in login_attempts.
// The IP budget is the larger of the two: several people can legitimately sign
// in from one office NAT, but one account should not fail five times.
const (
	maxFailuresPerEmail = 5
	maxFailuresPerIP    = 25
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

	// Rate limit: per email and per source IP.
	//
	// The email counter alone is the wrong shape in both directions. It does not
	// see password spraying — one guess each against a hundred accounts trips
	// nothing — and it lets anyone lock a known user out of their own account
	// with five deliberate failures. The IP counter covers the first; the
	// per-IP token bucket on the route covers bursts.
	failCount, _ := s.deps.Q.CountRecentFailuresByEmail(ctx, req.Email)
	ipFailCount, _ := s.deps.Q.CountRecentFailuresByIP(ctx, ipAddr)
	if failCount >= maxFailuresPerEmail || ipFailCount >= maxFailuresPerIP {
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
		// Pay the same Argon2id cost a real verification would. This path used to
		// return immediately under a comment claiming it was timing-safe, which
		// made response time a reliable answer to "does this account exist?".
		auth.VerifyAgainstDummyHash(req.Password)
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

	// Check if MFA is required: either the user has enrolled, or a role they
	// hold demands it (local auth only — see CheckMFARequiredForUser).
	mfaRequired := user.TotpVerified
	roleRequires := false
	if !mfaRequired {
		roleRequires, _ = s.deps.Q.CheckMFARequiredForUser(ctx, req.Email)
		mfaRequired = roleRequires
	}

	// Role demands MFA but the user has not enrolled. Hand out a token that can
	// do exactly one thing — enrol a second factor — rather than refusing
	// outright. Refusing was a dead end: enrolling requires a session, and this
	// is the only place a session is issued, so turning the flag on locked the
	// user out permanently.
	if roleRequires && !user.TotpVerified {
		token, err := s.startMFAChallenge(ctx, user.ID, user.Email, org.ID, mfaPurposeEnrol)
		if err != nil {
			apiInternal(ctx, c, "failed to start MFA enrolment")
			return
		}
		c.JSON(consts.StatusOK, utils.H{
			"mfaSetupRequired": true,
			"enrolmentToken":   token,
			"message":          "your role requires MFA — enrol a second factor to continue",
		})
		return
	}

	if mfaRequired {
		token, err := s.startMFAChallenge(ctx, user.ID, user.Email, org.ID, mfaPurposeVerify)
		if err != nil {
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

// startMFAChallenge mints a short-lived pending-MFA token for the given purpose.
func (s *Server) startMFAChallenge(ctx context.Context, userID, email, orgID, purpose string) (string, error) {
	token, err := generateSecureCode(32)
	if err != nil {
		return "", err
	}
	if err := s.deps.Q.InsertMFAPendingToken(ctx, db.InsertMFAPendingTokenParams{
		Token:     token,
		UserID:    userID,
		Email:     email,
		OrgID:     orgID,
		ExpiresAt: time.Now().Add(5 * time.Minute),
		Purpose:   purpose,
	}); err != nil {
		return "", err
	}
	return token, nil
}

// issueLocalAuthTokens creates a session and returns access + refresh tokens.
func (s *Server) issueLocalAuthTokens(ctx context.Context, c *app.RequestContext,
	userID, email, orgID string, forcePasswordChange bool) {

	refreshRaw, refreshHash, err := auth.GenerateRefreshToken()
	if err != nil {
		apiInternal(ctx, c, "failed to generate refresh token")
		return
	}

	// The session row comes first so its id can be embedded in the access token
	// as `sid` — that binding is what lets a later revocation actually take the
	// token away.
	sessionID, err := s.deps.Q.CreateSession(ctx, db.CreateSessionParams{
		UserID:           userID,
		TokenHash:        refreshHash,
		AbsLifetimeSecs:  30 * 24 * 3600,
		IdleLifetimeSecs: 7 * 24 * 3600,
	})
	if err != nil {
		apiInternal(ctx, c, "failed to create session")
		return
	}

	claims := &auth.Claims{
		Subject:    email,
		Email:      email,
		OrgID:      orgID,
		Provider:   "local",
		ExternalID: email,
		Principal:  email,
		SessionID:  sessionID,
	}

	accessToken, err := s.deps.Sessions.CreateSession(claims)
	if err != nil {
		apiInternal(ctx, c, "failed to create access token")
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
