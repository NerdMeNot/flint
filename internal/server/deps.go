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
	"github.com/jackc/pgx/v5/pgxpool"
)

// Deps holds all dependencies for the HTTP server.
type Deps struct {
	Config       *config.Config
	DB           *pgxpool.Pool
	Q            *db.Queries // sqlc type-safe queries
	Engine       engine.Engine
	Forge        forge.ForgeProvider
	Secrets      secret.SecretStore
	Logs         logsink.LogSink
	Sessions     *auth.SessionManager
	Mode         string
	OIDCProvider *auth.OIDCProvider // nil if not configured
	SAMLProvider *auth.SAMLProvider // nil if not configured
	Enforcer     *casbin.Enforcer   // Casbin RBAC enforcer
}
