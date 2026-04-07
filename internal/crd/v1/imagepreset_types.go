package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.spec.image`
// +kubebuilder:printcolumn:name="Description",type=string,JSONPath=`.spec.description`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:resource:shortName=ip

// ImagePreset defines a named, version-controlled container image reference.
// Platform teams create presets; pipeline authors reference them by name
// in the image: field instead of specifying full image references.
type ImagePreset struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ImagePresetSpec `json:"spec"`
}

// ImagePresetSpec defines the image preset's configuration.
type ImagePresetSpec struct {
	// Image is the full container image reference (e.g. "node:22-alpine").
	Image string `json:"image"`

	// Description is a human-readable summary.
	Description string `json:"description,omitempty"`

	// Tags are labels for categorization (e.g. ["javascript", "frontend"]).
	Tags []string `json:"tags,omitempty"`
}

// +kubebuilder:object:root=true

// ImagePresetList contains a list of ImagePreset.
type ImagePresetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImagePreset `json:"items"`
}
