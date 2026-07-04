package runner

import (
	"fmt"
	"sync"

	"github.com/NerdMeNot/flint/internal/core/flinterr"
)

// DefaultPoolName is the pool used when a job specifies no runner. Overridable
// via SetDefault from config (engine.defaultPool).
var defaultPoolName = "standard"

// SetDefault sets the registry-wide default pool name (from config).
func SetDefault(name string) {
	if name != "" {
		defaultPoolName = name
	}
}

// Registry holds known machine pools. Pools are loaded from the DB (the source
// of truth) into this in-memory registry (see LoadAll). Safe for concurrent
// reads (dispatch) and refresh (loader).
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

// Resolve looks up a machine pool by name. If the name is empty, the default pool is returned.
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
		spec.Resources.CPUMillis = profile.CPUMillis
		spec.Resources.MemoryMB = profile.MemoryMB
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
