// Package runner handles RunnerPool resolution — translating developer-facing
// pool names and t-shirt sizes into Kubernetes scheduling fragments
// (resource requests, nodeSelector, tolerations).
//
// Developers write: runner: standard
// Platform teams define: RunnerPool CRD with K8s scheduling details
// This package bridges the two.
package runner

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// PoolSpec is the resolved scheduling specification for a runner pool.
// It contains everything needed to augment a K8s Job spec.
type PoolSpec struct {
	Name         string
	Description  string
	Resources    ResourceProfile
	NodeSelector map[string]string
	Tolerations  []corev1.Toleration
	Spot         bool
}

// ResourceProfile defines the compute resources for a runner.
type ResourceProfile struct {
	CPU    resource.Quantity
	Memory resource.Quantity
	GPU    *GPURequest
}

// GPURequest specifies GPU or accelerator requirements.
type GPURequest struct {
	Vendor       string // e.g., "nvidia", "amd", "google", "aws", "habana"
	Model        string // e.g., "t4", "a100", "tpu-v5", "inferentia2", "gaudi2"
	Count        int
	ResourceName string // override K8s resource name; default: "{vendor}.com/gpu"
}

// K8sResourceName returns the Kubernetes resource name for this accelerator.
// Uses ResourceName if set, otherwise derives it from Vendor.
func (g *GPURequest) K8sResourceName() string {
	if g.ResourceName != "" {
		return g.ResourceName
	}
	return g.Vendor + ".com/gpu"
}
