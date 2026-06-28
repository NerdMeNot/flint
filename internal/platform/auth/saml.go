package auth

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
)

// SAMLProviderConfig configures a SAML Service Provider.
type SAMLProviderConfig struct {
	MetadataURL string // IdP metadata URL (fetched at construction)
	MetadataXML string // Raw IdP metadata XML, as an alternative to MetadataURL
	EntityID    string // SP entity ID (default: {baseURL}/auth/saml/metadata)
	ACSURL      string // Assertion Consumer Service URL (e.g., {baseURL}/auth/saml/acs)
	CertPEM     string // Optional SP signing certificate (PEM)
	KeyPEM      string // Optional SP signing private key (PEM)

	// NameIDFormat requested in the AuthnRequest. Empty defaults to emailAddress.
	NameIDFormat string
	// Mapping lists the attribute names that carry email/name/groups. Empty
	// fields fall back to the well-known defaults (incl. the Entra groups URI).
	Mapping SAMLMapping
}

// SAMLAuth is the interface for SAML authentication. *SAMLProvider implements
// it; tests can mock it to avoid real IdP metadata fetching and response validation.
type SAMLAuth interface {
	AuthURL(relayState string) (string, error)
	ValidateResponse(samlResponse string) (*Claims, error)
	MetadataXML() ([]byte, error)
}

// compile-time check
var _ SAMLAuth = (*SAMLProvider)(nil)

// SAMLProvider wraps crewjam/saml for SAML SP operations.
type SAMLProvider struct {
	sp      saml.ServiceProvider
	mapping SAMLMapping
}

// NewSAMLProvider creates a SAML Service Provider by fetching IdP metadata.
func NewSAMLProvider(cfg SAMLProviderConfig) (*SAMLProvider, error) {
	// Parse SP URLs.
	rootURL, err := url.Parse(cfg.ACSURL)
	if err != nil {
		return nil, fmt.Errorf("parsing ACS URL: %w", err)
	}

	entityID := cfg.EntityID
	if entityID == "" {
		entityID = rootURL.Scheme + "://" + rootURL.Host + "/auth/saml/metadata"
	}

	entityIDURL, err := url.Parse(entityID)
	if err != nil {
		return nil, fmt.Errorf("parsing entity ID: %w", err)
	}

	acsURL, err := url.Parse(cfg.ACSURL)
	if err != nil {
		return nil, fmt.Errorf("parsing ACS URL: %w", err)
	}

	// Load or generate SP certificate.
	var cert *x509.Certificate
	var key *rsa.PrivateKey

	if cfg.CertPEM != "" && cfg.KeyPEM != "" {
		tlsCert, err := tls.X509KeyPair([]byte(cfg.CertPEM), []byte(cfg.KeyPEM))
		if err != nil {
			return nil, fmt.Errorf("loading SP certificate: %w", err)
		}
		cert, err = x509.ParseCertificate(tlsCert.Certificate[0])
		if err != nil {
			return nil, fmt.Errorf("parsing SP certificate: %w", err)
		}
		key = tlsCert.PrivateKey.(*rsa.PrivateKey)
	}

	// Resolve IdP metadata, either from raw XML or by fetching the metadata URL.
	var idpMetadata *saml.EntityDescriptor
	switch {
	case cfg.MetadataXML != "":
		idpMetadata, err = samlsp.ParseMetadata([]byte(cfg.MetadataXML))
		if err != nil {
			return nil, fmt.Errorf("parsing IdP metadata XML: %w", err)
		}
	case cfg.MetadataURL != "":
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		metadataURL, perr := url.Parse(cfg.MetadataURL)
		if perr != nil {
			return nil, fmt.Errorf("parsing metadata URL: %w", perr)
		}
		idpMetadata, err = samlsp.FetchMetadata(ctx, http.DefaultClient, *metadataURL)
		if err != nil {
			return nil, fmt.Errorf("fetching IdP metadata from %s: %w", cfg.MetadataURL, err)
		}
	default:
		return nil, fmt.Errorf("SAML provider requires either MetadataURL or MetadataXML")
	}

	sp := saml.ServiceProvider{
		EntityID:          entityIDURL.String(),
		AcsURL:            *acsURL,
		IDPMetadata:       idpMetadata,
		AuthnNameIDFormat: nameIDFormat(cfg.NameIDFormat),
	}

	if cert != nil && key != nil {
		sp.Certificate = cert
		sp.Key = key
	}

	return &SAMLProvider{sp: sp, mapping: cfg.Mapping}, nil
}

// nameIDFormat maps a configured NameID format string to a crewjam NameIDFormat,
// defaulting to emailAddress (the format Flint keys users on).
func nameIDFormat(s string) saml.NameIDFormat {
	switch s {
	case "":
		return saml.EmailAddressNameIDFormat
	case string(saml.PersistentNameIDFormat):
		return saml.PersistentNameIDFormat
	case string(saml.TransientNameIDFormat):
		return saml.TransientNameIDFormat
	case string(saml.UnspecifiedNameIDFormat):
		return saml.UnspecifiedNameIDFormat
	default:
		return saml.NameIDFormat(s)
	}
}

// SAMLDiscovery summarizes the IdP metadata resolved at construction — used by
// the "test connection" flow to confirm the IdP was reachable and parseable.
type SAMLDiscovery struct {
	IDPEntityID string `json:"idpEntityId"`
	SSOURL      string `json:"ssoUrl,omitempty"`
}

// Discovery returns a summary of the configured IdP metadata.
func (p *SAMLProvider) Discovery() SAMLDiscovery {
	d := SAMLDiscovery{SSOURL: p.sp.GetSSOBindingLocation(saml.HTTPRedirectBinding)}
	if p.sp.IDPMetadata != nil {
		d.IDPEntityID = p.sp.IDPMetadata.EntityID
	}
	return d
}

// CertExpiry reports the earliest IdP signing-certificate expiry (parsed from
// the IdP metadata) and the SP signing-certificate expiry (if configured) — so
// the UI can warn before a silent SSO outage. nil fields mean "not available".
type CertExpiry struct {
	IdPNotAfter *time.Time `json:"idpNotAfter,omitempty"`
	SPNotAfter  *time.Time `json:"spNotAfter,omitempty"`
}

func (p *SAMLProvider) CertExpiry() CertExpiry {
	var ce CertExpiry
	if p.sp.IDPMetadata != nil {
		for _, idp := range p.sp.IDPMetadata.IDPSSODescriptors {
			for _, kd := range idp.KeyDescriptors {
				if kd.Use == "encryption" {
					continue // signing certs only
				}
				for _, xc := range kd.KeyInfo.X509Data.X509Certificates {
					der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(xc.Data))
					if err != nil {
						continue
					}
					cert, err := x509.ParseCertificate(der)
					if err != nil {
						continue
					}
					if ce.IdPNotAfter == nil || cert.NotAfter.Before(*ce.IdPNotAfter) {
						t := cert.NotAfter
						ce.IdPNotAfter = &t
					}
				}
			}
		}
	}
	if p.sp.Certificate != nil {
		t := p.sp.Certificate.NotAfter
		ce.SPNotAfter = &t
	}
	return ce
}

// MetadataXML returns the SP metadata as XML bytes.
func (p *SAMLProvider) MetadataXML() ([]byte, error) {
	md := p.sp.Metadata()
	return xml.MarshalIndent(md, "", "  ")
}

// AuthURL returns the URL to redirect the user to for SAML authentication.
// The relayState is returned by the IdP after authentication (used for CSRF / device flow binding).
func (p *SAMLProvider) AuthURL(relayState string) (string, error) {
	authnRequest, err := p.sp.MakeAuthenticationRequest(
		p.sp.GetSSOBindingLocation(saml.HTTPRedirectBinding),
		saml.HTTPRedirectBinding,
		saml.HTTPPostBinding,
	)
	if err != nil {
		return "", fmt.Errorf("creating AuthnRequest: %w", err)
	}

	redirectURL, err := authnRequest.Redirect(relayState, &p.sp)
	if err != nil {
		return "", fmt.Errorf("building redirect URL: %w", err)
	}

	return redirectURL.String(), nil
}

// SLOConfigured reports whether the IdP advertises a SingleLogoutService, i.e.
// whether SAML Single Logout is available.
func (p *SAMLProvider) SLOConfigured() bool {
	return p.sp.GetSLOBindingLocation(saml.HTTPRedirectBinding) != ""
}

// LogoutRequestURL builds an SP-initiated SAML LogoutRequest (HTTP-Redirect
// binding) for the given NameID, scoped to the SessionIndex captured at login.
// Returns "" (no error) when the IdP advertises no SingleLogoutService.
func (p *SAMLProvider) LogoutRequestURL(nameID, sessionIndex, relayState string) (string, error) {
	if !p.SLOConfigured() {
		return "", nil
	}
	req, err := p.sp.MakeLogoutRequest(p.sp.GetSLOBindingLocation(saml.HTTPRedirectBinding), nameID)
	if err != nil {
		return "", fmt.Errorf("creating LogoutRequest: %w", err)
	}
	if sessionIndex != "" {
		req.SessionIndex = &saml.SessionIndex{Value: sessionIndex}
	}
	return req.Redirect(relayState).String(), nil
}

// ParseLogoutRequest decodes an IdP-initiated LogoutRequest delivered over the
// HTTP-Redirect binding (base64 + raw DEFLATE) and returns the parsed request,
// from which the caller reads the NameID to revoke local sessions.
func (p *SAMLProvider) ParseLogoutRequest(samlRequest string) (*saml.LogoutRequest, error) {
	compressed, err := base64.StdEncoding.DecodeString(samlRequest)
	if err != nil {
		return nil, fmt.Errorf("decoding SAMLRequest: %w", err)
	}
	r := flate.NewReader(bytes.NewReader(compressed))
	defer r.Close()
	xmlBytes, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("inflating SAMLRequest: %w", err)
	}
	var req saml.LogoutRequest
	if err := xml.Unmarshal(xmlBytes, &req); err != nil {
		return nil, fmt.Errorf("parsing LogoutRequest: %w", err)
	}
	return &req, nil
}

// LogoutResponseURL builds the SAML LogoutResponse (HTTP-Redirect binding)
// acknowledging an IdP-initiated LogoutRequest with the given ID.
func (p *SAMLProvider) LogoutResponseURL(requestID, relayState string) (string, error) {
	u, err := p.sp.MakeRedirectLogoutResponse(requestID, relayState)
	if err != nil {
		return "", fmt.Errorf("building LogoutResponse: %w", err)
	}
	return u.String(), nil
}

// ValidateResponse validates a SAML response and extracts claims.
func (p *SAMLProvider) ValidateResponse(samlResponse string) (*Claims, error) {
	// Decode the base64-encoded SAML response.
	responseBytes, err := base64.StdEncoding.DecodeString(samlResponse)
	if err != nil {
		return nil, fmt.Errorf("decoding SAML response: %w", err)
	}

	assertion, err := p.sp.ParseXMLResponse(responseBytes, []string{""}, p.sp.AcsURL)
	if err != nil {
		return nil, fmt.Errorf("validating SAML response: %w", err)
	}

	// Extract claims from the assertion.
	claims := &Claims{
		Provider: "saml",
		IssuedAt: time.Now(),
	}

	if assertion.Subject != nil && assertion.Subject.NameID != nil {
		claims.Subject = assertion.Subject.NameID.Value
		claims.ExternalID = assertion.Subject.NameID.Value

		// If NameID format is email, use it as email.
		if assertion.Subject.NameID.Format == "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress" {
			claims.Email = assertion.Subject.NameID.Value
		}
	}

	// Capture the SessionIndex — required to scope a later SP-initiated
	// LogoutRequest to this exact IdP session (SAML SLO).
	for _, stmt := range assertion.AuthnStatements {
		if stmt.SessionIndex != "" {
			claims.SessionIndex = stmt.SessionIndex
			break
		}
	}

	// Build the attribute bag (also indexed by FriendlyName when present, since
	// some IdPs only set one of Name/FriendlyName), then resolve the unified
	// fields through the configured mapping.
	raw := make(map[string]any)
	for _, stmt := range assertion.AttributeStatements {
		for _, attr := range stmt.Attributes {
			values := make([]string, len(attr.Values))
			for i, v := range attr.Values {
				values[i] = v.Value
			}
			var stored any = values
			if len(values) == 1 {
				stored = values[0]
			}
			if attr.Name != "" {
				raw[attr.Name] = stored
			}
			if attr.FriendlyName != "" {
				raw[attr.FriendlyName] = stored
			}
		}
	}

	email, name, groups := resolveSAMLAttributes(raw, p.mapping)
	if email != "" {
		claims.Email = email
	}
	claims.Name = name
	claims.Groups = groups
	claims.Raw = raw

	// Set expiry from conditions.
	if assertion.Conditions != nil && !assertion.Conditions.NotOnOrAfter.IsZero() {
		claims.ExpiresAt = assertion.Conditions.NotOnOrAfter
	}

	// Email is required for user identification and policy lookups.
	if claims.Email == "" {
		// Fall back to Subject if it looks like an email.
		if strings.Contains(claims.Subject, "@") {
			claims.Email = claims.Subject
		} else {
			return nil, fmt.Errorf("SAML assertion did not contain an email attribute")
		}
	}

	return claims, nil
}
