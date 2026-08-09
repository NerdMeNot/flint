package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/pkg/secret"
)

// ProviderConfig is the JSON shape persisted (envelope-encrypted) in
// auth_provider_config.config_enc. It is the single source of truth for an SSO
// provider's configuration, shared by the API handler that writes it, the boot
// sequence and syncd that read it, and the hot-reload path. The original fields
// (issuerUrl/clientId/clientSecret/metadataUrl/entityId) keep their JSON tags so
// previously-stored blobs continue to decode.
type ProviderConfig struct {
	// OIDC.
	IssuerURL    string   `json:"issuerUrl,omitempty"`
	ClientID     string   `json:"clientId,omitempty"`
	ClientSecret string   `json:"clientSecret,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
	EmailClaim   string   `json:"emailClaim,omitempty"`
	NameClaim    string   `json:"nameClaim,omitempty"`
	GroupsClaim  string   `json:"groupsClaim,omitempty"`

	// SAML.
	MetadataURL  string   `json:"metadataUrl,omitempty"`
	MetadataXML  string   `json:"metadataXml,omitempty"`
	EntityID     string   `json:"entityId,omitempty"`
	NameIDFormat string   `json:"nameIdFormat,omitempty"`
	EmailAttrs   []string `json:"emailAttributes,omitempty"`
	NameAttrs    []string `json:"nameAttributes,omitempty"`
	GroupsAttrs  []string `json:"groupsAttributes,omitempty"`
	SPCertPEM    string   `json:"spCertPem,omitempty"`
	SPKeyPEM     string   `json:"spKeyPem,omitempty"`

	// Azure AD / Entra group resolution via Microsoft Graph (optional). When set,
	// group GUIDs are resolved to display names and over-quota memberships are
	// fetched from Graph. Applies to both the OIDC and SAML Azure configs.
	GraphTenantID     string `json:"graphTenantId,omitempty"`
	GraphClientID     string `json:"graphClientId,omitempty"`
	GraphClientSecret string `json:"graphClientSecret,omitempty"`
}

// graphConfig returns the Microsoft Graph credentials from the stored config.
func (c ProviderConfig) graphConfig() GraphConfig {
	return GraphConfig{TenantID: c.GraphTenantID, ClientID: c.GraphClientID, ClientSecret: c.GraphClientSecret}
}

// OIDCMapping derives the claim-name mapping from the stored config.
func (c ProviderConfig) OIDCMapping() OIDCMapping {
	return OIDCMapping{EmailClaim: c.EmailClaim, NameClaim: c.NameClaim, GroupsClaim: c.GroupsClaim}
}

// SAMLMapping derives the attribute-name mapping from the stored config.
func (c ProviderConfig) SAMLMapping() SAMLMapping {
	return SAMLMapping{EmailAttrs: c.EmailAttrs, NameAttrs: c.NameAttrs, GroupsAttrs: c.GroupsAttrs}
}

// HasOIDC reports whether the config has the minimum to build an OIDC provider.
func (c ProviderConfig) HasOIDC() bool { return c.IssuerURL != "" && c.ClientID != "" }

// HasSAML reports whether the config has the minimum to build a SAML provider.
func (c ProviderConfig) HasSAML() bool { return c.MetadataURL != "" || c.MetadataXML != "" }

// BuildOIDCProvider constructs an OIDC provider from a stored config. baseURL is
// the server's external base URL, used to derive the callback redirect URI.
func BuildOIDCProvider(ctx context.Context, c ProviderConfig, baseURL string) (*OIDCProvider, error) {
	return NewOIDCProvider(ctx, OIDCProviderConfig{
		IssuerURL:    c.IssuerURL,
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		RedirectURL:  strings.TrimRight(baseURL, "/") + "/auth/oidc/callback",
		Scopes:       c.Scopes,
		Mapping:      c.OIDCMapping(),
		Graph:        NewGraphClient(c.graphConfig()),
	})
}

// BuildSAMLProvider constructs a SAML provider from a stored config. baseURL is
// the server's external base URL, used to derive the ACS URL.
func BuildSAMLProvider(c ProviderConfig, baseURL string) (*SAMLProvider, error) {
	return NewSAMLProvider(SAMLProviderConfig{
		MetadataURL:  c.MetadataURL,
		MetadataXML:  c.MetadataXML,
		EntityID:     c.EntityID,
		ACSURL:       strings.TrimRight(baseURL, "/") + "/auth/saml/acs",
		CertPEM:      c.SPCertPEM,
		KeyPEM:       c.SPKeyPEM,
		NameIDFormat: c.NameIDFormat,
		Mapping:      c.SAMLMapping(),
		Graph:        NewGraphClient(c.graphConfig()),
	})
}

// LoadProviderConfig decrypts and decodes the stored config for one provider
// type ("oidc" or "saml"). It returns (nil, nil) when no config is stored.
func LoadProviderConfig(ctx context.Context, q db.Querier, masterKey []byte, providerType string) (*ProviderConfig, error) {
	row, err := q.GetAuthProviderConfig(ctx, providerType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("loading %s provider config: %w", providerType, err)
	}
	plaintext, _, err := secret.Decrypt(row.ConfigEnc, masterKey)
	if err != nil {
		return nil, fmt.Errorf("decrypting %s provider config: %w", providerType, err)
	}
	var cfg ProviderConfig
	if err := json.Unmarshal(plaintext, &cfg); err != nil {
		return nil, fmt.Errorf("decoding %s provider config: %w", providerType, err)
	}
	return &cfg, nil
}
