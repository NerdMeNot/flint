// Package secretstore provides the PostgreSQL-backed SecretStore implementation
// for Flint. This is the "internal" store — secrets encrypted at rest with
// AES-256-GCM envelope encryption.
package secretstore

import (
	"context"
	"fmt"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/flinterr"
	"github.com/NerdMeNot/flint/pkg/secret"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore implements secret.SecretStore backed by PostgreSQL
// with envelope encryption.
type PostgresStore struct {
	pool             *pgxpool.Pool
	q                *db.Queries
	masterKey        []byte
	masterKeyVersion byte
}

// NewPostgresStore creates a PostgresStore.
// masterKey must be exactly 32 bytes (AES-256).
func NewPostgresStore(pool *pgxpool.Pool, masterKey []byte, masterKeyVersion byte) (*PostgresStore, error) {
	if len(masterKey) != 32 {
		return nil, fmt.Errorf("secretstore: master key must be exactly 32 bytes, got %d", len(masterKey))
	}
	return &PostgresStore{
		pool:             pool,
		q:                db.New(pool),
		masterKey:        masterKey,
		masterKeyVersion: masterKeyVersion,
	}, nil
}

func (s *PostgresStore) Get(ctx context.Context, ref secret.Ref) (string, error) {
	if err := validateRef(ref); err != nil {
		return "", err
	}

	projectID := coalesce(ref.ProjectID)
	encryptedValue, err := s.q.GetSecret(ctx, db.GetSecretParams{
		OrgID:     ref.OrgID,
		ProjectID: &projectID,
		Name:      ref.Name,
	})

	if err != nil {
		if err == pgx.ErrNoRows {
			return "", flinterr.NewNotFound(fmt.Sprintf("secret %q not found", ref.Name))
		}
		return "", flinterr.WrapInternal("failed to query secret", err)
	}

	plaintext, _, err := secret.Decrypt(encryptedValue, s.masterKey)
	if err != nil {
		return "", flinterr.WrapInternal("failed to decrypt secret", err)
	}

	return string(plaintext), nil
}

func (s *PostgresStore) Set(ctx context.Context, ref secret.Ref, value string) error {
	if err := validateRef(ref); err != nil {
		return err
	}

	encrypted, err := secret.Encrypt([]byte(value), s.masterKey, s.masterKeyVersion)
	if err != nil {
		return flinterr.WrapInternal("failed to encrypt secret", err)
	}

	var projectID *string
	if ref.ProjectID != "" {
		projectID = &ref.ProjectID
	}

	err = s.q.UpsertSecret(ctx, db.UpsertSecretParams{
		OrgID:          ref.OrgID,
		ProjectID:      projectID,
		Name:           ref.Name,
		EncryptedValue: encrypted,
		UpdatedAt:      time.Now().UTC(),
	})
	if err != nil {
		return flinterr.WrapInternal("failed to store secret", err)
	}

	return nil
}

func (s *PostgresStore) Delete(ctx context.Context, ref secret.Ref) error {
	if err := validateRef(ref); err != nil {
		return err
	}

	projectID := coalesce(ref.ProjectID)
	rowsAffected, err := s.q.DeleteSecret(ctx, db.DeleteSecretParams{
		OrgID:     ref.OrgID,
		ProjectID: &projectID,
		Name:      ref.Name,
	})
	if err != nil {
		return flinterr.WrapInternal("failed to delete secret", err)
	}

	if rowsAffected == 0 {
		return flinterr.NewNotFound(fmt.Sprintf("secret %q not found", ref.Name))
	}

	return nil
}

func (s *PostgresStore) List(ctx context.Context, scope secret.Scope) ([]secret.Entry, error) {
	if scope.OrgID == "" {
		return nil, flinterr.NewInvalidInput("org_id is required")
	}

	projectID := coalesce(scope.ProjectID)
	rows, err := s.q.ListSecrets(ctx, db.ListSecretsParams{
		OrgID:     scope.OrgID,
		ProjectID: &projectID,
	})
	if err != nil {
		return nil, flinterr.WrapInternal("failed to list secrets", err)
	}

	var entries []secret.Entry
	for _, row := range rows {
		entries = append(entries, secret.Entry{
			Name:      row.Name,
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
			Provider:  s.Provider(),
		})
	}

	return entries, nil
}

func (s *PostgresStore) Provider() string {
	return "internal"
}

// RotateMasterKey re-encrypts all secrets with a new master key.
func (s *PostgresStore) RotateMasterKey(ctx context.Context, newMasterKey []byte, newKeyVersion byte) (int, error) {
	if len(newMasterKey) != 32 {
		return 0, fmt.Errorf("secretstore: new master key must be exactly 32 bytes")
	}

	all, err := s.q.ListAllSecrets(ctx)
	if err != nil {
		return 0, flinterr.WrapInternal("failed to query secrets for rotation", err)
	}

	rotated := 0
	for _, r := range all {
		newBlob, err := secret.ReEncrypt(r.EncryptedValue, s.masterKey, newMasterKey, newKeyVersion)
		if err != nil {
			return rotated, fmt.Errorf("secretstore: re-encrypt secret %s: %w", r.ID, err)
		}

		err = s.q.UpdateSecretValue(ctx, db.UpdateSecretValueParams{
			ID:             r.ID,
			EncryptedValue: newBlob,
			UpdatedAt:      time.Now().UTC(),
		})
		if err != nil {
			return rotated, flinterr.WrapInternal(fmt.Sprintf("failed to update secret %s", r.ID), err)
		}
		rotated++
	}

	s.masterKey = newMasterKey
	s.masterKeyVersion = newKeyVersion
	return rotated, nil
}

func validateRef(ref secret.Ref) error {
	if ref.OrgID == "" {
		return flinterr.NewInvalidInput("org_id is required")
	}
	if ref.Name == "" {
		return flinterr.NewInvalidInput("secret name is required")
	}
	return nil
}

func coalesce(s string) string { return s }
