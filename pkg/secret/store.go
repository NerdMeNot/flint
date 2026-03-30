// Package secret provides the SecretStore interface for managing pipeline
// secrets across multiple backends (internal encrypted store, Kubernetes
// Secrets, HashiCorp Vault, AWS Secrets Manager, etc.).
//
// The interface is designed for production use:
//   - All operations are scoped to org + optional project
//   - Get never returns metadata — only the plaintext value
//   - List never returns values — only names
//   - Errors are classified via flinterr for consistent handling
//   - Backends that don't support writes return ErrReadOnly
package secret

import (
	"context"
	"errors"
	"time"
)

// ErrReadOnly is returned by Set/Delete on backends that don't support writes
// (e.g., Kubernetes Secrets, Vault with read-only policy).
var ErrReadOnly = errors.New("secret: backend is read-only")

// SecretStore is the interface for secret storage and retrieval.
//
// Implementations must be safe for concurrent use.
type SecretStore interface {
	// Get retrieves a secret's plaintext value.
	// Returns flinterr.KindNotFound if the secret does not exist.
	Get(ctx context.Context, ref Ref) (string, error)

	// Set creates or updates a secret.
	// Returns ErrReadOnly if the backend doesn't support writes.
	Set(ctx context.Context, ref Ref, value string) error

	// Delete removes a secret.
	// Returns ErrReadOnly if the backend doesn't support writes.
	// Returns flinterr.KindNotFound if the secret does not exist.
	Delete(ctx context.Context, ref Ref) error

	// List returns metadata for all secrets in a scope. Never returns values.
	List(ctx context.Context, scope Scope) ([]Entry, error)

	// Provider returns a human-readable name for this backend (e.g., "internal", "vault", "aws-sm").
	Provider() string
}

// Ref identifies a specific secret.
type Ref struct {
	OrgID       string
	ProjectID   string // empty = org-level secret
	Environment string // empty = not environment-scoped
	Name        string
}

// Scope identifies a set of secrets to list.
type Scope struct {
	OrgID     string
	ProjectID string // empty = list org-level secrets
}

// Entry is metadata about a secret. Never contains the value.
type Entry struct {
	Name      string
	Provider  string // which backend this secret comes from
	CreatedAt time.Time
	UpdatedAt time.Time
}

// MultiStore resolves secrets across multiple backends with priority ordering.
// The first backend that returns a value wins. Writes go to the primary (first) backend.
type MultiStore struct {
	backends []SecretStore
}

// NewMultiStore creates a MultiStore. The first backend is the primary (used for writes).
// Get/List fan out across all backends; the first match wins.
func NewMultiStore(backends ...SecretStore) *MultiStore {
	return &MultiStore{backends: backends}
}

// Get tries each backend in order and returns the first successful result.
func (m *MultiStore) Get(ctx context.Context, ref Ref) (string, error) {
	var lastErr error
	for _, b := range m.backends {
		val, err := b.Get(ctx, ref)
		if err == nil {
			return val, nil
		}
		lastErr = err
	}
	return "", lastErr
}

// Set writes to the primary (first) backend.
func (m *MultiStore) Set(ctx context.Context, ref Ref, value string) error {
	if len(m.backends) == 0 {
		return ErrReadOnly
	}
	return m.backends[0].Set(ctx, ref, value)
}

// Delete removes from the primary (first) backend.
func (m *MultiStore) Delete(ctx context.Context, ref Ref) error {
	if len(m.backends) == 0 {
		return ErrReadOnly
	}
	return m.backends[0].Delete(ctx, ref)
}

// List aggregates entries from all backends, deduplicating by name
// (first backend wins on conflict).
func (m *MultiStore) List(ctx context.Context, scope Scope) ([]Entry, error) {
	seen := make(map[string]bool)
	var all []Entry

	for _, b := range m.backends {
		entries, err := b.List(ctx, scope)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !seen[e.Name] {
				seen[e.Name] = true
				all = append(all, e)
			}
		}
	}

	return all, nil
}

// Provider returns "multi".
func (m *MultiStore) Provider() string {
	return "multi"
}
