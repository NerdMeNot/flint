package auth

import (
	"context"
	"fmt"
	"net/url"
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

	// Mapping names the claims that carry email/name/groups. Empty fields fall
	// back to the OIDC-conventional claim names.
	Mapping OIDCMapping
}

// OIDCAuth is the interface for OIDC authentication. *OIDCProvider implements
// it; tests can mock it to avoid real OIDC discovery and token exchange.
//
// codeVerifier is the PKCE (S256) verifier: AuthURL embeds its derived challenge
// and Exchange presents it, binding the authorization code to this login.
type OIDCAuth interface {
	AuthURL(state, nonce, codeVerifier string) string
	Exchange(ctx context.Context, code, expectedNonce, codeVerifier string) (*Claims, *oauth2.Token, error)
}

// compile-time check
var _ OIDCAuth = (*OIDCProvider)(nil)

// OIDCProvider wraps go-oidc for OIDC authentication.
type OIDCProvider struct {
	provider           *oidc.Provider
	verifier           *oidc.IDTokenVerifier
	oauth2             oauth2.Config
	mapping            OIDCMapping
	clientID           string
	endSessionEndpoint string // RP-initiated logout endpoint, if the IdP advertises one
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

	// The end_session_endpoint is part of OIDC discovery (RP-Initiated Logout
	// 1.0) but not surfaced by go-oidc's Endpoint(), so read it from the raw
	// discovery document. Empty when the IdP doesn't support RP-initiated logout.
	var disc struct {
		EndSessionEndpoint string `json:"end_session_endpoint"`
	}
	_ = provider.Claims(&disc)

	return &OIDCProvider{
		provider:           provider,
		verifier:           verifier,
		oauth2:             oauth2Cfg,
		mapping:            cfg.Mapping,
		clientID:           cfg.ClientID,
		endSessionEndpoint: disc.EndSessionEndpoint,
	}, nil
}

// AuthURL returns the URL to redirect the user to for OIDC authentication.
// The state parameter is used for CSRF protection.
// The nonce is embedded in the ID token for replay protection.
func (p *OIDCProvider) AuthURL(state, nonce, codeVerifier string) string {
	opts := []oauth2.AuthCodeOption{oidc.Nonce(nonce)}
	if codeVerifier != "" {
		opts = append(opts, oauth2.S256ChallengeOption(codeVerifier))
	}
	return p.oauth2.AuthCodeURL(state, opts...)
}

// Exchange exchanges an authorization code for tokens, verifies the ID token,
// and returns unified Claims plus the raw OAuth2 token (which may contain a
// refresh token for IdP sync).
func (p *OIDCProvider) Exchange(ctx context.Context, code, expectedNonce, codeVerifier string) (*Claims, *oauth2.Token, error) {
	// Exchange authorization code for tokens, presenting the PKCE verifier.
	var opts []oauth2.AuthCodeOption
	if codeVerifier != "" {
		opts = append(opts, oauth2.VerifierOption(codeVerifier))
	}
	token, err := p.oauth2.Exchange(ctx, code, opts...)
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

	// Extract the full claims bag, then resolve unified fields through the
	// configured mapping (claim names vary widely across IdPs).
	var rawClaims map[string]any
	if err := idToken.Claims(&rawClaims); err != nil {
		return nil, nil, fmt.Errorf("extracting id_token claims: %w", err)
	}
	email, name, groups := resolveOIDCClaims(rawClaims, p.mapping)

	return &Claims{
		Subject:    idToken.Subject,
		Email:      email,
		Name:       name,
		Groups:     groups,
		Provider:   "oidc",
		ExternalID: idToken.Subject,
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

	var raw map[string]any
	if err := userInfo.Claims(&raw); err != nil {
		return nil, fmt.Errorf("userinfo claims: %w", err)
	}
	email, name, groups := resolveOIDCClaims(raw, p.mapping)

	return &Claims{
		Subject:    userInfo.Subject,
		Email:      email,
		Name:       name,
		Groups:     groups,
		Provider:   "oidc",
		ExternalID: userInfo.Subject,
		Raw:        raw,
	}, nil
}

// OIDCDiscovery summarizes what was learned from the issuer's discovery
// document — used by the "test connection" flow to confirm a working provider
// and to hint which scopes/claims (e.g. groups) the IdP actually supports.
type OIDCDiscovery struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorizationEndpoint"`
	TokenEndpoint         string   `json:"tokenEndpoint"`
	UserinfoEndpoint      string   `json:"userinfoEndpoint,omitempty"`
	EndSessionEndpoint    string   `json:"endSessionEndpoint,omitempty"`
	ScopesSupported       []string `json:"scopesSupported,omitempty"`
	ClaimsSupported       []string `json:"claimsSupported,omitempty"`
}

// SupportsRPLogout reports whether the IdP advertises an end_session_endpoint,
// i.e. whether RP-initiated single logout is available.
func (p *OIDCProvider) SupportsRPLogout() bool { return p.endSessionEndpoint != "" }

// EndSessionURL builds the RP-initiated logout URL (OIDC RP-Initiated Logout
// 1.0). idTokenHint is the user's ID token (the IdP uses it to identify the
// session to terminate); postLogoutRedirectURI is where the IdP returns the
// browser afterwards. Returns "" when the IdP advertises no end_session_endpoint.
func (p *OIDCProvider) EndSessionURL(idTokenHint, postLogoutRedirectURI string) string {
	if p.endSessionEndpoint == "" {
		return ""
	}
	u, err := url.Parse(p.endSessionEndpoint)
	if err != nil {
		return ""
	}
	q := u.Query()
	if idTokenHint != "" {
		q.Set("id_token_hint", idTokenHint)
	}
	if postLogoutRedirectURI != "" {
		q.Set("post_logout_redirect_uri", postLogoutRedirectURI)
	}
	// client_id is required by some IdPs (e.g. when no id_token_hint is sent)
	// and harmless otherwise.
	q.Set("client_id", p.clientID)
	u.RawQuery = q.Encode()
	return u.String()
}

// Discovery returns the discovery metadata resolved at construction.
func (p *OIDCProvider) Discovery() OIDCDiscovery {
	var raw struct {
		Issuer           string   `json:"issuer"`
		UserinfoEndpoint string   `json:"userinfo_endpoint"`
		ScopesSupported  []string `json:"scopes_supported"`
		ClaimsSupported  []string `json:"claims_supported"`
	}
	_ = p.provider.Claims(&raw)
	return OIDCDiscovery{
		Issuer:                raw.Issuer,
		AuthorizationEndpoint: p.oauth2.Endpoint.AuthURL,
		TokenEndpoint:         p.oauth2.Endpoint.TokenURL,
		UserinfoEndpoint:      raw.UserinfoEndpoint,
		EndSessionEndpoint:    p.endSessionEndpoint,
		ScopesSupported:       raw.ScopesSupported,
		ClaimsSupported:       raw.ClaimsSupported,
	}
}

// TokenSource returns an oauth2.TokenSource for the given token.
// The returned source automatically refreshes the token when expired.
func (p *OIDCProvider) TokenSource(ctx context.Context, token *oauth2.Token) oauth2.TokenSource {
	return p.oauth2.TokenSource(ctx, token)
}

// GeneratePKCEVerifier returns a fresh PKCE (S256) code verifier to store with
// the pending login and present on the token exchange.
func GeneratePKCEVerifier() string {
	return oauth2.GenerateVerifier()
}

// OIDCConfigured returns true if the OIDC provider config has the minimum
// required fields set.
func OIDCConfigured(issuerURL, clientID string) bool {
	return issuerURL != "" && clientID != ""
}

// ensure time is used (for tests).
var _ = time.Now
