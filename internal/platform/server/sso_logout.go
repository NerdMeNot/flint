package server

import (
	"context"
	"encoding/json"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/NerdMeNot/flint/pkg/secret"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// ssoLogoutState is the per-session material needed to terminate the upstream
// IdP session at logout (Single Logout). It is stored envelope-encrypted in
// sessions.logout_state_enc and only ever read back at logout time.
type ssoLogoutState struct {
	Provider     string `json:"provider"`               // "oidc" | "saml"
	IDToken      string `json:"idToken,omitempty"`      // OIDC id_token_hint
	NameID       string `json:"nameID,omitempty"`       // SAML subject
	SessionIndex string `json:"sessionIndex,omitempty"` // SAML AuthnStatement index
}

// persistLogoutState stores the SLO material on the session created for the
// given device-code login. Best-effort: a missing master key or session simply
// means single logout is unavailable for this session (local revoke still works).
func (s *Server) persistLogoutState(ctx context.Context, deviceCode string, st ssoLogoutState) {
	masterKey, err := s.deps.Config.Encryption.DecodeMasterKey()
	if err != nil {
		return
	}
	blob, err := json.Marshal(st)
	if err != nil {
		return
	}
	enc, err := secret.Encrypt(blob, masterKey, 1)
	if err != nil {
		return
	}
	refreshPtr, _ := s.deps.Q.GetDeviceCodeRefreshToken(ctx, deviceCode)
	refresh := derefStr(refreshPtr)
	if refresh == "" {
		return
	}
	sess, err := s.deps.Q.GetSessionByTokenHash(ctx, auth.HashToken(refresh))
	if err != nil {
		return
	}
	_ = s.deps.Q.UpdateSessionLogoutState(ctx, db.UpdateSessionLogoutStateParams{
		ID:             sess.ID,
		LogoutStateEnc: enc,
	})
}

// decodeLogoutState decrypts a session's stored SLO material. Returns ok=false
// when there is none or it can't be read.
func (s *Server) decodeLogoutState(blob []byte) (ssoLogoutState, bool) {
	var st ssoLogoutState
	if len(blob) == 0 {
		return st, false
	}
	masterKey, err := s.deps.Config.Encryption.DecodeMasterKey()
	if err != nil {
		return st, false
	}
	plain, _, err := secret.Decrypt(blob, masterKey)
	if err != nil {
		return st, false
	}
	if err := json.Unmarshal(plain, &st); err != nil {
		return st, false
	}
	return st, true
}

// ssoLogoutURL turns a session's stored SLO material into the IdP logout URL the
// browser should be redirected to, or "" when single logout is unavailable
// (no material, provider not configured, or IdP advertises no logout endpoint).
func (s *Server) ssoLogoutURL(st ssoLogoutState) string {
	baseURL := s.deps.Config.Server.BaseURL
	switch st.Provider {
	case "oidc":
		if p, ok := s.deps.OIDCProvider.(*auth.OIDCProvider); ok && p != nil {
			return p.EndSessionURL(st.IDToken, baseURL+"/auth/oidc/logout-complete")
		}
	case "saml":
		if p, ok := s.deps.SAMLProvider.(*auth.SAMLProvider); ok && p != nil {
			url, err := p.LogoutRequestURL(st.NameID, st.SessionIndex, "")
			if err == nil {
				return url
			}
		}
	}
	return ""
}

// handleOIDCLogoutComplete is the post_logout_redirect_uri target the IdP
// returns the browser to after terminating the upstream OIDC session. The local
// session was already revoked when logout was initiated, so just bounce to login.
func (s *Server) handleOIDCLogoutComplete(ctx context.Context, c *app.RequestContext) {
	c.Redirect(consts.StatusFound, []byte("/login"))
}

// handleSAMLSLO handles the SAML SingleLogout endpoint for both directions:
//   - SAMLResponse present  → the IdP's acknowledgement of our SP-initiated
//     LogoutRequest. Validate and bounce to login.
//   - SAMLRequest present   → an IdP-initiated LogoutRequest. Revoke every local
//     session for the named user and reply with a LogoutResponse.
func (s *Server) handleSAMLSLO(ctx context.Context, c *app.RequestContext) {
	p, ok := s.deps.SAMLProvider.(*auth.SAMLProvider)
	if !ok || p == nil {
		c.Redirect(consts.StatusFound, []byte("/login"))
		return
	}

	if samlReq := string(c.Query("SAMLRequest")); samlReq != "" {
		s.handleSAMLIdPInitiatedLogout(ctx, c, p, samlReq, string(c.Query("RelayState")))
		return
	}

	// SP-initiated round trip completing: the IdP acknowledged our request.
	// The local session is already revoked; acknowledge by returning to login.
	c.Redirect(consts.StatusFound, []byte("/login"))
}

func (s *Server) handleSAMLIdPInitiatedLogout(ctx context.Context, c *app.RequestContext, p *auth.SAMLProvider, samlReq, relayState string) {
	req, err := p.ParseLogoutRequest(samlReq)
	if err != nil {
		logErr(ctx, err, "parsing IdP LogoutRequest")
		c.Redirect(consts.StatusFound, []byte("/login"))
		return
	}

	// Revoke every Flint session belonging to the user the IdP is logging out.
	if req.NameID != nil && req.NameID.Value != "" {
		if org, oerr := s.deps.Q.GetOrg(ctx); oerr == nil {
			if user, uerr := s.deps.Q.GetUserByExternalID(ctx, db.GetUserByExternalIDParams{
				OrgID: org.ID, ExternalID: req.NameID.Value,
			}); uerr == nil {
				_ = s.deps.Q.RevokeUserSessions(ctx, user.ID)
				rid := user.ID
				_ = s.deps.Q.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
					OrgID: org.ID, UserID: &rid, Action: "auth.logout.slo",
					ResourceType: "session",
				})
			}
		}
	}

	// Acknowledge with a LogoutResponse over the redirect binding.
	respURL, err := p.LogoutResponseURL(req.ID, relayState)
	if err != nil {
		logErr(ctx, err, "building SAML LogoutResponse")
		c.Redirect(consts.StatusFound, []byte("/login"))
		return
	}
	c.Redirect(consts.StatusFound, []byte(respURL))
}

// logoutResponse is the JSON returned to the SPA on logout: when logoutURL is
// set the SPA must redirect the browser there to complete Single Logout.
func logoutResponse(c *app.RequestContext, logoutURL string) {
	c.JSON(consts.StatusOK, utils.H{"status": "logged out", "logoutUrl": logoutURL})
}
