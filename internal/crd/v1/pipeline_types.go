package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Repo",type=string,JSONPath=`.spec.repo`
// +kubebuilder:printcolumn:name="Branch",type=string,JSONPath=`.spec.defaultBranch`
// +kubebuilder:printcolumn:name="Archived",type=boolean,JSONPath=`.spec.archived`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Registered")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:resource:shortName=pl

// Pipeline registers a repository as a Flint project. The controller syncs
// this to the projects table, configures forge webhooks, and writes status back.
type Pipeline struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PipelineSpec   `json:"spec"`
	Status PipelineStatus `json:"status,omitempty"`
}

// PipelineSpec defines the desired state of a pipeline project.
type PipelineSpec struct {
	// Repo is the full repository path (e.g., "acme/skills-service").
	Repo string `json:"repo"`

	// ForgeRef references the ForgeConnection to use for this repo.
	ForgeRef string `json:"forgeRef"`

	// DisplayName is a human-readable project name. Defaults to repo name.
	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// Description is a one-line project description.
	// +optional
	Description string `json:"description,omitempty"`

	// Colour is the project accent colour (hex). Auto-assigned if empty.
	// +optional
	// +kubebuilder:validation:Pattern=`^#[0-9a-fA-F]{6}$`
	Colour string `json:"colour,omitempty"`

	// Icon is an emoji or SVG URL for the project avatar.
	// +optional
	Icon string `json:"icon,omitempty"`

	// Workspace is the workspace slug that contains this project.
	// +optional
	Workspace string `json:"workspace,omitempty"`

	// Tags are arbitrary labels for filtering and grouping.
	// +optional
	Tags []string `json:"tags,omitempty"`

	// DefaultBranch is the primary branch. Defaults to "main".
	// +optional
	// +kubebuilder:default=main
	DefaultBranch string `json:"defaultBranch,omitempty"`

	// PipelineSource defines where pipeline YAML files live.
	// +optional
	PipelineSource *PipelineSourceSpec `json:"pipelineSource,omitempty"`

	// Archived marks the project as archived. Archived projects don't trigger runs.
	// +optional
	Archived bool `json:"archived,omitempty"`
}

// PipelineSourceSpec defines where pipeline YAML is fetched from.
type PipelineSourceSpec struct {
	// Type is "self" (default — YAML lives in the repo) or "external" (central pipeline repo).
	// +kubebuilder:validation:Enum=self;external
	// +kubebuilder:default=self
	Type string `json:"type"`

	// Path is the directory containing pipeline YAML. Defaults to ".flint/".
	// +optional
	// +kubebuilder:default=".flint/"
	Path string `json:"path,omitempty"`

	// Repo is the external pipeline repository (required when type is "external").
	// +optional
	Repo string `json:"repo,omitempty"`

	// Ref is the branch/tag in the external repo. Defaults to "main".
	// +optional
	Ref string `json:"ref,omitempty"`
}

// PipelineStatus defines the observed state of a pipeline project.
type PipelineStatus struct {
	// Conditions represent the latest observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ProjectID is the internal database ID assigned after sync.
	// +optional
	ProjectID string `json:"projectId,omitempty"`

	// WebhookID is the forge webhook ID created for this project.
	// +optional
	WebhookID string `json:"webhookId,omitempty"`

	// LastSyncTime is the last time the CRD was synced to the database.
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`
}

// +kubebuilder:object:root=true

// PipelineList contains a list of Pipelines.
type PipelineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Pipeline `json:"items"`
}
