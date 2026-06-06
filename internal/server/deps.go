// Package server implements the flint-server HTTP API using Cloudwego Hertz.
package server

import (
	"github.com/NerdMeNot/flint/internal/auth"
	"github.com/NerdMeNot/flint/internal/config"
	"github.com/NerdMeNot/flint/internal/db"
	"github.com/NerdMeNot/flint/internal/engine"
	"github.com/NerdMeNot/flint/pkg/forge"
	"github.com/NerdMeNot/flint/pkg/logsink"
	"github.com/NerdMeNot/flint/pkg/secret"
	"github.com/casbin/casbin/v2"
)

// Deps holds all dependencies for the HTTP server.
type Deps struct {
	Config       *config.Config
	DB           db.Pool
	Q            db.Querier // sqlc type-safe queries (interface for mockability)
	Engine       engine.Engine
	Forge        forge.ForgeProvider
	Secrets      secret.SecretStore
	Logs         logsink.LogSink
	LogBroadcast LogStream     // SSE log streaming; nil disables streaming
	Sessions     auth.Sessions // JWT session management
	Mode         string
	OIDCProvider auth.OIDCAuth    // nil if not configured
	SAMLProvider auth.SAMLAuth    // nil if not configured
	Enforcer     casbin.IEnforcer // Casbin RBAC enforcer (interface for mockability)
}
