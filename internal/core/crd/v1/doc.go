// Package v1 contains the CRD types for the flint.dev/v1 API group.
//
// CRDs:
//   - Project — registers a repository as a Flint project
//   - RunnerPool — defines a named compute pool with K8s scheduling specs
//   - StepTemplate — a reusable step referenced from pipeline YAML via use:
//
// Forge connections and auth providers are DB-managed (via the API), not CRDs.
//
// +kubebuilder:object:generate=true
// +groupName=flint.dev
package v1

const (
	Group   = "flint.dev"
	Version = "v1"
)
