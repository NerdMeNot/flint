// Package server implements the flint-server HTTP API using Cloudwego Hertz.
package server

import (
	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/NerdMeNot/flint/internal/platform/config"
	"github.com/NerdMeNot/flint/pkg/forge"
	"github.com/NerdMeNot/flint/pkg/logsink"
	"github.com/NerdMeNot/flint/pkg/secret"
	"github.com/casbin/casbin/v2"
	"github.com/cloudwego/hertz/pkg/route"
)

// APIRouteRegistrar mounts additional routes under the authenticated /api/v1
// group. Product packages provide registrars and the composition root wires them
// via Deps.APIRoutes, so the platform server never imports product packages.
type APIRouteRegistrar func(rg *route.RouterGroup)

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

	// APIRoutes are product route registrars mounted under the authenticated
	// /api/v1 group (e.g. Flint Workflows). Wired by the composition root.
	APIRoutes []APIRouteRegistrar
}
