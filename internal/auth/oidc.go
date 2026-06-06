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

// OIDCAuth is the interface for OIDC authentication. *OIDCProvider implements
// it; tests can mock it to avoid real OIDC discovery and token exchange.
type OIDCAuth interface {
	AuthURL(state, nonce string) string
	Exchange(ctx context.Context, code, expectedNonce string) (*Claims, *oauth2.Token, error)
}

// compile-time check
var _ OIDCAuth = (*OIDCProvider)(nil)

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
// and returns unified Claims plus the raw OAuth2 token (which may contain a
// refresh token for IdP sync).
func (p *OIDCProvider) Exchange(ctx context.Context, code, expectedNonce string) (*Claims, *oauth2.Token, error) {
	// Exchange authorization code for tokens.
	token, err := p.oauth2.Exchange(ctx, code)
	if err != nil {
		return nil, nil, fmt.Errorf("exchanging auth code: %w", err)
	}

	// Extract the ID token.
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, nil, fmt.Errorf("no id_token in token response")
	}

	// Verify the ID token (signature, aud, exp, iss).
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, nil, fmt.Errorf("verifying id_token: %w", err)
	}

	// Verify nonce matches to prevent replay attacks.
	if idToken.Nonce != expectedNonce {
		return nil, nil, fmt.Errorf("nonce mismatch: expected %q, got %q", expectedNonce, idToken.Nonce)
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
		return nil, nil, fmt.Errorf("extracting id_token claims: %w", err)
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
	}, token, nil
}

// UserInfo calls the OIDC UserInfo endpoint using the given token to get
// current user claims. Used by the sync daemon to validate sessions.
func (p *OIDCProvider) UserInfo(ctx context.Context, token *oauth2.Token) (*Claims, error) {
	tokenSource := p.oauth2.TokenSource(ctx, token)
	userInfo, err := p.provider.UserInfo(ctx, tokenSource)
	if err != nil {
		return nil, fmt.Errorf("userinfo: %w", err)
	}

	var info struct {
		Email  string   `json:"email"`
		Name   string   `json:"name"`
		Sub    string   `json:"sub"`
		Groups []string `json:"groups"`
	}
	if err := userInfo.Claims(&info); err != nil {
		return nil, fmt.Errorf("userinfo claims: %w", err)
	}

	return &Claims{
		Subject:    info.Sub,
		Email:      info.Email,
		Name:       info.Name,
		Groups:     info.Groups,
		Provider:   "oidc",
		ExternalID: info.Sub,
	}, nil
}

// TokenSource returns an oauth2.TokenSource for the given token.
// The returned source automatically refreshes the token when expired.
func (p *OIDCProvider) TokenSource(ctx context.Context, token *oauth2.Token) oauth2.TokenSource {
	return p.oauth2.TokenSource(ctx, token)
}

// OIDCConfigured returns true if the OIDC provider config has the minimum
// required fields set.
func OIDCConfigured(issuerURL, clientID string) bool {
	return issuerURL != "" && clientID != ""
}

// ensure time is used (for tests).
var _ = time.Now
