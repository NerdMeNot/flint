package cache

import (
	"context"

	"github.com/rs/zerolog/log"
)

// Layered is the two-tier cache persistent machines earn: the machine-local
// store answers first (no network), the remote (S3) tier shares across
// machines. Saves write through to both — synchronous, so a machine dying
// right after a build never loses the cache for the rest of the fleet (and
// the remote tier's HEAD-first skip makes the warm write-through one request).
type Layered struct {
	local  *LocalStore
	remote Cache
}

// NewLayered stacks a local store over a remote tier. remote may be nil
// (no object storage configured) — the local tier still works alone.
func NewLayered(local *LocalStore, remote Cache) *Layered {
	return &Layered{local: local, remote: remote}
}

func (l *Layered) Restore(ctx context.Context, root, key string, paths []string) (bool, error) {
	if hit, err := l.local.Restore(ctx, root, key, paths); err == nil && hit {
		return true, nil
	}
	if l.remote == nil {
		return false, nil
	}
	return l.remote.Restore(ctx, root, key, paths)
}

func (l *Layered) RestoreWithFallback(ctx context.Context, root, key string, restoreKeys []string, paths []string) (string, error) {
	if match, err := l.local.RestoreWithFallback(ctx, root, key, restoreKeys, paths); err == nil && match != "" {
		return match, nil
	}
	if l.remote == nil {
		return "", nil
	}
	// Remote-tier hits are not copied into the local store on read: the next
	// Save on this machine populates it (write-through), which covers the
	// repeat-build case that matters.
	return l.remote.RestoreWithFallback(ctx, root, key, restoreKeys, paths)
}

func (l *Layered) Save(ctx context.Context, root, key string, paths []string) error {
	if err := l.local.Save(ctx, root, key, paths); err != nil {
		// A full local disk must not fail the build — the remote tier is the
		// durable one.
		log.Warn().Err(err).Str("key", key).Msg("cache: local save failed")
	}
	if l.remote == nil {
		return nil
	}
	return l.remote.Save(ctx, root, key, paths)
}
