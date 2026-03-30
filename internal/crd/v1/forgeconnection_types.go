package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:resource:shortName=fc

// ForgeConnection declares a Git forge integration. Platform teams create these
// to connect GitHub Apps, GitLab OAuth apps, or Bitbucket OAuth consumers.
// Credentials are referenced via K8s Secrets — never inline in the CRD.
type ForgeConnection struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ForgeConnectionSpec   `json:"spec"`
	Status ForgeConnectionStatus `json:"status,omitempty"`
}

// ForgeConnectionSpec defines the desired state of a forge connection.
type ForgeConnectionSpec struct {
	// Type is the forge provider type.
	// +kubebuilder:validation:Enum=github;gitlab;bitbucket
	Type string `json:"type"`

	// DisplayName is a human-readable name for this connection.
	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// GitHub-specific configuration. Required when type is "github".
	// +optional
	GitHub *GitHubConnectionSpec `json:"github,omitempty"`

	// GitLab-specific configuration. Required when type is "gitlab".
	// +optional
	GitLab *GitLabConnectionSpec `json:"gitlab,omitempty"`

	// Bitbucket-specific configuration. Required when type is "bitbucket".
	// +optional
	Bitbucket *BitbucketConnectionSpec `json:"bitbucket,omitempty"`
}

// GitHubConnectionSpec configures a GitHub App connection.
type GitHubConnectionSpec struct {
	// AppID is the GitHub App ID.
	AppID string `json:"appId"`

	// InstallationID is the GitHub App installation ID for this org.
	InstallationID string `json:"installationId"`

	// PrivateKeyRef references a K8s Secret containing the GitHub App private key (PEM).
	PrivateKeyRef SecretKeyRef `json:"privateKeyRef"`

	// WebhookSecretRef references a K8s Secret containing the webhook signing secret.
	WebhookSecretRef SecretKeyRef `json:"webhookSecretRef"`
}

// GitLabConnectionSpec configures a GitLab OAuth connection.
type GitLabConnectionSpec struct {
	// BaseURL is the GitLab instance URL. Defaults to "https://gitlab.com".
	// +optional
	BaseURL string `json:"baseUrl,omitempty"`

	// CredentialsRef references a K8s Secret containing clientId and clientSecret.
	CredentialsRef SecretRef `json:"credentialsRef"`
}

// BitbucketConnectionSpec configures a Bitbucket OAuth connection.
type BitbucketConnectionSpec struct {
	// CredentialsRef references a K8s Secret containing clientId and clientSecret.
	CredentialsRef SecretRef `json:"credentialsRef"`
}

// SecretKeyRef references a specific key within a K8s Secret.
type SecretKeyRef struct {
	// Name of the K8s Secret.
	Name string `json:"name"`
	// Key within the Secret.
	Key string `json:"key"`
}

// SecretRef references a K8s Secret by name. Expected to contain known keys
// depending on context (e.g., "client-id" and "client-secret" for OAuth).
type SecretRef struct {
	// Name of the K8s Secret.
	Name string `json:"name"`
}

// ForgeConnectionStatus defines the observed state of a forge connection.
type ForgeConnectionStatus struct {
	// Conditions represent the latest observations of the connection state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ConnectionID is the internal database ID assigned after sync.
	// +optional
	ConnectionID string `json:"connectionId,omitempty"`

	// LastVerifiedAt is the last time credentials were verified against the forge API.
	// +optional
	LastVerifiedAt *metav1.Time `json:"lastVerifiedAt,omitempty"`
}

// +kubebuilder:object:root=true

// ForgeConnectionList contains a list of ForgeConnections.
type ForgeConnectionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ForgeConnection `json:"items"`
}
