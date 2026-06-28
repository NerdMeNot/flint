package runner

import (
	"fmt"
	"sync"

	"github.com/NerdMeNot/flint/internal/core/flinterr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// DefaultPoolName is the pool used when a job specifies no runner. Overridable
// via SetDefault from config (worker.defaultRunnerPool).
var defaultPoolName = "standard"

// SetDefault sets the registry-wide default pool name (from config).
func SetDefault(name string) {
	if name != "" {
		defaultPoolName = name
	}
}

// Registry holds known runner pools. Pools are loaded from the DB (the source of
// truth) into this in-memory registry by the worker (see LoadAll). Safe for
// concurrent reads (dispatch) and refresh (loader).
type Registry struct {
	mu    sync.RWMutex
	pools map[string]PoolSpec
}

// NewRegistry creates an empty pool registry.
func NewRegistry() *Registry {
	return &Registry{pools: make(map[string]PoolSpec)}
}

// Register adds (or replaces) a pool in the registry.
func (r *Registry) Register(spec PoolSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pools[spec.Name] = spec
}

// ReplaceAll atomically swaps the registry contents — used by the DB loader so a
// refresh never exposes a partially-populated registry.
func (r *Registry) ReplaceAll(specs []PoolSpec) {
	next := make(map[string]PoolSpec, len(specs))
	for _, s := range specs {
		next[s.Name] = s
	}
	r.mu.Lock()
	r.pools = next
	r.mu.Unlock()
}

// Resolve looks up a runner pool by name. If the name is empty, the default pool is returned.
func (r *Registry) Resolve(name string) (*PoolSpec, error) {
	if name == "" {
		name = defaultPoolName
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
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
	r.mu.RLock()
	defer r.mu.RUnlock()
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

	// CPU/memory are optional pool defaults — only stamp them when the pool set a
	// value (zero Quantity means "unset", so the job's own requests / cluster
	// defaults apply).
	if !spec.Resources.CPU.IsZero() {
		container.Resources.Requests[corev1.ResourceCPU] = spec.Resources.CPU
		container.Resources.Limits[corev1.ResourceCPU] = spec.Resources.CPU
	}
	if !spec.Resources.Memory.IsZero() {
		container.Resources.Requests[corev1.ResourceMemory] = spec.Resources.Memory
		container.Resources.Limits[corev1.ResourceMemory] = spec.Resources.Memory
	}

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

	// Service account — enables IAM role-based auth (IRSA, Workload Identity).
	if spec.ServiceAccountName != "" {
		podSpec.ServiceAccountName = spec.ServiceAccountName
	}
}
