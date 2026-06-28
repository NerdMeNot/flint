package auth

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"net/url"
	"testing"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOIDCEndSessionURL(t *testing.T) {
	t.Run("builds RP-initiated logout URL with hint, redirect and client_id", func(t *testing.T) {
		p := &OIDCProvider{clientID: "flint-client", endSessionEndpoint: "https://idp.example.com/logout"}
		require.True(t, p.SupportsRPLogout())

		raw := p.EndSessionURL("the-id-token", "https://flint.example.com/auth/oidc/logout-complete")
		u, err := url.Parse(raw)
		require.NoError(t, err)
		assert.Equal(t, "https://idp.example.com/logout", u.Scheme+"://"+u.Host+u.Path)
		q := u.Query()
		assert.Equal(t, "the-id-token", q.Get("id_token_hint"))
		assert.Equal(t, "https://flint.example.com/auth/oidc/logout-complete", q.Get("post_logout_redirect_uri"))
		assert.Equal(t, "flint-client", q.Get("client_id"))
	})

	t.Run("preserves existing query params on the endpoint", func(t *testing.T) {
		p := &OIDCProvider{clientID: "c", endSessionEndpoint: "https://idp.example.com/logout?tenant=acme"}
		u, err := url.Parse(p.EndSessionURL("tok", "https://flint/back"))
		require.NoError(t, err)
		assert.Equal(t, "acme", u.Query().Get("tenant"))
		assert.Equal(t, "tok", u.Query().Get("id_token_hint"))
	})

	t.Run("returns empty when the IdP advertises no end_session_endpoint", func(t *testing.T) {
		p := &OIDCProvider{clientID: "c"}
		assert.False(t, p.SupportsRPLogout())
		assert.Empty(t, p.EndSessionURL("tok", "https://flint/back"))
	})
}

func TestSAMLParseLogoutRequest(t *testing.T) {
	// Encode a LogoutRequest the way an IdP delivers it over the HTTP-Redirect
	// binding (base64 of raw DEFLATE), then assert we recover the NameID.
	req := saml.LogoutRequest{
		ID:          "id-123",
		Version:     "2.0",
		Destination: "https://flint.example.com/auth/saml/slo",
		NameID:      &saml.NameID{Value: "alice@example.com"},
	}
	doc := etree.NewDocument()
	doc.SetRoot(req.Element())
	xmlBytes, err := doc.WriteToBytes()
	require.NoError(t, err)

	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.DefaultCompression)
	_, err = w.Write(xmlBytes)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	p := &SAMLProvider{}
	parsed, err := p.ParseLogoutRequest(encoded)
	require.NoError(t, err)
	require.NotNil(t, parsed.NameID)
	assert.Equal(t, "alice@example.com", parsed.NameID.Value)
	assert.Equal(t, "id-123", parsed.ID)
}

func TestSAMLParseLogoutRequest_Garbage(t *testing.T) {
	p := &SAMLProvider{}
	_, err := p.ParseLogoutRequest("not-valid-base64-or-deflate!!!")
	assert.Error(t, err)
}
