// Package auth provides authentication (OIDC, SAML) and authorization (RBAC)
// for Flint. It is a thin layer over go-oidc and crewjam/saml — not a reusable
// auth library. Protocol details live in the underlying libraries; this package
// normalizes their output into unified Claims and enforces Flint's RBAC model.
package auth

import "time"

// Claims represents a unified identity extracted from either OIDC or SAML.
type Claims struct {
	Subject    string // IdP subject / NameID
	Email      string
	Name       string
	Groups     []string
	OrgID      string // resolved after authentication
	Provider   string // "oidc" or "saml"
	ExternalID string // raw IdP identifier for user matching
	IssuedAt   time.Time
	ExpiresAt  time.Time
	Raw        map[string]any // all claims/attributes for custom mapping
}

// Role represents a Flint RBAC role.
type Role string

const (
	RoleOrgAdmin      Role = "org_admin"
	RolePipelineAdmin Role = "pipeline_admin"
	RoleDeveloper     Role = "developer"
	RoleViewer        Role = "viewer"
)

// Action represents a permission-controlled operation.
type Action string

const (
	// Org management.
	ActionOrgManage Action = "org.manage"
	ActionOrgRead   Action = "org.read"

	// Project management.
	ActionProjectCreate  Action = "project.create"
	ActionProjectUpdate  Action = "project.update"
	ActionProjectArchive Action = "project.archive"
	ActionProjectRead    Action = "project.read"

	// Pipeline execution.
	ActionPipelineRun    Action = "pipeline.run"
	ActionPipelineCancel Action = "pipeline.cancel"
	ActionPipelineRead   Action = "pipeline.read"

	// Gate approval.
	ActionGateApprove Action = "gate.approve"

	// Secrets.
	ActionSecretCreate Action = "secret.create"
	ActionSecretRead   Action = "secret.read"
	ActionSecretDelete Action = "secret.delete"

	// Runner pools.
	ActionRunnerManage Action = "runner.manage"
	ActionRunnerRead   Action = "runner.read"

	// RBAC.
	ActionRBACManage Action = "rbac.manage"

	// Audit.
	ActionAuditRead Action = "audit.read"
)

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

func (c *SessionConfig) refreshDuration() time.Duration {
	if c.RefreshDuration > 0 {
		return c.RefreshDuration
	}
	return 7 * 24 * time.Hour
}
