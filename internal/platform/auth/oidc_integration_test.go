package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testIdP is a minimal, standards-shaped OIDC provider (discovery + JWKS +
// token) used to exercise oidc.go end-to-end: real discovery, real RS256 ID
// token signing/verification, PKCE, nonce, and claim resolution — without a
// network IdP. It is the auth-package counterpart to the server's mock IdP.
type testIdP struct {
	srv    *httptest.Server
	priv   *rsa.PrivateKey
	kid    string
	mu     sync.Mutex
	grants map[string]map[string]any // code -> id_token claims
}

func newTestIdP(t *testing.T) *testIdP {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	idp := &testIdP{priv: priv, kid: "test-key", grants: map[string]map[string]any{}}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                                idp.srv.URL,
			"authorization_endpoint":                idp.srv.URL + "/authorize",
			"token_endpoint":                        idp.srv.URL + "/token",
			"jwks_uri":                              idp.srv.URL + "/jwks",
			"end_session_endpoint":                  idp.srv.URL + "/logout",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: &idp.priv.PublicKey, KeyID: idp.kid, Algorithm: "RS256", Use: "sig",
		}}}
		writeJSON(w, set)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		idp.mu.Lock()
		claims := idp.grants[r.FormValue("code")]
		delete(idp.grants, r.FormValue("code"))
		idp.mu.Unlock()
		if claims == nil {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		now := time.Now()
		mc := jwt.MapClaims{"iss": idp.srv.URL, "aud": "flint-client", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix()}
		for k, v := range claims {
			mc[k] = v
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, mc)
		tok.Header["kid"] = idp.kid
		signed, err := tok.SignedString(idp.priv)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"access_token": "at-1", "token_type": "Bearer", "id_token": signed})
	})
	idp.srv = httptest.NewServer(mux)
	t.Cleanup(idp.srv.Close)
	return idp
}

// grant registers an authorization code that will mint an ID token with claims.
func (idp *testIdP) grant(code string, claims map[string]any) {
	idp.mu.Lock()
	idp.grants[code] = claims
	idp.mu.Unlock()
}

// signLogoutToken mints a back-channel logout token signed by the IdP key, so we
// can drive VerifyLogoutToken exactly as a real IdP would.
func (idp *testIdP) signLogoutToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	base := jwt.MapClaims{"iss": idp.srv.URL, "aud": "flint-client", "iat": time.Now().Unix(), "jti": "jti-1"}
	for k, v := range claims {
		base[k] = v
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, base)
	tok.Header["kid"] = idp.kid
	signed, err := tok.SignedString(idp.priv)
	require.NoError(t, err)
	return signed
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestOIDCProvider_Integration(t *testing.T) {
	idp := newTestIdP(t)
	ctx := context.Background()

	p, err := NewOIDCProvider(ctx, OIDCProviderConfig{
		IssuerURL:   idp.srv.URL,
		ClientID:    "flint-client",
		RedirectURL: "https://flint.example/auth/oidc/callback",
	})
	require.NoError(t, err)

	t.Run("discovery surfaces endpoints incl. end_session", func(t *testing.T) {
		d := p.Discovery()
		assert.Equal(t, idp.srv.URL, d.Issuer)
		assert.Equal(t, idp.srv.URL+"/logout", d.EndSessionEndpoint)
		assert.True(t, p.SupportsRPLogout())
	})

	t.Run("AuthURL carries state, nonce and PKCE challenge", func(t *testing.T) {
		u := p.AuthURL("state-1", "nonce-1", "verifier-abcdefghijklmnopqrstuvwxyz0123456789")
		assert.Contains(t, u, "state=state-1")
		assert.Contains(t, u, "nonce=nonce-1")
		assert.Contains(t, u, "code_challenge=")
		assert.Contains(t, u, "code_challenge_method=S256")
	})

	t.Run("Exchange verifies the ID token and resolves claims", func(t *testing.T) {
		idp.grant("code-good", map[string]any{
			"sub": "user-123", "email": "dev@flint.dev", "name": "Dev", "groups": []string{"eng"},
			"nonce": "nonce-1",
		})
		claims, tok, err := p.Exchange(ctx, "code-good", "nonce-1", "verifier-abcdefghijklmnopqrstuvwxyz0123456789")
		require.NoError(t, err)
		require.NotNil(t, tok)
		assert.Equal(t, "user-123", claims.Subject)
		assert.Equal(t, "dev@flint.dev", claims.Email)
		assert.Equal(t, "Dev", claims.Name)
		assert.Equal(t, []string{"eng"}, claims.Groups)
		assert.Equal(t, "oidc", claims.Provider)
	})

	t.Run("Exchange rejects a mismatched nonce (replay guard)", func(t *testing.T) {
		idp.grant("code-replay", map[string]any{"sub": "u", "email": "e@f.dev", "nonce": "attacker"})
		_, _, err := p.Exchange(ctx, "code-replay", "expected-nonce", "verifier-abcdefghijklmnopqrstuvwxyz0123456789")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nonce")
	})

	t.Run("Exchange fails on an unknown code", func(t *testing.T) {
		_, _, err := p.Exchange(ctx, "no-such-code", "n", "verifier-abcdefghijklmnopqrstuvwxyz0123456789")
		require.Error(t, err)
	})
}

// VerifyLogoutToken must accept a spec-compliant back-channel logout token and
// reject the malformed variants (with nonce, without the event).
func TestOIDCProvider_BackchannelLogout(t *testing.T) {
	idp := newTestIdP(t)
	ctx := context.Background()
	p, err := NewOIDCProvider(ctx, OIDCProviderConfig{
		IssuerURL: idp.srv.URL, ClientID: "flint-client",
		RedirectURL: "https://flint.example/auth/oidc/callback",
	})
	require.NoError(t, err)
	const event = "http://schemas.openid.net/event/backchannel-logout"

	t.Run("valid logout token yields the subject", func(t *testing.T) {
		tok := idp.signLogoutToken(t, jwt.MapClaims{
			"sub": "user-42", "events": map[string]any{event: map[string]any{}},
		})
		sub, _, err := p.VerifyLogoutToken(ctx, tok)
		require.NoError(t, err)
		assert.Equal(t, "user-42", sub)
	})

	t.Run("a token carrying a nonce is rejected (it's an ID token, not a logout token)", func(t *testing.T) {
		tok := idp.signLogoutToken(t, jwt.MapClaims{
			"sub": "u", "nonce": "n", "events": map[string]any{event: map[string]any{}},
		})
		_, _, err := p.VerifyLogoutToken(ctx, tok)
		require.Error(t, err)
	})

	t.Run("a token missing the backchannel-logout event is rejected", func(t *testing.T) {
		tok := idp.signLogoutToken(t, jwt.MapClaims{"sub": "u", "events": map[string]any{}})
		_, _, err := p.VerifyLogoutToken(ctx, tok)
		require.Error(t, err)
	})
}

// Exchange must enrich Azure group GUIDs into names via Graph end-to-end:
// IdP mints a token with GUID groups + oid, the provider calls Graph, and the
// returned Claims carry display names ready for group→role mapping.
func TestOIDCProvider_GraphGroupResolution(t *testing.T) {
	idp := newTestIdP(t)
	mg := newMockGraph(t)
	mg.groups["11111111-1111-1111-1111-111111111111"] = "Engineering"
	mg.groups["22222222-2222-2222-2222-222222222222"] = "Admins"
	ctx := context.Background()

	p, err := NewOIDCProvider(ctx, OIDCProviderConfig{
		IssuerURL:   idp.srv.URL,
		ClientID:    "flint-client",
		RedirectURL: "https://flint.example/auth/oidc/callback",
		Graph:       mg.client(),
	})
	require.NoError(t, err)
	const verifier = "verifier-abcdefghijklmnopqrstuvwxyz0123456789"

	t.Run("GUID group claims are resolved to names", func(t *testing.T) {
		idp.grant("code-guids", map[string]any{
			"sub": "u1", "oid": "user-1", "email": "a@flint.dev", "nonce": "n1",
			"groups": []string{"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"},
		})
		claims, _, err := p.Exchange(ctx, "code-guids", "n1", verifier)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"Engineering", "Admins"}, claims.Groups)
	})

	t.Run("overage: empty groups claim triggers a Graph membership fetch", func(t *testing.T) {
		mg.member["user-2"] = []string{"11111111-1111-1111-1111-111111111111"}
		idp.grant("code-overage", map[string]any{
			"sub": "u2", "oid": "user-2", "email": "b@flint.dev", "nonce": "n2",
			"_claim_names":   map[string]any{"groups": "src1"},
			"_claim_sources": map[string]any{"src1": map[string]any{"endpoint": "https://graph/x"}},
		})
		claims, _, err := p.Exchange(ctx, "code-overage", "n2", verifier)
		require.NoError(t, err)
		assert.Equal(t, []string{"Engineering"}, claims.Groups)
	})
}
