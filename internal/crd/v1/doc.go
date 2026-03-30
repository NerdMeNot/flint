// Package v1 contains the CRD types for the flint.dev/v1 API group.
//
// CRDs:
//   - ForgeConnection — declares a Git forge integration (GitHub App, GitLab OAuth, etc.)
//   - Pipeline — registers a repository as a Flint project
//   - RunnerPool — defines a named compute pool with K8s scheduling specs
//
// +kubebuilder:object:generate=true
// +groupName=flint.dev
package v1

const (
	Group   = "flint.dev"
	Version = "v1"
)
