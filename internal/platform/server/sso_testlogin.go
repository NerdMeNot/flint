package server

import (
	"context"
	"html"
	"sync"
	"time"

	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// Decoded "test sign-in": a real login round-trip against the IdP that captures
// and displays the EXACT claims/assertion it emits — without creating a session
// or provisioning the user — so an admin sees what they're mapping before going
// live. The login uses the normal /auth/oidc/callback and /auth/saml/acs routes;
// those handlers branch to here when the state belongs to a pending test.

type testLogin struct {
	protocol  string // "oidc" | "saml"
	nonce     string
	verifier  string
	samlReqID string // SAML AuthnRequest ID, bound to the response
	oidc      auth.OIDCAuth
	saml      auth.SAMLAuth
	status    string // "pending" | "complete" | "error"
	result    *testLoginResult
	errMsg    string
	created   time.Time
}

type testLoginResult struct {
	Email  string         `json:"email"`
	Name   string         `json:"name"`
	Groups []string       `json:"groups"`
	Raw    map[string]any `json:"raw"` // the exact claims/attributes the IdP sent
}

type tlStore struct {
	mu sync.Mutex
	m  map[string]*testLogin
}

var testLogins = &tlStore{m: make(map[string]*testLogin)}

func (s *tlStore) put(state string, t *testLogin) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Opportunistic eviction of stale entries (>10m).
	cutoff := time.Now().Add(-10 * time.Minute)
	for k, v := range s.m {
		if v.created.Before(cutoff) {
			delete(s.m, k)
		}
	}
	s.m[state] = t
}

func (s *tlStore) get(state string) (*testLogin, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.m[state]
	return t, ok
}

// handleStartTestLogin builds a provider from the posted config (saved or not),
// returns the IdP authorization URL, and parks a pending test keyed by state.
func (s *Server) handleStartTestLogin(ctx context.Context, c *app.RequestContext) {
	var req struct {
		ProviderType string              `json:"providerType"`
		Config       auth.ProviderConfig `json:"config"`
	}
	if err := c.BindJSON(&req); err != nil {
		apiBadRequest(ctx, c, "invalid request body")
		return
	}

	state := generateSecureCode(32)
	nonce := generateSecureCode(32)
	baseURL := s.deps.Config.Server.BaseURL
	tl := &testLogin{protocol: req.ProviderType, nonce: nonce, status: "pending", created: time.Now()}

	var authURL string
	switch req.ProviderType {
	case "oidc":
		if !req.Config.HasOIDC() {
			apiBadRequest(ctx, c, "OIDC requires issuerUrl and clientId")
			return
		}
		p, err := auth.BuildOIDCProvider(ctx, req.Config, baseURL)
		if err != nil {
			apiBadRequest(ctx, c, "OIDC provider could not be initialized: "+err.Error())
			return
		}
		tl.oidc = p
		tl.verifier = auth.GeneratePKCEVerifier()
		authURL = p.AuthURL(state, nonce, tl.verifier)
	case "saml":
		if !req.Config.HasSAML() {
			apiBadRequest(ctx, c, "SAML requires metadataUrl or metadataXml")
			return
		}
		p, err := auth.BuildSAMLProvider(req.Config, baseURL)
		if err != nil {
			apiBadRequest(ctx, c, "SAML provider could not be initialized: "+err.Error())
			return
		}
		tl.saml = p
		authURL, tl.samlReqID, err = p.AuthURL(state)
		if err != nil {
			apiBadRequest(ctx, c, "SAML auth URL failed: "+err.Error())
			return
		}
	default:
		apiBadRequest(ctx, c, "providerType must be 'oidc' or 'saml'")
		return
	}

	testLogins.put(state, tl)
	c.JSON(consts.StatusOK, utils.H{"testId": state, "authUrl": authURL})
}

// handleGetTestLogin polls the result of a test sign-in.
func (s *Server) handleGetTestLogin(ctx context.Context, c *app.RequestContext) {
	tl, ok := testLogins.get(c.Param("id"))
	if !ok {
		apiNotFound(ctx, c, "test login not found or expired")
		return
	}
	resp := utils.H{"status": tl.status}
	if tl.result != nil {
		resp["result"] = tl.result
	}
	if tl.errMsg != "" {
		resp["error"] = tl.errMsg
	}
	c.JSON(consts.StatusOK, resp)
}

// completeOIDCTestLogin is invoked from the OIDC callback when the state belongs
// to a pending test. It exchanges the code, captures the claims, and renders a
// close-the-window page — never creating a session.
func (s *Server) completeOIDCTestLogin(ctx context.Context, c *app.RequestContext, tl *testLogin, code string) {
	claims, _, err := tl.oidc.Exchange(ctx, code, tl.nonce, tl.verifier)
	s.finishTestLogin(c, tl, claims, err)
}

// completeSAMLTestLogin is invoked from the SAML ACS when the RelayState belongs
// to a pending test.
func (s *Server) completeSAMLTestLogin(c *app.RequestContext, tl *testLogin, samlResponse string) {
	claims, err := tl.saml.ValidateResponse(samlResponse, tl.samlReqID)
	s.finishTestLogin(c, tl, claims, err)
}

func (s *Server) finishTestLogin(c *app.RequestContext, tl *testLogin, claims *auth.Claims, err error) {
	if err != nil {
		tl.status = "error"
		tl.errMsg = err.Error()
		c.Data(consts.StatusOK, "text/html; charset=utf-8", []byte(testLoginDonePage(false, err.Error())))
		return
	}
	tl.result = &testLoginResult{Email: claims.Email, Name: claims.Name, Groups: claims.Groups, Raw: claims.Raw}
	tl.status = "complete"
	c.Data(consts.StatusOK, "text/html; charset=utf-8", []byte(testLoginDonePage(true, "")))
}

func testLoginDonePage(ok bool, detail string) string {
	icon, title, msg := "✓", "Test sign-in captured", "We recorded exactly what your IdP sent. Return to Flint — you can close this window."
	color := "#22d3ee"
	if !ok {
		icon, title, msg, color = "✕", "Test sign-in failed", detail, "#f87171"
	}
	return `<!doctype html><html><head><meta charset="utf-8"><title>Flint — test sign-in</title>
<style>body{font-family:ui-sans-serif,system-ui;background:#0b1620;color:#cdeaf2;display:flex;min-height:100vh;align-items:center;justify-content:center;margin:0}
.card{background:#0f2231;border:1px solid #1d3b4d;border-radius:14px;padding:32px;width:420px;text-align:center;box-shadow:0 20px 50px rgba(0,0,0,.4)}
.icon{font-size:34px;color:` + color + `}h1{font-size:18px;margin:10px 0 6px}p{color:#7fb0c4;font-size:13px;line-height:1.5;margin:0}</style></head>
<body><div class="card"><div class="icon">` + icon + `</div><h1>` + title + `</h1><p>` + html.EscapeString(msg) + `</p></div></body></html>`
}
