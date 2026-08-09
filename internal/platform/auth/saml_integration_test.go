package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeTestCert returns a fresh self-signed RSA cert/key for use as a test IdP or
// SP signing credential.
func makeTestCert(t *testing.T, cn string, notAfter time.Time) (*x509.Certificate, *rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	return cert, key, pemStr
}

// testIdPMetadataXML builds a realistic IdP metadata document (with SSO + SLO
// endpoints and a signing cert) using crewjam's IdentityProvider, so the SP can
// be constructed exactly as it would be against a real IdP.
func testIdPMetadataXML(t *testing.T, idpCert *x509.Certificate, idpKey *rsa.PrivateKey) string {
	t.Helper()
	idp := saml.IdentityProvider{
		Key:         idpKey,
		Certificate: idpCert,
		MetadataURL: url.URL{Scheme: "https", Host: "idp.example.com", Path: "/metadata"},
		SSOURL:      url.URL{Scheme: "https", Host: "idp.example.com", Path: "/sso"},
		LogoutURL:   url.URL{Scheme: "https", Host: "idp.example.com", Path: "/slo"},
	}
	md := idp.Metadata()
	xmlBytes, err := xml.Marshal(md)
	require.NoError(t, err)
	return string(xmlBytes)
}

func TestSAMLProvider_Integration(t *testing.T) {
	idpExpiry := time.Now().Add(90 * 24 * time.Hour)
	idpCert, idpKey, _ := makeTestCert(t, "Test IdP", idpExpiry)
	metadataXML := testIdPMetadataXML(t, idpCert, idpKey)

	spExpiry := time.Now().Add(365 * 24 * time.Hour)
	makeTestCert(t, "Flint SP", spExpiry) // warm the helper; the SP cert is re-issued below against spKey
	spKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	spKeyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(spKey)}))
	// Re-issue the SP cert against the SP key so cert and key match.
	spTmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "Flint SP"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: spExpiry}
	spDER, err := x509.CreateCertificate(rand.Reader, spTmpl, spTmpl, &spKey.PublicKey, spKey)
	require.NoError(t, err)
	spCertPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: spDER}))

	p, err := BuildSAMLProvider(ProviderConfig{
		MetadataXML: metadataXML,
		EntityID:    "https://flint.example.com/saml",
		SPCertPEM:   spCertPEM,
		SPKeyPEM:    spKeyPEM,
		EmailAttrs:  []string{"email"},
		NameAttrs:   []string{"displayName"},
		GroupsAttrs: []string{"groups"},
	}, "https://flint.example.com")
	require.NoError(t, err)

	t.Run("AuthURL builds a redirect-binding AuthnRequest to the IdP SSO endpoint", func(t *testing.T) {
		raw, requestID, err := p.AuthURL("relay-1")
		require.NoError(t, err)
		assert.NotEmpty(t, requestID)
		u, err := url.Parse(raw)
		require.NoError(t, err)
		assert.Equal(t, "idp.example.com", u.Host)
		assert.Equal(t, "/sso", u.Path)
		assert.NotEmpty(t, u.Query().Get("SAMLRequest"))
		assert.Equal(t, "relay-1", u.Query().Get("RelayState"))
	})

	t.Run("SLO is configured from the IdP metadata", func(t *testing.T) {
		assert.True(t, p.SLOConfigured())
		raw, err := p.LogoutRequestURL("alice@flint.dev", "session-idx-1", "relay-2")
		require.NoError(t, err)
		u, err := url.Parse(raw)
		require.NoError(t, err)
		assert.Equal(t, "/slo", u.Path)
		assert.NotEmpty(t, u.Query().Get("SAMLRequest"))
	})

	t.Run("SP metadata XML is emitted with the ACS URL", func(t *testing.T) {
		md, err := p.MetadataXML()
		require.NoError(t, err)
		assert.Contains(t, string(md), "https://flint.example.com/auth/saml/acs")
	})

	t.Run("CertExpiry reads the IdP signing cert from metadata", func(t *testing.T) {
		ce := p.CertExpiry()
		require.NotNil(t, ce.IdPNotAfter)
		assert.WithinDuration(t, idpExpiry, *ce.IdPNotAfter, time.Minute)
	})
}

// spProvider lets the crewjam IdP find our SP's metadata when validating the
// AuthnRequest and addressing the response.
type spProvider struct{ md *saml.EntityDescriptor }

func (s spProvider) GetServiceProvider(_ *http.Request, _ string) (*saml.EntityDescriptor, error) {
	return s.md, nil
}

// TestSAMLProvider_SignedRoundTrip is the strongest SAML test: a crewjam IdP
// mints a genuinely RSA-signed Response in reply to our SP's AuthnRequest, and
// we run it through the real ValidateResponse — exercising signature validation,
// InResponseTo request-binding, and claim extraction against a signed assertion.
func TestSAMLProvider_SignedRoundTrip(t *testing.T) {
	idpExpiry := time.Now().Add(90 * 24 * time.Hour)
	idpCert, idpKey, _ := makeTestCert(t, "Round-trip IdP", idpExpiry)
	metadataXML := testIdPMetadataXML(t, idpCert, idpKey)

	p, err := BuildSAMLProvider(ProviderConfig{
		MetadataXML: metadataXML,
		EntityID:    "https://flint.example.com/saml",
		EmailAttrs:  []string{"mail"},
		GroupsAttrs: []string{"eduPersonAffiliation"},
	}, "https://flint.example.com")
	require.NoError(t, err)

	idp := &saml.IdentityProvider{
		Key:                     idpKey,
		Certificate:             idpCert,
		MetadataURL:             url.URL{Scheme: "https", Host: "idp.example.com", Path: "/metadata"},
		SSOURL:                  url.URL{Scheme: "https", Host: "idp.example.com", Path: "/sso"},
		ServiceProviderProvider: spProvider{md: p.sp.Metadata()},
	}

	// mintResponse drives a full SP→IdP→SP exchange and returns the base64
	// SAMLResponse plus the request ID the SP expects it to answer.
	mintResponse := func(t *testing.T) (samlResponse, requestID string) {
		t.Helper()
		authURL, reqID, err := p.AuthURL("relay-xyz")
		require.NoError(t, err)
		httpReq := httptest.NewRequest(http.MethodGet, authURL, nil)

		idpReq, err := saml.NewIdpAuthnRequest(idp, httpReq)
		require.NoError(t, err)
		require.NoError(t, idpReq.Validate())

		idpReq.Now = time.Now()
		require.NoError(t, saml.DefaultAssertionMaker{}.MakeAssertion(idpReq, &saml.Session{
			ID: "sess-1", NameID: "alice@flint.dev", UserEmail: "alice@flint.dev",
			Groups: []string{"engineering", "admins"}, Index: "session-index-7",
		}))
		require.NoError(t, idpReq.MakeResponse())

		doc := etree.NewDocument()
		doc.SetRoot(idpReq.ResponseEl)
		xmlBytes, err := doc.WriteToBytes()
		require.NoError(t, err)
		return base64.StdEncoding.EncodeToString(xmlBytes), reqID
	}

	t.Run("a signed response bound to our request validates and yields claims", func(t *testing.T) {
		resp, reqID := mintResponse(t)
		claims, err := p.ValidateResponse(resp, reqID)
		require.NoError(t, err)
		assert.Equal(t, "alice@flint.dev", claims.Email)
		assert.ElementsMatch(t, []string{"engineering", "admins"}, claims.Groups)
		assert.NotEmpty(t, claims.AssertionID)
		assert.Equal(t, "session-index-7", claims.SessionIndex)
	})

	t.Run("a response for a different request is rejected (InResponseTo binding)", func(t *testing.T) {
		resp, _ := mintResponse(t)
		_, err := p.ValidateResponse(resp, "id-some-other-request")
		require.Error(t, err)
	})

	t.Run("an unsolicited response (no expected request) is rejected", func(t *testing.T) {
		resp, _ := mintResponse(t)
		_, err := p.ValidateResponse(resp, "")
		require.Error(t, err)
	})
}
