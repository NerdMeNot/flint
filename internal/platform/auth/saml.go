package auth

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"fmt"
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
