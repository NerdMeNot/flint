package server

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"golang.org/x/oauth2"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/platform/auth"
)

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
	state, sErr := generateSecureCode(32)
	nonce, nErr := generateSecureCode(32)
	if sErr != nil || nErr != nil {
		c.Data(consts.StatusInternalServerError, "text/html", []byte(authErrorHTML("Login failed")))
		return
	}
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
		authURL, requestID, err := s.deps.SAMLProvider.AuthURL(state)
		if err != nil {
			logErr(ctx, err, "failed to generate SAML auth URL")
			c.Data(consts.StatusInternalServerError, "text/html", []byte(authErrorHTML("SAML error")))
			return
		}
		// Persist the AuthnRequest ID so the ACS can bind the response to it.
		_ = s.deps.Q.SetDeviceCodeSAMLRequestID(ctx, db.SetDeviceCodeSAMLRequestIDParams{
			DeviceCode: deviceCode, SamlRequestID: &requestID,
		})
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

	// Validate the SAML response, bound to the AuthnRequest we issued for this
	// device-flow login (InResponseTo), rejecting unsolicited/mismatched responses.
	reqIDPtr, _ := s.deps.Q.GetDeviceCodeSAMLRequestID(ctx, deviceCode)
	claims, err := s.deps.SAMLProvider.ValidateResponse(samlResponse, derefStr(reqIDPtr))
	if err != nil {
		logErr(ctx, err, "SAML validation failed")
		s.auditLoginFailure(ctx, c, "saml:validation_failed", err.Error())
		c.Data(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("Authentication failed")))
		return
	}

	// One-time-use: refuse a replayed assertion even within its validity window.
	if claims.AssertionID != "" {
		exp := claims.ExpiresAt
		if exp.IsZero() {
			exp = time.Now().Add(10 * time.Minute)
		}
		fresh, merr := s.deps.Q.MarkSAMLAssertionUsed(ctx, db.MarkSAMLAssertionUsedParams{
			AssertionID: claims.AssertionID, ExpiresAt: exp,
		})
		if merr != nil || fresh == 0 {
			s.auditLoginFailure(ctx, c, "saml:replay", "assertion already used")
			c.Data(consts.StatusBadRequest, "text/html", []byte(authErrorHTML("Authentication failed")))
			return
		}
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

	// Audit + metric: successful SSO login (method is the IdP protocol).
	_ = s.deps.Q.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		OrgID:        org.ID,
		UserID:       &userID,
		Action:       "auth.login",
		ResourceType: "session",
	})
	recordLoginMetric(ctx, claims.Provider, "success")

	if err := s.CompleteDeviceAuth(ctx, deviceCode, claims, userID); err != nil {
		return "", err
	}

	return userID, nil
}

// ── Local Auth ────────────────────────────────────────────────
