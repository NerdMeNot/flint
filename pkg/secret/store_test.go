package secret_test

import (
	"context"
	"errors"
	"testing"

	"github.com/NerdMeNot/flint/pkg/secret"
)

// memoryStore is a simple in-memory SecretStore for testing MultiStore.
type memoryStore struct {
	secrets  map[string]string // "org/project/name" → value
	provider string
	readOnly bool
}

func newMemoryStore(provider string, readOnly bool) *memoryStore {
	return &memoryStore{
		secrets:  make(map[string]string),
		provider: provider,
		readOnly: readOnly,
	}
}

func (m *memoryStore) key(ref secret.Ref) string {
	return ref.OrgID + "/" + ref.ProjectID + "/" + ref.Name
}

func (m *memoryStore) Get(_ context.Context, ref secret.Ref) (string, error) {
	v, ok := m.secrets[m.key(ref)]
	if !ok {
		return "", secret.ErrNotFound
	}
	return v, nil
}

func (m *memoryStore) Set(_ context.Context, ref secret.Ref, value string) error {
	if m.readOnly {
		return secret.ErrReadOnly
	}
	m.secrets[m.key(ref)] = value
	return nil
}

func (m *memoryStore) Delete(_ context.Context, ref secret.Ref) error {
	if m.readOnly {
		return secret.ErrReadOnly
	}
	if _, ok := m.secrets[m.key(ref)]; !ok {
		return secret.ErrNotFound
	}
	delete(m.secrets, m.key(ref))
	return nil
}

func (m *memoryStore) List(_ context.Context, scope secret.Scope) ([]secret.Entry, error) {
	var entries []secret.Entry
	prefix := scope.OrgID + "/" + scope.ProjectID + "/"
	for k := range m.secrets {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			name := k[len(prefix):]
			entries = append(entries, secret.Entry{Name: name, Provider: m.provider})
		}
	}
	return entries, nil
}

func (m *memoryStore) Provider() string { return m.provider }

func TestMultiStore_GetFallsThrough(t *testing.T) {
	ctx := context.Background()

	primary := newMemoryStore("internal", false)
	fallback := newMemoryStore("vault", true)

	fallback.secrets["org1//DB_PASSWORD"] = "vault-secret"

	multi := secret.NewMultiStore(primary, fallback)

	val, err := multi.Get(ctx, secret.Ref{OrgID: "org1", Name: "DB_PASSWORD"})
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if val != "vault-secret" {
		t.Errorf("Get() = %q, want %q", val, "vault-secret")
	}
}

func TestMultiStore_GetPrimaryWins(t *testing.T) {
	ctx := context.Background()

	primary := newMemoryStore("internal", false)
	fallback := newMemoryStore("vault", true)

	primary.secrets["org1//API_KEY"] = "internal-value"
	fallback.secrets["org1//API_KEY"] = "vault-value"

	multi := secret.NewMultiStore(primary, fallback)

	val, err := multi.Get(ctx, secret.Ref{OrgID: "org1", Name: "API_KEY"})
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if val != "internal-value" {
		t.Errorf("Get() = %q, want %q", val, "internal-value")
	}
}

func TestMultiStore_GetNotFound(t *testing.T) {
	ctx := context.Background()

	primary := newMemoryStore("internal", false)
	fallback := newMemoryStore("vault", true)

	multi := secret.NewMultiStore(primary, fallback)

	_, err := multi.Get(ctx, secret.Ref{OrgID: "org1", Name: "MISSING"})
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
	if !errors.Is(err, secret.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestMultiStore_SetGoesToPrimary(t *testing.T) {
	ctx := context.Background()

	primary := newMemoryStore("internal", false)
	fallback := newMemoryStore("vault", true)

	multi := secret.NewMultiStore(primary, fallback)

	ref := secret.Ref{OrgID: "org1", Name: "NEW_SECRET"}
	if err := multi.Set(ctx, ref, "value"); err != nil {
		t.Fatalf("Set() error: %v", err)
	}

	if primary.secrets["org1//NEW_SECRET"] != "value" {
		t.Error("secret not stored in primary")
	}

	if _, ok := fallback.secrets["org1//NEW_SECRET"]; ok {
		t.Error("secret unexpectedly stored in fallback")
	}
}

func TestMultiStore_SetReadOnlyPrimary(t *testing.T) {
	ctx := context.Background()

	readOnlyPrimary := newMemoryStore("vault", true)
	multi := secret.NewMultiStore(readOnlyPrimary)

	err := multi.Set(ctx, secret.Ref{OrgID: "org1", Name: "X"}, "val")
	if err != secret.ErrReadOnly {
		t.Errorf("Set() error = %v, want ErrReadOnly", err)
	}
}

func TestMultiStore_ListDeduplicates(t *testing.T) {
	ctx := context.Background()

	primary := newMemoryStore("internal", false)
	fallback := newMemoryStore("vault", true)

	primary.secrets["org1//SHARED"] = "internal"
	fallback.secrets["org1//SHARED"] = "vault"

	primary.secrets["org1//INTERNAL_ONLY"] = "x"
	fallback.secrets["org1//VAULT_ONLY"] = "y"

	multi := secret.NewMultiStore(primary, fallback)

	entries, err := multi.List(ctx, secret.Scope{OrgID: "org1"})
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("len(List()) = %d, want 3", len(entries))
	}

	for _, e := range entries {
		if e.Name == "SHARED" && e.Provider != "internal" {
			t.Errorf("SHARED provider = %q, want %q", e.Provider, "internal")
		}
	}
}

func TestMultiStore_Provider(t *testing.T) {
	multi := secret.NewMultiStore()
	if got := multi.Provider(); got != "multi" {
		t.Errorf("Provider() = %q, want %q", got, "multi")
	}
}
