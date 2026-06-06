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

	// ServiceAccountName is the K8s ServiceAccount assigned to step pods
	// in this pool. Used for IAM role-based auth:
	//   - EKS IRSA: annotate the SA with eks.amazonaws.com/role-arn
	//   - GKE Workload Identity: annotate with iam.gke.io/gcp-service-account
	//   - AKS Workload Identity: annotate with azure.workload.identity/client-id
	//
	// When set, step pods automatically receive cloud credentials via the
	// platform's token projection — no secrets stored in Flint.
	ServiceAccountName string

	// Workspace configures how step pods share the /workspace directory.
	Workspace WorkspaceConfig
}

// WorkspaceMode selects the workspace storage backend.
type WorkspaceMode string

const (
	// WorkspaceModeAgent uses emptyDir per pod + per-run gRPC workspace agent
	// for incremental sync between steps. Default; needs no cluster storage.
	WorkspaceModeAgent WorkspaceMode = "agent"

	// WorkspaceModePVC creates a ReadWriteMany PVC per run, shared by all step
	// pods. No sync needed — all pods mount the same volume. Requires a
	// StorageClass that supports ReadWriteMany (EFS, CephFS, NFS, FSx Lustre).
	WorkspaceModePVC WorkspaceMode = "pvc"

	// WorkspaceModeS3 uses S3 for incremental workspace sync between steps.
	// Each step uses a local emptyDir; sync happens via S3 with per-run key
	// prefix. No infrastructure beyond an S3 bucket. Slowest but most portable.
	WorkspaceModeS3 WorkspaceMode = "s3"
)

// WorkspaceConfig holds the resolved workspace settings for a pool.
type WorkspaceConfig struct {
	Mode         WorkspaceMode
	StorageClass string // required for PVC mode
	Size         string // PVC size, default "10Gi"
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
