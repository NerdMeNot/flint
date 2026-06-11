package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"golang.org/x/oauth2"
)

// deviceCodes stores pending device authorization codes (in-memory, ephemeral).
// In production, consider Redis or a DB table with TTL.
var deviceCodes = struct {
	sync.RWMutex
	codes map[string]*deviceCodeEntry
}{codes: make(map[string]*deviceCodeEntry)}

type deviceCodeEntry struct {
	userCode     string
	expiresAt    time.Time
	interval     int
	accessToken  string // set when user completes auth
	refreshToken string // set when user completes auth
	claims       *auth.Claims
	userID       string // DB user UUID (set after sync)
	completed    bool
	oauthState   string // CSRF state token for OIDC/SAML
	nonce        string // OIDC nonce for replay protection
}

// registerAuthRoutes registers all auth endpoints.
func (s *Server) registerAuthRoutes() {
	s.hertz.POST("/auth/device/code", s.handleDeviceCode)
	s.hertz.POST("/auth/device/token", s.handleDeviceToken)
	s.hertz.POST("/auth/refresh", s.handleRefresh)
	s.hertz.POST("/auth/logout", s.handleLogout)
	s.hertz.GET("/auth/me", s.authMiddleware(), s.handleAuthMe)
	s.hertz.GET("/auth/sessions", s.authMiddleware(), s.handleListSessions)
	s.hertz.DELETE("/auth/sessions/:id", s.authMiddleware(), s.handleRevokeSession)

	// Local auth — no JWT required.
	s.hertz.POST("/auth/login", s.handlePasswordLogin)
	s.hertz.POST("/auth/mfa/verify", s.handleMFAVerify)

	// Password + MFA management — JWT required.
	s.hertz.POST("/auth/change-password", s.authMiddleware(), s.handleChangePassword)
	s.hertz.POST("/auth/mfa/setup", s.authMiddleware(), s.handleMFASetup)
	s.hertz.POST("/auth/mfa/setup/verify", s.authMiddleware(), s.handleMFASetupVerify)
	s.hertz.DELETE("/auth/mfa", s.authMiddleware(), s.handleMFADisable)

	// SSO routes — no JWT required (these establish the JWT).
	s.hertz.GET("/auth/login", s.handleLogin)
	if s.deps.OIDCProvider != nil {
		s.hertz.GET("/auth/oidc/callback", s.handleOIDCCallback)
	}
	if s.deps.SAMLProvider != nil {
		s.hertz.POST("/auth/saml/acs", s.handleSAMLACS)
		s.hertz.GET("/auth/saml/metadata", s.handleSAMLMetadata)
	}
}

// handleDeviceCode initiates the device authorization flow for TUI/CLI.
// Returns a device code and user code that the user enters in a browser.
func (s *Server) handleDeviceCode(ctx context.Context, c *app.RequestContext) {
	deviceCode := generateSecureCode(32)
	userCode := generateUserCode()

	entry := &deviceCodeEntry{
		userCode:  userCode,
		expiresAt: time.Now().Add(15 * time.Minute),
		interval:  5,
	}

	deviceCodes.Lock()
	deviceCodes.codes[deviceCode] = entry
	deviceCodes.Unlock()

	baseURL := s.deps.Config.Server.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	c.JSON(consts.StatusOK, utils.H{
		"deviceCode":      deviceCode,
		"userCode":        userCode,
		"verificationUri": fmt.Sprintf("%s/auth/device", baseURL),
		"expiresIn":       900,
		"interval":        5,
	})
}

// handleDeviceToken polls for device flow completion.
// Returns AUTHORIZATION_PENDING until the user completes auth.
func (s *Server) handleDeviceToken(ctx context.Context, c *app.RequestContext) {
	var req struct {
		DeviceCode string `json:"deviceCode"`
	}
	if err := c.BindJSON(&req); err != nil || req.DeviceCode == "" {
		apiBadRequest(ctx, c, "deviceCode is required")
		return
	}

	deviceCodes.RLock()
	entry, exists := deviceCodes.codes[req.DeviceCode]
	deviceCodes.RUnlock()

	if !exists {
		apiBadRequest(ctx, c, "invalid device code")
		return
	}

	if time.Now().After(entry.expiresAt) {
		deviceCodes.Lock()
		delete(deviceCodes.codes, req.DeviceCode)
		deviceCodes.Unlock()
		apiError(ctx, c, consts.StatusBadRequest, "EXPIRED_TOKEN", "device code expired")
		return
	}

	if !entry.completed {
		apiError(ctx, c, consts.StatusBadRequest, "AUTHORIZATION_PENDING", "waiting for user authorization")
		return
	}

	// Auth complete — return tokens.
	deviceCodes.Lock()
	delete(deviceCodes.codes, req.DeviceCode)
	deviceCodes.Unlock()

	c.JSON(consts.StatusOK, utils.H{
		"accessToken":  entry.accessToken,
		"refreshToken": entry.refreshToken,
		"expiresIn":    900, // 15 minutes
	})
}

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

	// Look up the session to get user info for audit.
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

	c.JSON(consts.StatusOK, utils.H{"status": "logged out"})
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
	}

	// Derive role from Casbin grouping policies.
	roles, _ := s.deps.Enforcer.GetRolesForUser(claims.Email)
	role := ""
	if len(roles) > 0 {
		role = roles[0]
	}

	c.JSON(consts.StatusOK, utils.H{
		"userId":      claims.Subject,
		"email":       claims.Email,
		"name":        claims.Name,
		"orgId":       claims.OrgID,
		"role":        role,
		"permissions": permissions,
		"provider":    claims.Provider,
		"groups":      claims.Groups,
	})
}

// CompleteDeviceAuth is called when a user completes device flow auth (from browser).
// In production, this would be called after OIDC callback validates the user.
func (s *Server) CompleteDeviceAuth(ctx context.Context, deviceCode string, claims *auth.Claims, userID string) error {
	if s.deps.Sessions == nil {
		return fmt.Errorf("sessions not configured")
	}

	accessToken, err := s.deps.Sessions.CreateSession(claims)
	if err != nil {
		return err
	}

	// Create server-side session with refresh token.
	refreshRaw, refreshHash, err := auth.GenerateRefreshToken()
	if err != nil {
		return err
	}

	_, err = s.deps.Q.CreateSession(ctx, db.CreateSessionParams{
		UserID:           userID,
		TokenHash:        refreshHash,
		AbsLifetimeSecs:  30 * 24 * 3600, // 30 days
		IdleLifetimeSecs: 7 * 24 * 3600,  // 7 days
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	deviceCodes.Lock()
	defer deviceCodes.Unlock()

	entry, exists := deviceCodes.codes[deviceCode]
	if !exists {
		return fmt.Errorf("device code not found")
	}

	entry.accessToken = accessToken
	entry.refreshToken = refreshRaw
	entry.userID = userID
	entry.claims = claims
	entry.completed = true

	return nil
}

func generateSecureCode(bytes int) string {
	b := make([]byte, bytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func generateUserCode() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	code := hex.EncodeToString(b)
	return fmt.Sprintf("FLNT-%s", code[:8])
}

// handleLogin initiates SSO authentication for the device flow.
// The user's browser hits this endpoint with their user_code, and is
// redirected to the configured OIDC or SAML provider.
func (s *Server) handleLogin(ctx context.Context, c *app.RequestContext) {
	userCode := string(c.Query("code"))
	if userCode == "" {
		c.HTML(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("Missing code parameter")))
		return
	}

	deviceCode := findDeviceEntryByUserCode(userCode)
	if deviceCode == "" {
		c.HTML(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("Invalid or expired code")))
		return
	}

	// Generate CSRF state and OIDC nonce.
	state := generateSecureCode(32)
	nonce := generateSecureCode(32)

	deviceCodes.Lock()
	entry := deviceCodes.codes[deviceCode]
	entry.oauthState = state
	entry.nonce = nonce
	deviceCodes.Unlock()

	// Redirect to the configured SSO provider.
	if s.deps.OIDCProvider != nil {
		authURL := s.deps.OIDCProvider.AuthURL(state, nonce)
		c.Redirect(consts.StatusFound, []byte(authURL))
		return
	}

	if s.deps.SAMLProvider != nil {
		authURL, err := s.deps.SAMLProvider.AuthURL(state)
		if err != nil {
			logErr(ctx, err, "failed to generate SAML auth URL")
			c.HTML(consts.StatusInternalServerError, "text/html", []byte(authErrorHTML("SAML error")))
			return
		}
		c.Redirect(consts.StatusFound, []byte(authURL))
		return
	}

	c.HTML(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("No SSO provider configured")))
}

// handleOIDCCallback handles the OIDC authorization code callback.
func (s *Server) handleOIDCCallback(ctx context.Context, c *app.RequestContext) {
	code := string(c.Query("code"))
	state := string(c.Query("state"))

	if code == "" || state == "" {
		c.HTML(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("Missing code or state")))
		return
	}

	// Find the device entry by OAuth state.
	deviceCode := findDeviceEntryByState(state)
	if deviceCode == "" {
		c.HTML(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("Invalid or expired state")))
		return
	}

	deviceCodes.RLock()
	entry := deviceCodes.codes[deviceCode]
	nonce := entry.nonce
	deviceCodes.RUnlock()

	// Exchange authorization code for tokens.
	claims, idpToken, err := s.deps.OIDCProvider.Exchange(ctx, code, nonce)
	if err != nil {
		logErr(ctx, err, "OIDC exchange failed")
		c.HTML(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("Authentication failed")))
		return
	}

	if err := s.completeSSOWithToken(ctx, deviceCode, claims, idpToken); err != nil {
		logErr(ctx, err, "SSO completion failed")
		c.HTML(consts.StatusInternalServerError, "text/html", []byte(authErrorHTML("Login failed")))
		return
	}

	c.HTML(consts.StatusOK, "text/html", []byte(authSuccessHTML))
}

// handleSAMLACS handles the SAML Assertion Consumer Service POST.
func (s *Server) handleSAMLACS(ctx context.Context, c *app.RequestContext) {
	samlResponse := string(c.FormValue("SAMLResponse"))
	relayState := string(c.FormValue("RelayState"))

	if samlResponse == "" {
		c.HTML(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("Missing SAML response")))
		return
	}

	// Find the device entry by RelayState (which is our OAuth state).
	deviceCode := findDeviceEntryByState(relayState)
	if deviceCode == "" {
		c.HTML(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("Invalid or expired state")))
		return
	}

	// Validate the SAML response.
	claims, err := s.deps.SAMLProvider.ValidateResponse(samlResponse)
	if err != nil {
		logErr(ctx, err, "SAML validation failed")
		c.HTML(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("Authentication failed")))
		return
	}

	if _, err := s.completeSSO(ctx, deviceCode, claims); err != nil {
		logErr(ctx, err, "SSO completion failed")
		c.HTML(consts.StatusInternalServerError, "text/html", []byte(authErrorHTML("Login failed")))
		return
	}

	c.HTML(consts.StatusOK, "text/html", []byte(authSuccessHTML))
}

// handleSAMLMetadata returns the SP metadata XML.
func (s *Server) handleSAMLMetadata(ctx context.Context, c *app.RequestContext) {
	xml, err := s.deps.SAMLProvider.MetadataXML()
	if err != nil {
		logErr(ctx, err, "failed to generate SAML metadata")
		apiInternal(ctx, c, "metadata generation failed")
		return
	}

	c.SetContentType("application/samlmetadata+xml")
	c.SetStatusCode(consts.StatusOK)
	c.Write(xml) //nolint:errcheck
}

// completeSSO handles the shared logic after OIDC/SAML authentication:
// resolve org, sync user/teams/Casbin, create session, complete device flow.
// completeSSOWithToken handles OIDC completion where we have an IdP OAuth2 token to store.
func (s *Server) completeSSOWithToken(ctx context.Context, deviceCode string, claims *auth.Claims, idpToken *oauth2.Token) error {
	_, err := s.completeSSO(ctx, deviceCode, claims)
	if err != nil {
		return err
	}

	// Store IdP token in the session for sync daemon validation.
	if idpToken != nil && idpToken.RefreshToken != "" {
		tokenJSON, _ := json.Marshal(idpToken)

		// Find the session we just created and store the IdP token.
		deviceCodes.RLock()
		entry := deviceCodes.codes[deviceCode]
		deviceCodes.RUnlock()
		if entry != nil {
			hash := auth.HashToken(entry.refreshToken)
			sess, getErr := s.deps.Q.GetSessionByTokenHash(ctx, hash)
			if getErr == nil {
				_ = s.deps.Q.UpdateSessionIdpToken(ctx, db.UpdateSessionIdpTokenParams{
					ID:          sess.ID,
					IdpTokenEnc: tokenJSON,
				})
			}
		}
	}

	return nil
}

func (s *Server) completeSSO(ctx context.Context, deviceCode string, claims *auth.Claims) (string, error) {
	// Get the single org (auto-created at boot).
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		return "", fmt.Errorf("getting org: %w", err)
	}
	claims.OrgID = org.ID

	// Sync user, teams, and Casbin assignments.
	userID, err := auth.SyncUserOnLogin(ctx, s.deps.Q, s.deps.DB, s.deps.Enforcer,
		org.ID, claims, s.deps.Config.Auth.AdminUsers, s.deps.Config.Auth.DefaultRoleOrFallback())
	if err != nil {
		return "", fmt.Errorf("syncing user: %w", err)
	}

	// Audit: login event.
	_ = s.deps.Q.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		OrgID:        org.ID,
		UserID:       &userID,
		Action:       "auth.login",
		ResourceType: "session",
	})

	if err := s.CompleteDeviceAuth(ctx, deviceCode, claims, userID); err != nil {
		return "", err
	}

	return userID, nil
}

// findDeviceEntryByState finds a device code by matching oauthState.
func findDeviceEntryByState(state string) string {
	deviceCodes.RLock()
	defer deviceCodes.RUnlock()

	for code, entry := range deviceCodes.codes {
		if entry.oauthState == state && time.Now().Before(entry.expiresAt) {
			return code
		}
	}
	return ""
}

// findDeviceEntryByUserCode finds a device code by matching userCode.
func findDeviceEntryByUserCode(userCode string) string {
	deviceCodes.RLock()
	defer deviceCodes.RUnlock()

	for code, entry := range deviceCodes.codes {
		if entry.userCode == userCode && time.Now().Before(entry.expiresAt) {
			return code
		}
	}
	return ""
}

// ── Local Auth ────────────────────────────────────────────────

// mfaPendingTokens stores temporary tokens for the MFA verification step.
var mfaPendingTokens = struct {
	sync.RWMutex
	tokens map[string]*mfaPendingEntry
}{tokens: make(map[string]*mfaPendingEntry)}

type mfaPendingEntry struct {
	userID    string
	email     string
	orgID     string
	expiresAt time.Time
}

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

	// Look up user.
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "org not configured")
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
		apiUnauthorized(ctx, c, "invalid credentials")
		return
	}

	if !user.IsActive {
		apiUnauthorized(ctx, c, "account is deactivated")
		return
	}

	// Verify password (Argon2id).
	if !auth.VerifyPassword(req.Password, *user.PasswordHash) {
		_ = s.deps.Q.RecordLoginAttempt(ctx, db.RecordLoginAttemptParams{
			Email: req.Email, IpAddress: ipAddr, Success: false,
		})
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
		mfaPendingTokens.Lock()
		mfaPendingTokens.tokens[token] = &mfaPendingEntry{
			userID:    user.ID,
			email:     user.Email,
			orgID:     org.ID,
			expiresAt: time.Now().Add(5 * time.Minute),
		}
		mfaPendingTokens.Unlock()

		c.JSON(consts.StatusOK, utils.H{
			"mfaRequired": true,
			"mfaToken":    token,
		})
		return
	}

	// No MFA — issue tokens directly.
	s.issueLocalAuthTokens(ctx, c, user.ID, user.Email, org.ID, user.ForcePasswordChange)
}

// handleMFAVerify completes the second phase of login with a TOTP code.
func (s *Server) handleMFAVerify(ctx context.Context, c *app.RequestContext) {
	var req struct {
		MFAToken     string `json:"mfaToken"`
		Code         string `json:"code"`
		RecoveryCode string `json:"recoveryCode"`
	}
	if err := c.BindJSON(&req); err != nil || req.MFAToken == "" {
		apiBadRequest(ctx, c, "mfaToken is required")
		return
	}
	if req.Code == "" && req.RecoveryCode == "" {
		apiBadRequest(ctx, c, "code or recoveryCode is required")
		return
	}

	// Look up pending MFA entry.
	mfaPendingTokens.RLock()
	entry, exists := mfaPendingTokens.tokens[req.MFAToken]
	mfaPendingTokens.RUnlock()

	if !exists || time.Now().After(entry.expiresAt) {
		apiUnauthorized(ctx, c, "invalid or expired MFA token")
		return
	}

	// Get user's TOTP secret.
	user, err := s.deps.Q.GetUserForAuth(ctx, db.GetUserForAuthParams{
		OrgID: entry.orgID, Email: entry.email,
	})
	if err != nil || user.TotpSecretEnc == nil {
		apiUnauthorized(ctx, c, "MFA not configured")
		return
	}

	valid := false

	if req.Code != "" {
		// Validate TOTP code.
		valid = auth.ValidateTOTPCode(string(user.TotpSecretEnc), req.Code)
	} else if req.RecoveryCode != "" {
		// Validate recovery code (single-use).
		codes, _ := s.deps.Q.GetUserRecoveryCodes(ctx, user.ID)
		var remaining []string
		remaining, valid = auth.ValidateRecoveryCode(req.RecoveryCode, codes)
		if valid {
			_ = s.deps.Q.SetUserRecoveryCodes(ctx, db.SetUserRecoveryCodesParams{
				ID:            user.ID,
				RecoveryCodes: remaining,
			})
		}
	}

	if !valid {
		apiUnauthorized(ctx, c, "invalid code")
		return
	}

	// Remove used MFA token.
	mfaPendingTokens.Lock()
	delete(mfaPendingTokens.tokens, req.MFAToken)
	mfaPendingTokens.Unlock()

	// Issue tokens.
	s.issueLocalAuthTokens(ctx, c, entry.userID, entry.email, entry.orgID, false)
}

// handleMFASetup generates a new TOTP secret and returns the QR code URL.
func (s *Server) handleMFASetup(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	if claims == nil {
		apiUnauthorized(ctx, c, "not authenticated")
		return
	}

	issuer := "Flint"
	if s.deps.Config != nil && s.deps.Config.Server.BaseURL != "" {
		issuer = s.deps.Config.Server.BaseURL
	}

	key, err := auth.GenerateTOTPSecret(claims.Email, issuer)
	if err != nil {
		apiInternal(ctx, c, "failed to generate TOTP secret")
		return
	}

	// Store the secret (unverified — user must confirm with a code first).
	user, err := s.deps.Q.GetUserByEmail(ctx, db.GetUserByEmailParams{
		OrgID: claims.OrgID, Email: claims.Email,
	})
	if err != nil {
		apiNotFound(ctx, c, "user not found")
		return
	}

	_ = s.deps.Q.SetUserTOTPSecret(ctx, db.SetUserTOTPSecretParams{
		ID:            user.ID,
		TotpSecretEnc: []byte(key.Secret()),
	})

	// Generate recovery codes.
	rawCodes, hashedCodes, err := auth.GenerateRecoveryCodes(8)
	if err != nil {
		apiInternal(ctx, c, "failed to generate recovery codes")
		return
	}

	// Store hashed recovery codes.
	_ = s.deps.Q.SetUserRecoveryCodes(ctx, db.SetUserRecoveryCodesParams{
		ID:            user.ID,
		RecoveryCodes: hashedCodes,
	})

	c.JSON(consts.StatusOK, utils.H{
		"secret":        key.Secret(),
		"qrCodeURL":     key.URL(),
		"recoveryCodes": rawCodes,
	})
}

// handleMFASetupVerify confirms the TOTP setup by validating a code.
func (s *Server) handleMFASetupVerify(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	if claims == nil {
		apiUnauthorized(ctx, c, "not authenticated")
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := c.BindJSON(&req); err != nil || req.Code == "" {
		apiBadRequest(ctx, c, "code is required")
		return
	}

	user, err := s.deps.Q.GetUserForAuth(ctx, db.GetUserForAuthParams{
		OrgID: claims.OrgID, Email: claims.Email,
	})
	if err != nil || user.TotpSecretEnc == nil {
		apiNotFound(ctx, c, "TOTP not set up — call /auth/mfa/setup first")
		return
	}

	if !auth.ValidateTOTPCode(string(user.TotpSecretEnc), req.Code) {
		apiBadRequest(ctx, c, "invalid code — scan the QR code and try again")
		return
	}

	_ = s.deps.Q.VerifyUserTOTP(ctx, user.ID)

	// Audit.
	org, _ := s.deps.Q.GetOrg(ctx)
	_ = s.deps.Q.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		OrgID: org.ID, UserID: &user.ID,
		Action: "auth.mfa.enabled", ResourceType: "user",
	})

	c.JSON(consts.StatusOK, utils.H{"status": "mfa_enabled"})
}

// handleMFADisable disables MFA for the current user.
func (s *Server) handleMFADisable(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	if claims == nil {
		apiUnauthorized(ctx, c, "not authenticated")
		return
	}

	var req struct {
		Code         string `json:"code"`
		RecoveryCode string `json:"recoveryCode"`
	}
	if err := c.BindJSON(&req); err != nil || (req.Code == "" && req.RecoveryCode == "") {
		apiBadRequest(ctx, c, "code or recoveryCode is required to disable MFA")
		return
	}

	user, err := s.deps.Q.GetUserForAuth(ctx, db.GetUserForAuthParams{
		OrgID: claims.OrgID, Email: claims.Email,
	})
	if err != nil || user.TotpSecretEnc == nil {
		apiNotFound(ctx, c, "MFA not enabled")
		return
	}

	// Verify current code before disabling.
	valid := false
	if req.Code != "" {
		valid = auth.ValidateTOTPCode(string(user.TotpSecretEnc), req.Code)
	} else if req.RecoveryCode != "" {
		codes, _ := s.deps.Q.GetUserRecoveryCodes(ctx, user.ID)
		_, valid = auth.ValidateRecoveryCode(req.RecoveryCode, codes)
	}

	if !valid {
		apiUnauthorized(ctx, c, "invalid code")
		return
	}

	_ = s.deps.Q.ClearUserTOTP(ctx, user.ID)

	// Audit.
	org, _ := s.deps.Q.GetOrg(ctx)
	_ = s.deps.Q.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		OrgID: org.ID, UserID: &user.ID,
		Action: "auth.mfa.disabled", ResourceType: "user",
	})

	c.JSON(consts.StatusOK, utils.H{"status": "mfa_disabled"})
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

	// Audit.
	_ = s.deps.Q.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		OrgID: orgID, UserID: &userID,
		Action: "auth.login", ResourceType: "session",
	})

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

const authSuccessHTML = `<!DOCTYPE html>
<html><head><title>Flint - Authentication Successful</title>
<style>body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#0f172a;color:#e2e8f0}
.card{text-align:center;padding:3rem;border-radius:12px;background:#1e293b;max-width:400px}
h1{color:#22c55e;font-size:1.5rem;margin-bottom:0.5rem}
p{color:#94a3b8;margin-top:0.5rem}</style></head>
<body><div class="card"><h1>Authentication Successful</h1>
<p>You can close this tab and return to the CLI.</p></div></body></html>`

func authErrorHTML(msg string) string {
	return `<!DOCTYPE html>
<html><head><title>Flint - Authentication Error</title>
<style>body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#0f172a;color:#e2e8f0}
.card{text-align:center;padding:3rem;border-radius:12px;background:#1e293b;max-width:400px}
h1{color:#ef4444;font-size:1.5rem;margin-bottom:0.5rem}
p{color:#94a3b8;margin-top:0.5rem}</style></head>
<body><div class="card"><h1>Authentication Error</h1>
<p>` + msg + `</p></div></body></html>`
}

// extractClientIP extracts and parses the client IP from the request.
func extractClientIP(c *app.RequestContext) netip.Addr {
	ipStr := string(c.GetHeader("X-Real-IP"))
	if ipStr == "" {
		ipStr = string(c.GetHeader("X-Forwarded-For"))
		if idx := strings.Index(ipStr, ","); idx > 0 {
			ipStr = strings.TrimSpace(ipStr[:idx])
		}
	}
	if ipStr == "" {
		ipStr = c.RemoteAddr().String()
	}

	// Try parsing directly first.
	if addr, err := netip.ParseAddr(ipStr); err == nil {
		return addr
	}

	// Strip port if present (e.g., "1.2.3.4:8080" or "[::1]:8080").
	if host, _, err := net.SplitHostPort(ipStr); err == nil {
		if addr, err := netip.ParseAddr(host); err == nil {
			return addr
		}
	}

	return netip.Addr{} // zero value — rate limiting still works, just less precise
}

// startDeviceCodeCleanup runs a background goroutine to clean up expired device codes.
func startDeviceCodeCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now()
				deviceCodes.Lock()
				for code, entry := range deviceCodes.codes {
					if now.After(entry.expiresAt) {
						delete(deviceCodes.codes, code)
					}
				}
				deviceCodes.Unlock()
			}

			// Also clean up expired MFA tokens.
			mfaPendingTokens.Lock()
			now := time.Now()
			for token, entry := range mfaPendingTokens.tokens {
				if now.After(entry.expiresAt) {
					delete(mfaPendingTokens.tokens, token)
				}
			}
			mfaPendingTokens.Unlock()
		}
	}()
}

// logErr logs an error with context.
func logErr(ctx context.Context, err error, msg string) {
	logger := observe.Logger(ctx)
	logger.Error().Err(err).Msg(msg)
}

// Ensure observe is used.
var _ = observe.RequestID
