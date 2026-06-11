package secretstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/flinterr"
	"github.com/NerdMeNot/flint/pkg/secret"
	"github.com/jackc/pgx/v5"
)

// EnvVarStore is a read adapter over Flint-managed secret env-variables. It
// implements the read side of secret.SecretStore so the agent secret-injection
// endpoint can fetch decrypted values at runtime. Writes happen through the
// env-variable HTTP handlers (which own the richer env_variables schema), so
// Set/Delete report read-only.
type EnvVarStore struct {
	q         db.Querier
	masterKey []byte
}

// NewEnvVarStore creates an EnvVarStore. masterKey must be exactly 32 bytes
// (AES-256) — the same master key used to encrypt the values on write.
func NewEnvVarStore(q db.Querier, masterKey []byte) (*EnvVarStore, error) {
	if len(masterKey) != 32 {
		return nil, fmt.Errorf("secretstore: master key must be exactly 32 bytes, got %d", len(masterKey))
	}
	return &EnvVarStore{q: q, masterKey: masterKey}, nil
}

// Get returns the decrypted value of a Flint-managed secret env-variable. The
// environment-scoped value is preferred when ref.Environment is set; otherwise
// (or as a fallback) the global value is used.
func (s *EnvVarStore) Get(ctx context.Context, ref secret.Ref) (string, error) {
	if ref.OrgID == "" {
		return "", flinterr.NewInvalidInput("org_id is required")
	}
	if ref.Name == "" {
		return "", flinterr.NewInvalidInput("secret name is required")
	}

	enc, err := s.q.GetSecretEnvVarValue(ctx, db.GetSecretEnvVarValueParams{
		OrgID:   ref.OrgID,
		Name:    ref.Name,
		EnvSlug: ref.Environment,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", flinterr.NewNotFound(fmt.Sprintf("secret %q not found", ref.Name))
		}
		return "", flinterr.WrapInternal("failed to query secret env var", err)
	}
	if len(enc) == 0 {
		return "", flinterr.NewNotFound(fmt.Sprintf("secret %q has no value", ref.Name))
	}

	plaintext, _, err := secret.Decrypt(enc, s.masterKey)
	if err != nil {
		return "", flinterr.WrapInternal("failed to decrypt secret env var", err)
	}
	return string(plaintext), nil
}

// Set is unsupported — secret env-variables are written through the env-variable
// HTTP handlers, not this store.
func (s *EnvVarStore) Set(ctx context.Context, ref secret.Ref, value string) error {
	return secret.ErrReadOnly
}

// Delete is unsupported — see Set.
func (s *EnvVarStore) Delete(ctx context.Context, ref secret.Ref) error {
	return secret.ErrReadOnly
}

// List returns no entries; enumeration is served by the env-variable handlers.
func (s *EnvVarStore) List(ctx context.Context, scope secret.Scope) ([]secret.Entry, error) {
	return nil, nil
}

// Provider identifies this backend.
func (s *EnvVarStore) Provider() string { return "env-var" }
