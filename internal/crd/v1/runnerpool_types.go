package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="CPU",type=string,JSONPath=`.spec.profile.cpu`
// +kubebuilder:printcolumn:name="Memory",type=string,JSONPath=`.spec.profile.memory`
// +kubebuilder:printcolumn:name="GPU",type=string,JSONPath=`.spec.profile.gpu.model`
// +kubebuilder:printcolumn:name="Spot",type=boolean,JSONPath=`.spec.spot.preferred`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:resource:shortName=rp

// RunnerPool defines a named compute pool. Developers reference pools by name
// in pipeline YAML (runner: gpu). Platform teams define the K8s scheduling
// details here — developers never see nodeSelector or tolerations.
type RunnerPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RunnerPoolSpec   `json:"spec"`
	Status RunnerPoolStatus `json:"status,omitempty"`
}

// RunnerPoolSpec defines the compute profile and K8s scheduling for a runner pool.
type RunnerPoolSpec struct {
	// Description is human-readable text shown to developers via `flint runner list`.
	// +optional
	Description string `json:"description,omitempty"`

	// Profile defines the compute resources visible to developers.
	Profile ResourceProfileSpec `json:"profile"`

	// Scheduling defines K8s-level scheduling details. Platform team only.
	// +optional
	Scheduling *SchedulingSpec `json:"scheduling,omitempty"`

	// Spot configures spot/preemptible instance preferences.
	// +optional
	Spot *SpotSpec `json:"spot,omitempty"`

	// DefaultTimeout is the default step timeout for this pool.
	// +optional
	DefaultTimeout string `json:"defaultTimeout,omitempty"`
}

// ResourceProfileSpec defines what developers see — human-readable compute resources.
type ResourceProfileSpec struct {
	// CPU cores (e.g., "2", "8", "0.5").
	CPU string `json:"cpu"`

	// Memory (e.g., "4Gi", "32Gi").
	Memory string `json:"memory"`

	// GPU requirements.
	// +optional
	GPU *GPUSpec `json:"gpu,omitempty"`

	// Arch is the CPU architecture. Defaults to "amd64".
	// +optional
	// +kubebuilder:validation:Enum=amd64;arm64
	// +kubebuilder:default=amd64
	Arch string `json:"arch,omitempty"`
}

// GPUSpec defines GPU or accelerator requirements for a runner pool.
type GPUSpec struct {
	// Vendor is the accelerator vendor (e.g., "nvidia", "amd", "google", "aws", "habana").
	Vendor string `json:"vendor"`

	// Model is the accelerator model (e.g., "t4", "a100", "tpu-v5", "inferentia2", "gaudi2").
	Model string `json:"model"`

	// Count is the number of accelerators. Defaults to 1.
	// +optional
	// +kubebuilder:default=1
	Count int `json:"count,omitempty"`

	// ResourceName overrides the auto-derived K8s resource name.
	// By default Flint uses "{vendor}.com/gpu" (e.g., "nvidia.com/gpu").
	// Set this for non-standard accelerator resources:
	//   - "google.com/tpu"            (Google TPU)
	//   - "aws.amazon.com/neuron"     (AWS Inferentia/Trainium)
	//   - "habana.ai/gaudi"           (Intel Gaudi)
	//   - "amd.com/gpu"               (AMD GPU)
	//   - "intel.com/gpu"             (Intel Arc)
	// +optional
	ResourceName string `json:"resourceName,omitempty"`
}

// SchedulingSpec defines K8s-level scheduling — never exposed to developers.
type SchedulingSpec struct {
	// NodeSelector constrains pods to nodes with matching labels.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations allow pods to schedule onto tainted nodes.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

// SpotSpec configures spot/preemptible instance preferences.
type SpotSpec struct {
	// Preferred indicates spot instances are preferred (may still fall back to on-demand).
	// +optional
	Preferred bool `json:"preferred,omitempty"`

	// Fallback controls what happens when spot capacity is unavailable.
	// +optional
	// +kubebuilder:validation:Enum=on-demand;fail
	// +kubebuilder:default=on-demand
	Fallback string `json:"fallback,omitempty"`
}

// RunnerPoolStatus defines the observed state of a runner pool.
type RunnerPoolStatus struct {
	// Conditions represent the latest observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Ready indicates the pool is available for use.
	// +optional
	Ready bool `json:"ready,omitempty"`
}

// +kubebuilder:object:root=true

// RunnerPoolList contains a list of RunnerPools.
type RunnerPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RunnerPool `json:"items"`
}
