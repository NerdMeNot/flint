package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDCProviderConfig configures an OIDC provider.
type OIDCProviderConfig struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string   // e.g., https://flint.example.com/auth/oidc/callback
	Scopes       []string // defaults to [openid, profile, email, groups]
}

// OIDCProvider wraps go-oidc for OIDC authentication.
type OIDCProvider struct {
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth2   oauth2.Config
}

// NewOIDCProvider creates an OIDC provider by performing discovery on the issuer URL.
func NewOIDCProvider(ctx context.Context, cfg OIDCProviderConfig) (*OIDCProvider, error) {
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery on %s: %w", cfg.IssuerURL, err)
	}

	verifier := provider.Verifier(&oidc.Config{
		ClientID: cfg.ClientID,
	})

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email", "groups"}
	}

	oauth2Cfg := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  cfg.RedirectURL,
		Scopes:       scopes,
	}

	return &OIDCProvider{
		provider: provider,
		verifier: verifier,
		oauth2:   oauth2Cfg,
	}, nil
}

// AuthURL returns the URL to redirect the user to for OIDC authentication.
// The state parameter is used for CSRF protection.
// The nonce is embedded in the ID token for replay protection.
func (p *OIDCProvider) AuthURL(state, nonce string) string {
	return p.oauth2.AuthCodeURL(state, oidc.Nonce(nonce))
}

// Exchange exchanges an authorization code for tokens, verifies the ID token,
// and returns unified Claims.
func (p *OIDCProvider) Exchange(ctx context.Context, code, expectedNonce string) (*Claims, error) {
	// Exchange authorization code for tokens.
	token, err := p.oauth2.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchanging auth code: %w", err)
	}

	// Extract the ID token.
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in token response")
	}

	// Verify the ID token (signature, aud, exp, iss).
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("verifying id_token: %w", err)
	}

	// Verify nonce matches to prevent replay attacks.
	if idToken.Nonce != expectedNonce {
		return nil, fmt.Errorf("nonce mismatch: expected %q, got %q", expectedNonce, idToken.Nonce)
	}

	// Extract claims from the ID token.
	var idClaims struct {
		Email         string   `json:"email"`
		EmailVerified bool     `json:"email_verified"`
		Name          string   `json:"name"`
		Sub           string   `json:"sub"`
		Groups        []string `json:"groups"`
	}
	if err := idToken.Claims(&idClaims); err != nil {
		return nil, fmt.Errorf("extracting id_token claims: %w", err)
	}

	// Build raw claims map for custom attribute mapping.
	var rawClaims map[string]any
	_ = idToken.Claims(&rawClaims)

	return &Claims{
		Subject:    idClaims.Sub,
		Email:      idClaims.Email,
		Name:       idClaims.Name,
		Groups:     idClaims.Groups,
		Provider:   "oidc",
		ExternalID: idClaims.Sub,
		IssuedAt:   idToken.IssuedAt,
		ExpiresAt:  idToken.Expiry,
		Raw:        rawClaims,
	}, nil
}

// OIDCConfigured returns true if the OIDC provider config has the minimum
// required fields set.
func OIDCConfigured(issuerURL, clientID string) bool {
	return issuerURL != "" && clientID != ""
}

// ensure time is used (for tests).
var _ = time.Now
