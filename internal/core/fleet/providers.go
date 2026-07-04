package fleet

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/pkg/compute"
	"github.com/NerdMeNot/flint/pkg/secret"
)

// providerCacheTTL bounds how long a constructed provider is reused before
// its DB config is re-read (API edits reach the fleet within this window).
const providerCacheTTL = 30 * time.Second

type cachedProvider struct {
	provider compute.Provider
	loadedAt time.Time
}

// SetMasterKey enables decryption of provider credentials stored via the API.
// Without it, only ambient-credential providers construct.
func (f *Fleet) SetMasterKey(key []byte) { f.masterKey = key }

// SetProviderResolver overrides provider construction (tests inject fakes).
func (f *Fleet) SetProviderResolver(r func(ctx context.Context, name string) (compute.Provider, error)) {
	f.providerResolver = r
}

// provider resolves a compute provider by its configured name, constructing
// from the compute_providers row and caching briefly.
func (f *Fleet) provider(ctx context.Context, name string) (compute.Provider, error) {
	if f.providerResolver != nil {
		return f.providerResolver(ctx, name)
	}

	f.provMu.Lock()
	if c, ok := f.providers[name]; ok && time.Since(c.loadedAt) < providerCacheTTL {
		f.provMu.Unlock()
		return c.provider, nil
	}
	f.provMu.Unlock()

	row, err := db.New(f.pool).GetComputeProvider(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("fleet: compute provider %q not found: %w", name, err)
	}
	var creds []byte
	if len(row.CredentialsEnc) > 0 {
		if len(f.masterKey) != 32 {
			return nil, errors.New("fleet: provider has stored credentials but no master key is configured")
		}
		if creds, _, err = secret.Decrypt(row.CredentialsEnc, f.masterKey); err != nil {
			return nil, fmt.Errorf("fleet: decrypt credentials for %q: %w", name, err)
		}
	}
	p, err := compute.New(ctx, row.ProviderType, row.Name, row.Config, creds)
	if err != nil {
		return nil, err
	}

	f.provMu.Lock()
	if f.providers == nil {
		f.providers = map[string]cachedProvider{}
	}
	f.providers[name] = cachedProvider{provider: p, loadedAt: time.Now()}
	f.provMu.Unlock()
	return p, nil
}

// providerCache fields live on Fleet.
type providerCache struct {
	provMu           sync.Mutex
	providers        map[string]cachedProvider
	masterKey        []byte
	providerResolver func(ctx context.Context, name string) (compute.Provider, error)
	bootstrap        BootstrapEndpoints
	zombieSightings  map[string]int
}
