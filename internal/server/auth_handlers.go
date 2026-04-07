package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/NerdMeNot/flint/internal/auth"
	"github.com/NerdMeNot/flint/internal/observe"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// deviceCodes stores pending device authorization codes (in-memory, ephemeral).
// In production, consider Redis or a DB table with TTL.
var deviceCodes = struct {
	sync.RWMutex
	codes map[string]*deviceCodeEntry
}{codes: make(map[string]*deviceCodeEntry)}

type deviceCodeEntry struct {
	userCode    string
	expiresAt   time.Time
	interval    int
	accessToken string // set when user completes auth
	claims      *auth.Claims
	completed   bool
	oauthState  string // CSRF state token for OIDC/SAML
	nonce       string // OIDC nonce for replay protection
}

// registerAuthRoutes registers all auth endpoints.
func (s *Server) registerAuthRoutes() {
	s.hertz.POST("/auth/device/code", s.handleDeviceCode)
	s.hertz.POST("/auth/device/token", s.handleDeviceToken)
	s.hertz.POST("/auth/refresh", s.handleRefresh)
	s.hertz.GET("/auth/me", s.authMiddleware(), s.handleAuthMe)

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
		"refreshToken": "", // TODO: implement refresh tokens
		"expiresIn":    86400,
	})
}

// handleRefresh exchanges a refresh token for a new JWT.
func (s *Server) handleRefresh(ctx context.Context, c *app.RequestContext) {
	var req struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := c.BindJSON(&req); err != nil || req.RefreshToken == "" {
		apiBadRequest(ctx, c, "refreshToken is required")
		return
	}

	// TODO: Implement refresh token validation and rotation.
	// For now, return a helpful error.
	apiError(ctx, c, consts.StatusNotImplemented, "NOT_IMPLEMENTED", "refresh tokens not yet implemented")
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
func (s *Server) CompleteDeviceAuth(deviceCode string, claims *auth.Claims) error {
	if s.deps.Sessions == nil {
		return fmt.Errorf("sessions not configured")
	}

	token, err := s.deps.Sessions.CreateSession(claims)
	if err != nil {
		return err
	}

	deviceCodes.Lock()
	defer deviceCodes.Unlock()

	entry, exists := deviceCodes.codes[deviceCode]
	if !exists {
		return fmt.Errorf("device code not found")
	}

	entry.accessToken = token
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
			logErr(ctx, err,"failed to generate SAML auth URL")
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
	claims, err := s.deps.OIDCProvider.Exchange(ctx, code, nonce)
	if err != nil {
		logErr(ctx, err,"OIDC exchange failed")
		c.HTML(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("Authentication failed")))
		return
	}

	if err := s.completeSSO(ctx, deviceCode, claims); err != nil {
		logErr(ctx, err,"SSO completion failed")
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
		logErr(ctx, err,"SAML validation failed")
		c.HTML(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("Authentication failed")))
		return
	}

	if err := s.completeSSO(ctx, deviceCode, claims); err != nil {
		logErr(ctx, err,"SSO completion failed")
		c.HTML(consts.StatusInternalServerError, "text/html", []byte(authErrorHTML("Login failed")))
		return
	}

	c.HTML(consts.StatusOK, "text/html", []byte(authSuccessHTML))
}

// handleSAMLMetadata returns the SP metadata XML.
func (s *Server) handleSAMLMetadata(ctx context.Context, c *app.RequestContext) {
	xml, err := s.deps.SAMLProvider.MetadataXML()
	if err != nil {
		logErr(ctx, err,"failed to generate SAML metadata")
		c.JSON(consts.StatusInternalServerError, utils.H{"error": "metadata generation failed"})
		return
	}

	c.SetContentType("application/samlmetadata+xml")
	c.SetStatusCode(consts.StatusOK)
	c.Write(xml) //nolint:errcheck
}

// completeSSO handles the shared logic after OIDC/SAML authentication:
// resolve org, sync user/teams/Casbin, create session, complete device flow.
func (s *Server) completeSSO(ctx context.Context, deviceCode string, claims *auth.Claims) error {
	// Get the single org (auto-created at boot).
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		return fmt.Errorf("getting org: %w", err)
	}
	claims.OrgID = org.ID

	// Sync user, teams, and Casbin assignments.
	_, err = auth.SyncUserOnLogin(ctx, s.deps.Q, s.deps.DB, s.deps.Enforcer,
		org.ID, claims, s.deps.Config.Auth.AdminUsers, s.deps.Config.Auth.DefaultRoleOrFallback())
	if err != nil {
		return fmt.Errorf("syncing user: %w", err)
	}

	return s.CompleteDeviceAuth(deviceCode, claims)
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

// logErr logs an error with context.
func logErr(ctx context.Context, err error, msg string) {
	logger := observe.Logger(ctx)
	logger.Error().Err(err).Msg(msg)
}

// Ensure observe is used.
var _ = observe.RequestID
