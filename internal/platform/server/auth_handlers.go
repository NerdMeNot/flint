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
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/oauth2"
)

// Device authorization and MFA-pending state are stored in Postgres (see
// device_codes / mfa_pending_tokens) rather than process memory, so the auth
// layer is correct under concurrency, survives restarts, and works across
// replicas. See the auth_stores migration.

const deviceCodeTTL = 15 * time.Minute

// registerAuthRoutes registers all auth endpoints.
func (s *Server) registerAuthRoutes() {
	s.hertz.POST("/auth/device/code", s.handleDeviceCode)
	s.hertz.POST("/auth/device/token", s.handleDeviceToken)
	s.hertz.POST("/auth/refresh", s.handleRefresh)
	s.hertz.POST("/auth/logout", s.handleLogout)
	s.hertz.GET("/auth/me", s.authMiddleware(), s.handleAuthMe)
	s.hertz.PUT("/auth/profile", s.authMiddleware(), s.handleUpdateProfile)
	s.hertz.GET("/auth/sessions", s.authMiddleware(), s.handleListSessions)
	s.hertz.DELETE("/auth/sessions/:id", s.authMiddleware(), s.handleRevokeSession)

	// Local auth — no JWT required.
	s.hertz.POST("/auth/login", s.handlePasswordLogin)
	s.hertz.POST("/auth/mfa/verify", s.handleMFAVerify)

	// Password + MFA management — JWT required.
	s.hertz.POST("/auth/change-password", s.authMiddleware(), s.handleChangePassword)
	s.hertz.POST("/auth/mfa/setup", s.authMiddleware(), s.handleMFASetup)
	s.hertz.POST("/auth/mfa/setup/verify", s.authMiddleware(), s.handleMFASetupVerify)
	s.hertz.POST("/auth/mfa/recovery-codes", s.authMiddleware(), s.handleRegenerateRecoveryCodes)
	s.hertz.DELETE("/auth/mfa", s.authMiddleware(), s.handleMFADisable)

	// SSO routes — no JWT required (these establish the JWT). Registered
	// unconditionally so that providers configured via the API (hot-reloaded
	// after boot) have working callbacks without a restart; the handlers guard
	// on the provider being configured at request time. Per-IP rate limited to
	// blunt brute-force/DoS against the unauthenticated auth surface.
	authLimit := s.ipRateLimit(newIPRateLimiter(5, 10))
	s.hertz.GET("/auth/login", authLimit, s.handleLogin)
	s.hertz.GET("/auth/oidc/callback", authLimit, s.handleOIDCCallback)
	s.hertz.POST("/auth/saml/acs", authLimit, s.handleSAMLACS)
	s.hertz.GET("/auth/saml/metadata", s.handleSAMLMetadata)

	// Single Logout endpoints: the post-logout redirect target (OIDC) and the
	// SAML SingleLogout service (SP-initiated response + IdP-initiated request).
	s.hertz.GET("/auth/oidc/logout-complete", s.handleOIDCLogoutComplete)
	s.hertz.GET("/auth/saml/slo", authLimit, s.handleSAMLSLO)
	s.hertz.POST("/auth/saml/slo", authLimit, s.handleSAMLSLO)
}

// auditEvent writes a best-effort audit entry that is NOT tied to an
// authenticated user JWT — used for failed logins and SCIM provisioning.
// userID/resourceID may be empty (stored as NULL).
func (s *Server) auditEvent(ctx context.Context, orgID, userID, action, resourceType, resourceID string, ip netip.Addr) {
	var uid, rid *string
	if userID != "" {
		uid = &userID
	}
	if resourceID != "" {
		rid = &resourceID
	}
	_ = s.deps.Q.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		OrgID:        orgID,
		UserID:       uid,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   rid,
		Column6:      ip,
	})
}

// auditLoginFailure records a failed SSO login with a short reason code and the
// raw error detail (stored in metadata) so the sign-in log can show exactly which
// field/check failed.
func (s *Server) auditLoginFailure(ctx context.Context, c *app.RequestContext, reason, detail string) {
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		return
	}
	meta, _ := json.Marshal(map[string]string{"reason": reason, "detail": detail})
	rid := reason
	_ = s.deps.Q.InsertAuditEntryWithMeta(ctx, db.InsertAuditEntryWithMetaParams{
		OrgID:        org.ID,
		Action:       "auth.login.failed",
		ResourceType: "session",
		ResourceID:   &rid,
		Column6:      extractClientIP(c),
		Metadata:     meta,
	})
}

// handleSignInLog returns the recent SSO sign-in attempts (success + failure)
// with field-level diagnostics — the per-connection sign-in history.
func (s *Server) handleSignInLog(ctx context.Context, c *app.RequestContext) {
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to load org")
		return
	}
	rows, err := s.deps.Q.ListRecentSignIns(ctx, db.ListRecentSignInsParams{OrgID: org.ID, Limit: 30})
	if err != nil {
		logErr(ctx, err, "list sign-ins")
		apiInternal(ctx, c, "failed to list sign-ins")
		return
	}
	events := make([]utils.H, 0, len(rows))
	for _, r := range rows {
		e := utils.H{
			"time":   r.CreatedAt.UTC().Format(time.RFC3339),
			"result": "success",
			"email":  derefStr(r.UserEmail),
			"ip":     r.IpAddress,
		}
		if r.Action == "auth.login.failed" {
			e["result"] = "failure"
			var m struct {
				Reason string `json:"reason"`
				Detail string `json:"detail"`
			}
			_ = json.Unmarshal(r.Metadata, &m)
			e["reason"] = m.Reason
			e["detail"] = m.Detail
			e["summary"] = classifyAuthError(m.Detail, m.Reason)
		}
		events = append(events, e)
	}
	c.JSON(consts.StatusOK, utils.H{"events": events})
}

// classifyAuthError turns a raw provider error into a short, human-readable,
// field-level explanation (the WorkOS/Scalekit diagnostics model).
func classifyAuthError(detail, reason string) string {
	d := strings.ToLower(detail)
	switch {
	case reason == "no_provider":
		return "No SSO provider configured"
	case strings.Contains(d, "audience"):
		return "Audience (SP Entity ID) mismatch"
	case strings.Contains(d, "nonce"):
		return "Nonce mismatch (possible replay)"
	case strings.Contains(d, "destination") || strings.Contains(d, "recipient"):
		return "ACS URL / destination mismatch"
	case strings.Contains(d, "signature") || strings.Contains(d, "verifying"):
		return "Signature verification failed"
	case strings.Contains(d, "expired") || strings.Contains(d, "notonorafter") || strings.Contains(d, "clock") || strings.Contains(d, "issue delay"):
		return "Assertion expired or clock skew"
	case strings.Contains(d, "email"):
		return "No email returned by the IdP"
	case strings.Contains(d, "issuer") || strings.Contains(d, "iss "):
		return "Issuer mismatch"
	default:
		return "Authentication failed"
	}
}

// handleDeviceCode initiates the device authorization flow for TUI/CLI.
// Returns a device code and user code that the user enters in a browser.
func (s *Server) handleDeviceCode(ctx context.Context, c *app.RequestContext) {
	deviceCode := generateSecureCode(32)
	userCode := generateUserCode()

	if err := s.deps.Q.InsertDeviceCode(ctx, db.InsertDeviceCodeParams{
		DeviceCode:   deviceCode,
		UserCode:     userCode,
		ExpiresAt:    time.Now().Add(deviceCodeTTL),
		IntervalSecs: 5,
	}); err != nil {
		apiInternal(ctx, c, "failed to create device code")
		return
	}

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

	row, err := s.deps.Q.GetDeviceCode(ctx, req.DeviceCode)
	if err != nil {
		apiBadRequest(ctx, c, "invalid device code")
		return
	}

	if time.Now().After(row.ExpiresAt) {
		_ = s.deps.Q.DeleteDeviceCode(ctx, req.DeviceCode)
		apiError(ctx, c, consts.StatusBadRequest, "EXPIRED_TOKEN", "device code expired")
		return
	}

	// Enforce the polling interval (OAuth slow_down). Measured from the last
	// accepted poll; too-fast polls are rejected without advancing the marker.
	if row.LastPolledAt != nil &&
		time.Since(*row.LastPolledAt) < time.Duration(row.IntervalSecs)*time.Second {
		apiError(ctx, c, consts.StatusBadRequest, "SLOW_DOWN", "polling too frequently")
		return
	}
	_ = s.deps.Q.TouchDeviceCodePoll(ctx, req.DeviceCode)

	if !row.Completed {
		apiError(ctx, c, consts.StatusBadRequest, "AUTHORIZATION_PENDING", "waiting for user authorization")
		return
	}

	// Auth complete — atomically claim (return + delete) the tokens. The atomic
	// DELETE ... RETURNING closes the TOCTOU window the in-memory map had.
	tok, err := s.deps.Q.ClaimCompletedDeviceCode(ctx, req.DeviceCode)
	if err != nil {
		// Lost a race to another poll, or already claimed.
		apiError(ctx, c, consts.StatusBadRequest, "AUTHORIZATION_PENDING", "waiting for user authorization")
		return
	}

	c.JSON(consts.StatusOK, utils.H{
		"accessToken":  derefStr(tok.AccessToken),
		"refreshToken": derefStr(tok.RefreshToken),
		"expiresIn":    900, // 15 minutes
	})
}

// derefStr returns the value of a *string, or "" if nil.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
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

	uid := userID
	if err := s.deps.Q.CompleteDeviceCode(ctx, db.CompleteDeviceCodeParams{
		DeviceCode:   deviceCode,
		AccessToken:  &accessToken,
		RefreshToken: &refreshRaw,
		UserID:       &uid,
	}); err != nil {
		return fmt.Errorf("complete device code: %w", err)
	}
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
		c.Data(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("Missing code parameter")))
		return
	}

	deviceCode, err := s.deps.Q.FindDeviceCodeByUserCode(ctx, userCode)
	if err != nil || deviceCode == "" {
		c.Data(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("Invalid or expired code")))
		return
	}

	// Generate CSRF state, OIDC nonce, and a PKCE (S256) verifier.
	state := generateSecureCode(32)
	nonce := generateSecureCode(32)
	codeVerifier := auth.GeneratePKCEVerifier()

	if err := s.deps.Q.SetDeviceCodeOAuthState(ctx, db.SetDeviceCodeOAuthStateParams{
		DeviceCode:   deviceCode,
		OauthState:   &state,
		Nonce:        &nonce,
		CodeVerifier: &codeVerifier,
	}); err != nil {
		c.Data(consts.StatusInternalServerError, "text/html", []byte(authErrorHTML("Login failed")))
		return
	}

	// Redirect to the configured SSO provider.
	if s.deps.OIDCProvider != nil {
		authURL := s.deps.OIDCProvider.AuthURL(state, nonce, codeVerifier)
		c.Redirect(consts.StatusFound, []byte(authURL))
		return
	}

	if s.deps.SAMLProvider != nil {
		authURL, err := s.deps.SAMLProvider.AuthURL(state)
		if err != nil {
			logErr(ctx, err, "failed to generate SAML auth URL")
			c.Data(consts.StatusInternalServerError, "text/html", []byte(authErrorHTML("SAML error")))
			return
		}
		c.Redirect(consts.StatusFound, []byte(authURL))
		return
	}

	s.auditLoginFailure(ctx, c, "no_provider", "")
	c.Data(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("No SSO provider configured")))
}

// handleOIDCCallback handles the OIDC authorization code callback.
func (s *Server) handleOIDCCallback(ctx context.Context, c *app.RequestContext) {
	code := string(c.Query("code"))
	state := string(c.Query("state"))

	if code == "" || state == "" {
		c.Data(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("Missing code or state")))
		return
	}

	// Decoded test sign-in: uses the test's own provider, works even when no
	// provider is active yet, and never creates a session.
	if tl, ok := testLogins.get(state); ok && tl.protocol == "oidc" {
		s.completeOIDCTestLogin(ctx, c, tl, code)
		return
	}

	if s.deps.OIDCProvider == nil {
		c.Data(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("OIDC is not configured")))
		return
	}

	// Find the device entry by OAuth state.
	deviceCode, err := s.deps.Q.FindDeviceCodeByOAuthState(ctx, &state)
	if err != nil || deviceCode == "" {
		c.Data(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("Invalid or expired state")))
		return
	}

	noncePtr, _ := s.deps.Q.GetDeviceCodeNonce(ctx, deviceCode)
	nonce := derefStr(noncePtr)
	verifierPtr, _ := s.deps.Q.GetDeviceCodeCodeVerifier(ctx, deviceCode)
	codeVerifier := derefStr(verifierPtr)

	// Exchange authorization code for tokens (presents the PKCE verifier).
	claims, idpToken, err := s.deps.OIDCProvider.Exchange(ctx, code, nonce, codeVerifier)
	if err != nil {
		logErr(ctx, err, "OIDC exchange failed")
		s.auditLoginFailure(ctx, c, "oidc:exchange_failed", err.Error())
		c.Data(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("Authentication failed")))
		return
	}

	if err := s.completeSSOWithToken(ctx, deviceCode, claims, idpToken); err != nil {
		logErr(ctx, err, "SSO completion failed")
		c.Data(consts.StatusInternalServerError, "text/html", []byte(authErrorHTML("Login failed")))
		return
	}

	c.Data(consts.StatusOK, "text/html", []byte(authSuccessHTML))
}

// handleSAMLACS handles the SAML Assertion Consumer Service POST.
func (s *Server) handleSAMLACS(ctx context.Context, c *app.RequestContext) {
	samlResponse := string(c.FormValue("SAMLResponse"))
	relayState := string(c.FormValue("RelayState"))

	if samlResponse == "" {
		c.Data(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("Missing SAML response")))
		return
	}

	// Decoded test sign-in branch (uses the test's own provider).
	if tl, ok := testLogins.get(relayState); ok && tl.protocol == "saml" {
		s.completeSAMLTestLogin(c, tl, samlResponse)
		return
	}

	if s.deps.SAMLProvider == nil {
		c.Data(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("SAML is not configured")))
		return
	}

	// Find the device entry by RelayState (which is our OAuth state).
	deviceCode, err := s.deps.Q.FindDeviceCodeByOAuthState(ctx, &relayState)
	if err != nil || deviceCode == "" {
		c.Data(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("Invalid or expired state")))
		return
	}

	// Validate the SAML response.
	claims, err := s.deps.SAMLProvider.ValidateResponse(samlResponse)
	if err != nil {
		logErr(ctx, err, "SAML validation failed")
		s.auditLoginFailure(ctx, c, "saml:validation_failed", err.Error())
		c.Data(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("Authentication failed")))
		return
	}

	if _, err := s.completeSSO(ctx, deviceCode, claims); err != nil {
		logErr(ctx, err, "SSO completion failed")
		c.Data(consts.StatusInternalServerError, "text/html", []byte(authErrorHTML("Login failed")))
		return
	}

	// Store NameID + SessionIndex as SAML Single Logout material.
	if claims.Subject != "" {
		s.persistLogoutState(ctx, deviceCode, ssoLogoutState{
			Provider: "saml", NameID: claims.Subject, SessionIndex: claims.SessionIndex,
		})
	}

	c.Data(consts.StatusOK, "text/html", []byte(authSuccessHTML))
}

// handleSAMLMetadata returns the SP metadata XML.
func (s *Server) handleSAMLMetadata(ctx context.Context, c *app.RequestContext) {
	if s.deps.SAMLProvider == nil {
		apiNotFound(ctx, c, "SAML is not configured")
		return
	}

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

	// Store the ID token as RP-initiated logout material (id_token_hint), so a
	// later logout can terminate the upstream OIDC session (Single Logout).
	if idpToken != nil {
		if rawID, ok := idpToken.Extra("id_token").(string); ok && rawID != "" {
			s.persistLogoutState(ctx, deviceCode, ssoLogoutState{Provider: "oidc", IDToken: rawID})
		}
	}

	// Store IdP token in the session for sync daemon validation — envelope-
	// encrypted at rest. If no master key is configured, skip storing it rather
	// than persisting a long-lived refresh token in plaintext.
	masterKey, mkErr := s.deps.Config.Encryption.DecodeMasterKey()
	if idpToken != nil && idpToken.RefreshToken != "" && mkErr == nil {
		tokenEnc, encErr := auth.EncryptIdpToken(idpToken, masterKey)

		// Find the session we just created and store the IdP token.
		refreshPtr, _ := s.deps.Q.GetDeviceCodeRefreshToken(ctx, deviceCode)
		if refresh := derefStr(refreshPtr); refresh != "" && encErr == nil {
			hash := auth.HashToken(refresh)
			sess, getErr := s.deps.Q.GetSessionByTokenHash(ctx, hash)
			if getErr == nil {
				_ = s.deps.Q.UpdateSessionIdpToken(ctx, db.UpdateSessionIdpTokenParams{
					ID:          sess.ID,
					IdpTokenEnc: tokenEnc,
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
		org.ID, claims, s.deps.Config.Auth.AdminUsers, s.deps.Config.Auth.DefaultRoleOrFallback(), org.SsoStrictGroups)
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

// ── Local Auth ────────────────────────────────────────────────

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

	// Look up pending MFA entry (the query filters out expired tokens).
	entry, err := s.deps.Q.GetMFAPendingToken(ctx, req.MFAToken)
	if err != nil {
		apiUnauthorized(ctx, c, "invalid or expired MFA token")
		return
	}

	// Get user's TOTP secret.
	user, err := s.deps.Q.GetUserForAuth(ctx, db.GetUserForAuthParams{
		OrgID: entry.OrgID, Email: entry.Email,
	})
	if err != nil || user.TotpSecretEnc == nil {
		apiUnauthorized(ctx, c, "MFA not configured")
		return
	}

	valid := false

	if req.Code != "" {
		// Validate TOTP code, then guard against replay (see checkTOTPReplay).
		valid = auth.ValidateTOTPCode(string(user.TotpSecretEnc), req.Code) &&
			s.checkTOTPReplay(ctx, user.ID)
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
	_ = s.deps.Q.DeleteMFAPendingToken(ctx, req.MFAToken)

	// Issue tokens.
	s.issueLocalAuthTokens(ctx, c, entry.UserID, entry.Email, entry.OrgID, false)
}

// checkTOTPReplay records the current TOTP period for the user and returns true
// only if it is strictly newer than the last accepted one — rejecting reuse of a
// code within (or before) its validity window. Replica-safe: the advance is a
// single atomic UPDATE.
func (s *Server) checkTOTPReplay(ctx context.Context, userID string) bool {
	period := auth.TOTPPeriod(time.Now())
	_, err := s.deps.Q.RecordTOTPUse(ctx, db.RecordTOTPUseParams{
		ID:                userID,
		MfaLastUsedPeriod: pgtype.Int8{Int64: period, Valid: true},
	})
	return err == nil
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

	if !auth.ValidateTOTPCode(string(user.TotpSecretEnc), req.Code) || !s.checkTOTPReplay(ctx, user.ID) {
		apiBadRequest(ctx, c, "invalid code — scan the QR code and try again")
		return
	}

	_ = s.deps.Q.VerifyUserTOTP(ctx, user.ID)

	// Recovery codes were already generated, stored, and shown to the user at
	// /auth/mfa/setup; do not regenerate here (that would invalidate the codes
	// they just saved). Rotation is available via /auth/mfa/recovery-codes.

	// Audit.
	org, _ := s.deps.Q.GetOrg(ctx)
	_ = s.deps.Q.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		OrgID: org.ID, UserID: &user.ID,
		Action: "auth.mfa.enabled", ResourceType: "user",
	})

	c.JSON(consts.StatusOK, utils.H{"status": "mfa_enabled"})
}

// handleRegenerateRecoveryCodes issues a fresh set of one-time recovery codes,
// invalidating the previous set. Requires a valid current TOTP or recovery code
// so a hijacked session can't silently rotate the user's break-glass codes.
func (s *Server) handleRegenerateRecoveryCodes(ctx context.Context, c *app.RequestContext) {
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
		apiBadRequest(ctx, c, "code or recoveryCode is required")
		return
	}

	user, err := s.deps.Q.GetUserForAuth(ctx, db.GetUserForAuthParams{
		OrgID: claims.OrgID, Email: claims.Email,
	})
	if err != nil || user.TotpSecretEnc == nil {
		apiNotFound(ctx, c, "MFA is not configured")
		return
	}

	// Re-authenticate the second factor before rotating the codes.
	valid := false
	if req.Code != "" {
		valid = auth.ValidateTOTPCode(string(user.TotpSecretEnc), req.Code) && s.checkTOTPReplay(ctx, user.ID)
	} else {
		codes, _ := s.deps.Q.GetUserRecoveryCodes(ctx, user.ID)
		if _, ok := auth.ValidateRecoveryCode(req.RecoveryCode, codes); ok {
			valid = true
		}
	}
	if !valid {
		apiUnauthorized(ctx, c, "invalid code")
		return
	}

	raw, hashed, err := auth.GenerateRecoveryCodes(10)
	if err != nil {
		apiInternal(ctx, c, "failed to generate recovery codes")
		return
	}
	if err := s.deps.Q.SetUserRecoveryCodes(ctx, db.SetUserRecoveryCodesParams{
		ID: user.ID, RecoveryCodes: hashed,
	}); err != nil {
		apiInternal(ctx, c, "failed to store recovery codes")
		return
	}

	org, _ := s.deps.Q.GetOrg(ctx)
	_ = s.deps.Q.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		OrgID: org.ID, UserID: &user.ID,
		Action: "auth.mfa.recovery_codes.regenerated", ResourceType: "user",
	})
	c.JSON(consts.StatusOK, utils.H{"recoveryCodes": raw})
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
		valid = auth.ValidateTOTPCode(string(user.TotpSecretEnc), req.Code) && s.checkTOTPReplay(ctx, user.ID)
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

// StartAuthStoreCleanup runs a background goroutine that periodically prunes
// expired device codes and MFA-pending tokens from Postgres. Call once at
// startup with a process-lifetime context; the goroutine exits on ctx.Done.
// (The old in-memory cleanup function was defined but never invoked — a leak.)
func (s *Server) StartAuthStoreCleanup(ctx context.Context) {
	if s.deps.Q == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.deps.Q.DeleteExpiredDeviceCodes(ctx)
				_ = s.deps.Q.DeleteExpiredMFAPendingTokens(ctx)
			}
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
