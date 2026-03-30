package runner

import (
	"fmt"

	"github.com/NerdMeNot/flint/internal/flinterr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Registry holds known runner pools. In production this is populated from
// RunnerPool CRDs; in dev/test it can be populated statically.
type Registry struct {
	pools map[string]PoolSpec
}

// NewRegistry creates an empty pool registry.
func NewRegistry() *Registry {
	return &Registry{pools: make(map[string]PoolSpec)}
}

// Register adds a pool to the registry.
func (r *Registry) Register(spec PoolSpec) {
	r.pools[spec.Name] = spec
}

// Resolve looks up a runner pool by name. If the name is empty, the default pool is returned.
func (r *Registry) Resolve(name string) (*PoolSpec, error) {
	if name == "" {
		name = "standard"
	}

	spec, ok := r.pools[name]
	if !ok {
		available := make([]string, 0, len(r.pools))
		for k := range r.pools {
			available = append(available, k)
		}
		return nil, flinterr.NewNotFound(
			fmt.Sprintf("runner pool %q not found (available: %v)", name, available),
		)
	}

	return &spec, nil
}

// ResolveWithSize resolves a pool by name, then overrides resources with a t-shirt size if specified.
func (r *Registry) ResolveWithSize(name string, size TShirtSize) (*PoolSpec, error) {
	spec, err := r.Resolve(name)
	if err != nil {
		return nil, err
	}

	if size != "" {
		profile, ok := ResourcesForSize(size)
		if !ok {
			return nil, flinterr.NewInvalidInput(
				fmt.Sprintf("unknown runner size %q (available: %v)", size, AllSizes()),
			)
		}
		spec.Resources.CPU = profile.CPU
		spec.Resources.Memory = profile.Memory
	}

	return spec, nil
}

// List returns all registered pool specs.
func (r *Registry) List() []PoolSpec {
	specs := make([]PoolSpec, 0, len(r.pools))
	for _, s := range r.pools {
		specs = append(specs, s)
	}
	return specs
}

// MergeIntoJob applies the pool's scheduling spec to a K8s Job template.
// This is the bridge between developer-facing pool names and K8s reality.
func MergeIntoJob(spec *PoolSpec, job *batchv1.Job) {
	if job.Spec.Template.Spec.Containers == nil {
		return
	}

	podSpec := &job.Spec.Template.Spec

	// Resource requests and limits on the first container.
	container := &podSpec.Containers[0]
	if container.Resources.Requests == nil {
		container.Resources.Requests = corev1.ResourceList{}
	}
	if container.Resources.Limits == nil {
		container.Resources.Limits = corev1.ResourceList{}
	}

	container.Resources.Requests[corev1.ResourceCPU] = spec.Resources.CPU
	container.Resources.Requests[corev1.ResourceMemory] = spec.Resources.Memory
	container.Resources.Limits[corev1.ResourceCPU] = spec.Resources.CPU
	container.Resources.Limits[corev1.ResourceMemory] = spec.Resources.Memory

	// Accelerator resources (GPU, TPU, Inferentia, Gaudi, etc.).
	if spec.Resources.GPU != nil && spec.Resources.GPU.Count > 0 {
		resName := corev1.ResourceName(spec.Resources.GPU.K8sResourceName())
		qty := resource.MustParse(fmt.Sprintf("%d", spec.Resources.GPU.Count))
		container.Resources.Requests[resName] = qty
		container.Resources.Limits[resName] = qty
	}

	// Node selector.
	if len(spec.NodeSelector) > 0 {
		if podSpec.NodeSelector == nil {
			podSpec.NodeSelector = make(map[string]string)
		}
		for k, v := range spec.NodeSelector {
			podSpec.NodeSelector[k] = v
		}
	}

	// Tolerations.
	if len(spec.Tolerations) > 0 {
		podSpec.Tolerations = append(podSpec.Tolerations, spec.Tolerations...)
	}
}
