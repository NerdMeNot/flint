package compute

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// Factory builds a Provider instance from a stored provider row: its
// configured name, freeform config, and (optionally) decrypted credentials.
// Credentials are nil when the provider uses ambient auth (instance role,
// env, shared config chain).
type Factory func(ctx context.Context, name string, config json.RawMessage, credentials []byte) (Provider, error)

var (
	regMu     sync.RWMutex
	factories = map[string]Factory{}
)

// Register makes a provider type ("static", "aws") constructible by New.
// Called from implementation packages' init(); a duplicate type panics —
// that's a programming error, not a runtime condition.
func Register(providerType string, f Factory) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := factories[providerType]; dup {
		panic(fmt.Sprintf("compute: provider type %q registered twice", providerType))
	}
	factories[providerType] = f
}

// New constructs a Provider of the given registered type.
func New(ctx context.Context, providerType, name string, config json.RawMessage, credentials []byte) (Provider, error) {
	regMu.RLock()
	f, ok := factories[providerType]
	regMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("compute: unknown provider type %q (registered: %v)", providerType, Types())
	}
	return f(ctx, name, config, credentials)
}

// Types lists the registered provider types, sorted.
func Types() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(factories))
	for t := range factories {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
