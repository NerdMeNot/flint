// Package testutil provides test helpers, mock constructors, and shared
// test infrastructure for Flint's unit and integration tests.
package testutil

import (
	"testing"

	"github.com/NerdMeNot/flint/internal/config"
	"github.com/NerdMeNot/flint/internal/testutil/mocks"
)

// Mocks holds all mock instances for a test. Construct with NewMocks().
type Mocks struct {
	Querier    *mocks.Querier
	Pool       *mocks.Pool
	Engine     *mocks.Engine
	Forge      *mocks.ForgeProvider
	Sessions   *mocks.Sessions
	OIDC       *mocks.OIDCAuth
	SAML       *mocks.SAMLAuth
	Logs       *mocks.LogSink
	Secrets    *mocks.SecretStore
	LogStream  *mocks.LogStream
}

// NewMocks creates a fresh set of mocks for a test.
func NewMocks(t *testing.T) *Mocks {
	t.Helper()
	return &Mocks{
		Querier:   mocks.NewQuerier(t),
		Pool:      mocks.NewPool(t),
		Engine:    mocks.NewEngine(t),
		Forge:     mocks.NewForgeProvider(t),
		Sessions:  mocks.NewSessions(t),
		OIDC:      mocks.NewOIDCAuth(t),
		SAML:      mocks.NewSAMLAuth(t),
		Logs:      mocks.NewLogSink(t),
		Secrets:   mocks.NewSecretStore(t),
		LogStream: mocks.NewLogStream(t),
	}
}

// TestConfig returns a minimal config suitable for unit tests.
func TestConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Port:          8080,
			BaseURL:       "http://localhost:8080",
			InternalToken: "test-internal-token",
		},
		Auth: config.AuthConfig{
			JWT: config.JWTConfig{
				Secret: "test-secret-key-at-least-32-bytes!",
			},
			DefaultRole: "viewer",
		},
	}
}
