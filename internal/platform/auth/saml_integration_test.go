package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"encoding/xml"
	"math/big"
	"net/url"
	"testing"
	"time"

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
	_, _, spCertPEM := makeTestCert(t, "Flint SP", spExpiry)
	spKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	spKeyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(spKey)}))
	// Re-issue the SP cert against the SP key so cert and key match.
	spTmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "Flint SP"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: spExpiry}
	spDER, err := x509.CreateCertificate(rand.Reader, spTmpl, spTmpl, &spKey.PublicKey, spKey)
	require.NoError(t, err)
	spCertPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: spDER}))

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
		raw, err := p.AuthURL("relay-1")
		require.NoError(t, err)
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
