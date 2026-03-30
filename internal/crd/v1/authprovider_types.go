package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:resource:shortName=ap

// AuthProvider declares an authentication provider (OIDC or SAML).
// Platform teams create these to configure SSO. Credentials are
// referenced via K8s Secrets — never inline in the CRD.
type AuthProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AuthProviderSpec   `json:"spec"`
	Status AuthProviderStatus `json:"status,omitempty"`
}

// AuthProviderSpec defines the desired state of an auth provider.
type AuthProviderSpec struct {
	// Type is the authentication protocol.
	// +kubebuilder:validation:Enum=oidc;saml
	Type string `json:"type"`

	// OIDC-specific configuration. Required when type is "oidc".
	// +optional
	OIDC *OIDCProviderSpec `json:"oidc,omitempty"`

	// SAML-specific configuration. Required when type is "saml".
	// +optional
	SAML *SAMLProviderSpec `json:"saml,omitempty"`
}

// OIDCProviderSpec configures an OIDC provider connection.
type OIDCProviderSpec struct {
	// IssuerURL is the OIDC issuer URL (e.g., https://accounts.google.com).
	IssuerURL string `json:"issuerUrl"`

	// ClientID is the OIDC client ID.
	ClientID string `json:"clientId"`

	// ClientSecretRef references a K8s Secret containing the OIDC client secret.
	ClientSecretRef SecretKeyRef `json:"clientSecretRef"`

	// Scopes to request from the OIDC provider. Defaults to [openid, profile, email, groups].
	// +optional
	Scopes []string `json:"scopes,omitempty"`
}

// SAMLProviderSpec configures a SAML Service Provider connection.
type SAMLProviderSpec struct {
	// MetadataURL is the IdP metadata URL.
	MetadataURL string `json:"metadataUrl"`

	// EntityID is the SP entity ID. Defaults to {baseURL}/auth/saml/metadata.
	// +optional
	EntityID string `json:"entityId,omitempty"`

	// CertificateRef references a K8s Secret containing the SP signing certificate (PEM).
	// +optional
	CertificateRef *SecretKeyRef `json:"certificateRef,omitempty"`

	// PrivateKeyRef references a K8s Secret containing the SP signing private key (PEM).
	// +optional
	PrivateKeyRef *SecretKeyRef `json:"privateKeyRef,omitempty"`
}

// AuthProviderStatus defines the observed state of an auth provider.
type AuthProviderStatus struct {
	// Conditions represent the latest observations of the provider state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ProviderID is the internal database ID assigned after sync.
	// +optional
	ProviderID string `json:"providerId,omitempty"`

	// LastVerifiedAt is the last time the provider config was verified.
	// +optional
	LastVerifiedAt *metav1.Time `json:"lastVerifiedAt,omitempty"`
}

// +kubebuilder:object:root=true

// AuthProviderList contains a list of AuthProviders.
type AuthProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AuthProvider `json:"items"`
}
