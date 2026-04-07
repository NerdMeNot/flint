package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Description",type=string,JSONPath=`.spec.description`
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.spec.image`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:resource:shortName=st

// StepTemplate defines a reusable step definition that pipeline authors
// reference with use: <name>. Managed by the platform team.
type StepTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              StepTemplateSpec `json:"spec"`
}

// StepTemplateSpec defines the step template's configuration.
type StepTemplateSpec struct {
	// Description is a human-readable summary of what this template does.
	Description string `json:"description,omitempty"`

	// Inputs defines the typed parameters that pipeline authors provide via with:.
	Inputs []StepTemplateInput `json:"inputs,omitempty"`

	// Image is the default container image for the step.
	Image string `json:"image,omitempty"`

	// Run is the shell command(s) to execute. Supports ${{ inputs.NAME }} expressions.
	Run string `json:"run"`
}

// StepTemplateInput defines a single typed input parameter.
type StepTemplateInput struct {
	// Name is the input identifier, used in expressions as inputs.NAME.
	Name string `json:"name"`

	// Type is the input type: string, boolean, or choice.
	Type string `json:"type"`

	// Required indicates whether the input must be provided.
	Required bool `json:"required,omitempty"`

	// Default is the default value if not provided.
	Default string `json:"default,omitempty"`

	// Description is a human-readable help text.
	Description string `json:"description,omitempty"`

	// Options lists valid values for choice-type inputs.
	Options []string `json:"options,omitempty"`
}

// +kubebuilder:object:root=true

// StepTemplateList contains a list of StepTemplate.
type StepTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StepTemplate `json:"items"`
}
