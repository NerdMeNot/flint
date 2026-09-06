// Package auth provides authentication (OIDC, SAML) and authorization (RBAC)
// for Flint. It is a thin layer over go-oidc and crewjam/saml — not a reusable
// auth library. Protocol details live in the underlying libraries; this package
// normalizes their output into unified Claims and enforces Flint's RBAC model.
package auth

import "time"

// Claims represents a unified identity extracted from either OIDC or SAML.
type Claims struct {
	Subject      string // IdP subject / NameID
	Email        string
	Name         string
	Groups       []string
	OrgID        string // resolved after authentication
	Provider     string // "oidc" or "saml"
	ExternalID   string // raw IdP identifier for user matching
	SessionIndex string // SAML AuthnStatement SessionIndex (for SLO)
	AssertionID  string // SAML assertion ID (for one-time-use replay protection)
	IssuedAt     time.Time
	ExpiresAt    time.Time
	Raw          map[string]any // all claims/attributes for custom mapping

	// Principal is the RBAC subject: the identity policies are keyed on. For
	// humans (browser sessions, personal tokens) it is the user's email; for an
	// API key it is "apikey:<id>", matching what addAPIKeyPolicies generates.
	//
	// Enforcement must use this and never Email. An API key has no email, so
	// enforcing on Email checked the empty string against every policy and
	// denied every API-key request — authentication succeeded and authorization
	// could not possibly match. Anything that authenticates a request has to set
	// it; the middleware denies (loudly) when it is empty.
	Principal string

	// SessionID is the sessions.id row backing this token, carried in the JWT's
	// `sid` claim. The request path checks it so revocation (logout,
	// back-channel logout, IdP deprovisioning) takes effect immediately instead
	// of at token expiry. Empty for API keys and personal tokens, which carry
	// their own revocation checks.
	SessionID string
}

// ────────────────────────────────────────────────────────────
// System role slugs
// ────────────────────────────────────────────────────────────

const (
	RoleAdmin           = "admin"
	RoleDeveloper       = "developer"
	RoleViewer          = "viewer"
	RolePlatformManager = "platform-manager"
)

// Legacy role constants — kept as aliases for migration compatibility.
const (
	RoleOrgAdmin      = "org_admin"      // maps to RoleAdmin
	RolePipelineAdmin = "pipeline_admin" // migrated to custom role
)

// ────────────────────────────────────────────────────────────
// Admin objects (platform-wide, no workspace/env scope)
// ────────────────────────────────────────────────────────────

const (
	ObjWorkspace   = "workspace"
	ObjTeam        = "team"
	ObjEnvironment = "environment"
	ObjRunner      = "runner"
	ObjConnection  = "connection"
	ObjAPIKey      = "apikey"
	ObjSecret      = "secret"
	ObjRole        = "role"
	ObjAudit       = "audit"
	ObjTag         = "tag" // the curated tag-key registry
)

// ────────────────────────────────────────────────────────────
// CI objects (scopable to workspaces + environments)
// ────────────────────────────────────────────────────────────

const (
	ObjProject = "project"
	ObjRun     = "run"
	ObjGate    = "gate"
)

// ────────────────────────────────────────────────────────────
// Actions
// ────────────────────────────────────────────────────────────

const (
	// Admin actions.
	ActRead   = "read"
	ActManage = "manage"

	// CI actions.
	ActWrite   = "write"
	ActTrigger = "trigger"
	ActCancel  = "cancel"
	ActApprove = "approve"
	ActReject  = "reject"

	// Wildcard (Admin role only).
	ActWildcard = "*"
	ObjWildcard = "*"
)

// Permission is an object:action pair.
type Permission struct {
	Object string `json:"object"`
	Action string `json:"action"`
}

// Key returns the "object:action" string.
func (p Permission) Key() string {
	return p.Object + ":" + p.Action
}

// AccessRequest describes what is being checked.
type AccessRequest struct {
	Subject     string
	Object      string
	Action      string
	Workspace   string // empty or "*" = any
	Environment string // empty or "*" = any
}

// SessionConfig configures JWT session creation and validation.
type SessionConfig struct {
	// SigningKey is the HMAC-SHA256 key for signing JWTs.
	SigningKey []byte

	// Issuer is the "iss" claim (e.g., "https://flint.example.com").
	Issuer string

	// SessionDuration is how long a session JWT is valid. Default: 24h.
	SessionDuration time.Duration

	// RefreshDuration is how long a refresh token is valid. Default: 7d.
	RefreshDuration time.Duration
}

func (c *SessionConfig) sessionDuration() time.Duration {
	if c.SessionDuration > 0 {
		return c.SessionDuration
	}
	return 24 * time.Hour
}
