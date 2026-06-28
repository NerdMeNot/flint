package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"html"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
)

// mockIdP is a self-contained OIDC identity provider for testing SSO without a
// real IdP — the bundled "DummyIDP". It serves discovery, JWKS, a tiny login
// form (where the tester chooses exactly which claims to emit, incl. groups and
// missing-email scenarios), a token endpoint that mints RS256-signed ID tokens,
// and userinfo. Gated to demo/dev mode; never registered in production.
type mockIdP struct {
	issuer string
	priv   *rsa.PrivateKey
	kid    string

	mu           sync.Mutex
	codes        map[string]mockGrant // authorization code -> grant
	accessClaims map[string]map[string]any
}

type mockGrant struct {
	claims   map[string]any
	nonce    string
	clientID string
}

func newMockIdP(baseURL string) (*mockIdP, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return &mockIdP{
		issuer:       strings.TrimRight(baseURL, "/") + "/mock-idp",
		priv:         priv,
		kid:          "mock-key-1",
		codes:        make(map[string]mockGrant),
		accessClaims: make(map[string]map[string]any),
	}, nil
}

func (m *mockIdP) discovery(_ context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, map[string]any{
		"issuer":                                m.issuer,
		"authorization_endpoint":                m.issuer + "/authorize",
		"token_endpoint":                        m.issuer + "/token",
		"jwks_uri":                              m.issuer + "/jwks",
		"userinfo_endpoint":                     m.issuer + "/userinfo",
		"end_session_endpoint":                  m.issuer + "/logout",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "profile", "email", "groups"},
		"claims_supported":                      []string{"sub", "email", "email_verified", "name", "groups"},
		"code_challenge_methods_supported":      []string{"S256", "plain"},
	})
}

func (m *mockIdP) jwks(_ context.Context, c *app.RequestContext) {
	set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: &m.priv.PublicKey, KeyID: m.kid, Algorithm: "RS256", Use: "sig",
	}}}
	b, _ := json.Marshal(set)
	c.Data(consts.StatusOK, "application/json", b)
}

// authorize renders a minimal login form where the tester picks the claims to emit.
func (m *mockIdP) authorize(_ context.Context, c *app.RequestContext) {
	redirectURI := string(c.Query("redirect_uri"))
	state := string(c.Query("state"))
	nonce := string(c.Query("nonce"))
	clientID := string(c.Query("client_id"))
	c.Data(consts.StatusOK, "text/html; charset=utf-8", []byte(mockLoginForm(m.issuer+"/login", redirectURI, state, nonce, clientID)))
}

// login captures the chosen claims, issues a code, and redirects back to the SP.
func (m *mockIdP) login(_ context.Context, c *app.RequestContext) {
	redirectURI := string(c.FormValue("redirect_uri"))
	state := string(c.FormValue("state"))
	nonce := string(c.FormValue("nonce"))
	clientID := string(c.FormValue("client_id"))
	email := strings.TrimSpace(string(c.FormValue("email")))
	name := strings.TrimSpace(string(c.FormValue("name")))
	groups := splitCommaList(string(c.FormValue("groups")))

	claims := map[string]any{"sub": email, "email_verified": true}
	if email != "" {
		claims["email"] = email
	}
	if name != "" {
		claims["name"] = name
	}
	if len(groups) > 0 {
		claims["groups"] = groups
	}
	if email == "" {
		claims["sub"] = "mock|" + generateSecureCode(8)
	}

	code := generateSecureCode(24)
	m.mu.Lock()
	m.codes[code] = mockGrant{claims: claims, nonce: nonce, clientID: clientID}
	m.mu.Unlock()

	u, err := url.Parse(redirectURI)
	if err != nil {
		c.Data(consts.StatusBadRequest, "text/plain", []byte("invalid redirect_uri"))
		return
	}
	q := u.Query()
	q.Set("code", code)
	q.Set("state", state)
	u.RawQuery = q.Encode()
	c.Redirect(consts.StatusFound, []byte(u.String()))
}

// token exchanges a code for an RS256-signed ID token carrying the chosen claims.
func (m *mockIdP) token(_ context.Context, c *app.RequestContext) {
	code := string(c.FormValue("code"))
	m.mu.Lock()
	grant, ok := m.codes[code]
	delete(m.codes, code)
	m.mu.Unlock()
	if !ok {
		c.JSON(consts.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		return
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iss": m.issuer,
		"aud": grant.clientID,
		"sub": grant.claims["sub"],
		"iat": now.Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
	}
	for _, k := range []string{"email", "email_verified", "name", "groups"} {
		if v, ok := grant.claims[k]; ok {
			claims[k] = v
		}
	}
	if grant.nonce != "" {
		claims["nonce"] = grant.nonce
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = m.kid
	idToken, err := tok.SignedString(m.priv)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, map[string]any{"error": "server_error"})
		return
	}

	accessToken := "mock-at-" + generateSecureCode(16)
	m.mu.Lock()
	m.accessClaims[accessToken] = grant.claims
	m.mu.Unlock()

	c.JSON(consts.StatusOK, map[string]any{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   600,
		"id_token":     idToken,
	})
}

func (m *mockIdP) userinfo(_ context.Context, c *app.RequestContext) {
	auth := strings.TrimPrefix(string(c.GetHeader("Authorization")), "Bearer ")
	m.mu.Lock()
	claims, ok := m.accessClaims[auth]
	m.mu.Unlock()
	if !ok {
		c.JSON(consts.StatusUnauthorized, map[string]any{"error": "invalid_token"})
		return
	}
	c.JSON(consts.StatusOK, claims)
}

// logout implements the OIDC RP-Initiated Logout end_session_endpoint: it
// terminates the (stateless) mock session and bounces the browser back to the
// SP's post_logout_redirect_uri so the full round trip can be exercised.
func (m *mockIdP) logout(_ context.Context, c *app.RequestContext) {
	redirect := string(c.Query("post_logout_redirect_uri"))
	if redirect == "" {
		c.Data(consts.StatusOK, "text/html; charset=utf-8", []byte("<!doctype html><title>Signed out</title><p>Signed out of the mock IdP.</p>"))
		return
	}
	c.Redirect(consts.StatusFound, []byte(redirect))
}

func splitCommaList(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func mockLoginForm(action, redirectURI, state, nonce, clientID string) string {
	esc := html.EscapeString
	return `<!doctype html><html><head><meta charset="utf-8"><title>Mock IdP — Flint</title>
<style>body{font-family:ui-sans-serif,system-ui;background:#0b1620;color:#cdeaf2;display:flex;min-height:100vh;align-items:center;justify-content:center;margin:0}
.card{background:#0f2231;border:1px solid #1d3b4d;border-radius:14px;padding:28px;width:360px;box-shadow:0 20px 50px rgba(0,0,0,.4)}
h1{font-size:18px;margin:0 0 4px}p{color:#7fb0c4;font-size:13px;margin:0 0 18px}
label{display:block;font-size:12px;margin:12px 0 4px;color:#a9d2e0}
input{width:100%;box-sizing:border-box;padding:9px 11px;border-radius:8px;border:1px solid #1d3b4d;background:#0b1a26;color:#e6f6fb;font-size:14px}
button{margin-top:20px;width:100%;padding:10px;border:0;border-radius:8px;background:#22d3ee;color:#062028;font-weight:600;font-size:14px;cursor:pointer}
small{color:#5e8598}</style></head><body><div class="card">
<h1>Mock identity provider</h1><p>Choose the claims this test login should emit, then sign in.</p>
<form method="post" action="` + esc(action) + `">
<input type="hidden" name="redirect_uri" value="` + esc(redirectURI) + `">
<input type="hidden" name="state" value="` + esc(state) + `">
<input type="hidden" name="nonce" value="` + esc(nonce) + `">
<input type="hidden" name="client_id" value="` + esc(clientID) + `">
<label>Email</label><input name="email" value="testuser@flint.dev">
<label>Name</label><input name="name" value="Test User">
<label>Groups <small>(comma-separated)</small></label><input name="groups" value="engineering,admins">
<button type="submit">Sign in as this user</button></form></div></body></html>`
}
