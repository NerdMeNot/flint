// Package config provides unified configuration for all Flint components.
// Configuration is loaded from a YAML file with environment variable overrides
// using the FLINT_ prefix (e.g., FLINT_SERVER_PORT=9090).
package config

import (
	"encoding/hex"
	"errors"
	"time"
)

// Config is the top-level configuration composing all subsystem configs.
type Config struct {
	Server     ServerConfig     `mapstructure:"server"`
	Worker     WorkerConfig     `mapstructure:"worker"`
	Agent      AgentConfig      `mapstructure:"agent"`
	Controller ControllerConfig `mapstructure:"controller"`
	Database   DatabaseConfig   `mapstructure:"database"`
	Auth       AuthConfig       `mapstructure:"auth"`
	Bootstrap  BootstrapConfig  `mapstructure:"bootstrap"`
	Sync       SyncConfig       `mapstructure:"sync"`
	Storage    StorageConfig    `mapstructure:"storage"`
	Forge      ForgeConfig      `mapstructure:"forge"`
	Encryption EncryptionConfig `mapstructure:"encryption"`
	Products   ProductsConfig   `mapstructure:"products"`
	// Provisioning holds the cluster-level inputs a Karpenter EC2NodeClass needs
	// (account-level infra Flint can't invent). Required only for managed runner
	// pools (the autoscaling add-on); set once by the platform team.
	Provisioning ProvisioningConfig `mapstructure:"provisioning"`
}

// ProvisioningConfig is the account-level node-provisioning profile for managed
// runner pools. Maps to runner.ProvisioningProfile (core doesn't import config).
//
// Selectors are "key=value" strings (not maps) because Karpenter discovery tags
// like "karpenter.sh/discovery" contain dots, which Viper would otherwise split
// into nested map keys. SelectorMap parses them.
type ProvisioningConfig struct {
	Role                  string   `mapstructure:"role"`                  // node instance IAM role / instance profile
	SubnetSelector        []string `mapstructure:"subnetSelector"`        // EC2NodeClass subnet tag selector ("key=value")
	SecurityGroupSelector []string `mapstructure:"securityGroupSelector"` // EC2NodeClass SG tag selector ("key=value")
	AMIFamily             string   `mapstructure:"amiFamily"`             // default AMI family (e.g. AL2023)
}

// SubnetTags / SecurityGroupTags parse the "key=value" selector strings into maps.
func (p ProvisioningConfig) SubnetTags() map[string]string { return parseKV(p.SubnetSelector) }
func (p ProvisioningConfig) SecurityGroupTags() map[string]string {
	return parseKV(p.SecurityGroupSelector)
}

// Configured reports whether enough is set to render a managed pool's Karpenter
// NodeClass. The UI gates the managed-pool option on this — no profile means
// managed pools can't be rendered, so the option is disabled.
func (p ProvisioningConfig) Configured() bool {
	return p.Role != "" && len(p.SubnetTags()) > 0 && len(p.SecurityGroupTags()) > 0
}

func parseKV(pairs []string) map[string]string {
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		if i := indexByte(p, '='); i > 0 {
			out[p[:i]] = p[i+1:]
		}
	}
	return out
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// ProductsConfig toggles the family's products for a deployment. The unified UI
// reads these (via /api/v1/capabilities) to decide which top-level sections to
// render; the server gates each product's routes the same way.
type ProductsConfig struct {
	CI        ProductToggle `mapstructure:"ci"`
	Workflows ProductToggle `mapstructure:"workflows"`
	LoadTest  ProductToggle `mapstructure:"loadtest"`
}

// ProductToggle enables a product surface (routes, nav). Enabled is a pointer so
// an unset value can default differently per product (CI and Workflows default
// on; an explicit `enabled: false` turns one off).
type ProductToggle struct {
	Enabled *bool `mapstructure:"enabled"`
}

func (t ProductToggle) enabledOr(def bool) bool {
	if t.Enabled != nil {
		return *t.Enabled
	}
	return def
}

// CIEnabled and WorkflowsEnabled default to true — both products ship today.
func (p ProductsConfig) CIEnabled() bool        { return p.CI.enabledOr(true) }
func (p ProductsConfig) WorkflowsEnabled() bool { return p.Workflows.enabledOr(true) }

// LoadTestEnabled is always false for now: Load Testing has no backend yet, so
// the UI shows it as "coming soon" rather than a working section.
func (p ProductsConfig) LoadTestEnabled() bool { return false }

// SyncConfig configures the IdP sync daemon.
type SyncConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Interval string `mapstructure:"interval"` // Go duration, default "15m"
}

// BootstrapConfig configures the initial admin user created on first run.
type BootstrapConfig struct {
	Email    string `mapstructure:"email"`    // default: admin@flint.local
	Password string `mapstructure:"password"` // auto-generated if empty
}

func (b *BootstrapConfig) EmailOrDefault() string {
	if b.Email != "" {
		return b.Email
	}
	return "admin@flint.local"
}

// ServerConfig configures the flint-server HTTP API.
type ServerConfig struct {
	Port          int    `mapstructure:"port"`
	BaseURL       string `mapstructure:"baseUrl"`
	InternalToken string `mapstructure:"internalToken"` // shared secret for /internal agent endpoints
}

func (c *ServerConfig) PortOrDefault() int {
	if c.Port > 0 {
		return c.Port
	}
	return 8080
}

// WorkerConfig configures the flint-worker.
type WorkerConfig struct {
	Replicas          int           `mapstructure:"replicas"`
	JobNamespace      string        `mapstructure:"jobNamespace"`
	AgentImage        string        `mapstructure:"agentImage"`
	DefaultRunnerPool string        `mapstructure:"defaultRunnerPool"`
	SweepInterval     time.Duration `mapstructure:"sweepInterval"`
}

// SweepIntervalOrDefault returns the observer sweep interval.
// Default: 5m. Minimum: 30s.
func (c *WorkerConfig) SweepIntervalOrDefault() time.Duration {
	if c.SweepInterval >= 30*time.Second {
		return c.SweepInterval
	}
	if c.SweepInterval > 0 {
		return 30 * time.Second // enforce minimum
	}
	return 5 * time.Minute
}

func (c *WorkerConfig) JobNamespaceOrDefault() string {
	if c.JobNamespace != "" {
		return c.JobNamespace
	}
	return "flint-jobs"
}

func (c *WorkerConfig) DefaultRunnerPoolOrDefault() string {
	if c.DefaultRunnerPool != "" {
		return c.DefaultRunnerPool
	}
	return "standard"
}

// AgentConfig configures the flint-agent.
type AgentConfig struct {
	Image string `mapstructure:"image"`
}

// ControllerConfig configures the flint-controller.
type ControllerConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// DatabaseConfig configures PostgreSQL.
type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Database string `mapstructure:"database"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	SSLMode  string `mapstructure:"sslMode"`
	MaxConns int32  `mapstructure:"maxConns"`
	MinConns int32  `mapstructure:"minConns"`
}

func (c *DatabaseConfig) PortOrDefault() int {
	if c.Port > 0 {
		return c.Port
	}
	return 5432
}

// Note: TemporalConfig removed — Flint uses its own embedded engine (internal/engine/).

// TLSConfig for mTLS connections (Temporal Cloud, etc.).
type TLSConfig struct {
	CertPath string `mapstructure:"certPath"`
	KeyPath  string `mapstructure:"keyPath"`
}

// AuthConfig holds authentication provider config — boot-time only.
// Casbin policies (role definitions + assignments) are stored in the
// casbin_rules table and managed via API — see /api/v1/roles.
type AuthConfig struct {
	OIDC        OIDCConfig `mapstructure:"oidc"`
	SAML        SAMLConfig `mapstructure:"saml"`
	JWT         JWTConfig  `mapstructure:"jwt"`
	AdminUsers  []string   `mapstructure:"adminUsers"`  // emails that always get org_admin
	DefaultRole string     `mapstructure:"defaultRole"` // fallback role, default "viewer"
}

// DefaultRoleOrFallback returns the configured default role or "viewer".
func (c *AuthConfig) DefaultRoleOrFallback() string {
	if c.DefaultRole != "" {
		return c.DefaultRole
	}
	return "viewer"
}

type OIDCConfig struct {
	IssuerURL    string   `mapstructure:"issuerUrl"`
	ClientID     string   `mapstructure:"clientId"`
	ClientSecret string   `mapstructure:"clientSecret"`
	Scopes       []string `mapstructure:"scopes"`      // default [openid profile email groups]
	EmailClaim   string   `mapstructure:"emailClaim"`  // default "email"
	NameClaim    string   `mapstructure:"nameClaim"`   // default "name"
	GroupsClaim  string   `mapstructure:"groupsClaim"` // default "groups"
}

type SAMLConfig struct {
	Enabled      bool     `mapstructure:"enabled"`
	MetadataURL  string   `mapstructure:"metadataUrl"`
	EntityID     string   `mapstructure:"entityId"`
	CertFile     string   `mapstructure:"certFile"`
	KeyFile      string   `mapstructure:"keyFile"`
	NameIDFormat string   `mapstructure:"nameIdFormat"`     // default emailAddress
	EmailAttrs   []string `mapstructure:"emailAttributes"`  // attribute names for email
	NameAttrs    []string `mapstructure:"nameAttributes"`   // attribute names for display name
	GroupsAttrs  []string `mapstructure:"groupsAttributes"` // attribute names for groups
}

type JWTConfig struct {
	Secret          string        `mapstructure:"secret"`
	SessionDuration time.Duration `mapstructure:"sessionDuration"`
}

// StorageConfig configures object storage for logs, artifacts, and cache.
type StorageConfig struct {
	Mode string   `mapstructure:"mode"` // "s3" or "filesystem"
	S3   S3Config `mapstructure:"s3"`
	FS   FSConfig `mapstructure:"filesystem"`
}

type S3Config struct {
	Bucket   string `mapstructure:"bucket"`
	Region   string `mapstructure:"region"`
	Endpoint string `mapstructure:"endpoint"`
}

type FSConfig struct {
	Path string `mapstructure:"path"`
}

// ForgeConfig is intentionally empty in boot config.
// All forge credentials (GitHub App, GitLab OAuth, Bitbucket OAuth) are per-org
// application data stored in the forge_connections table, managed via UI/API,
// and encrypted at rest with the master key.
type ForgeConfig struct{}

// EncryptionConfig configures secret envelope encryption.
type EncryptionConfig struct {
	MasterKey        string `mapstructure:"masterKey"` // hex-encoded 32-byte key
	MasterKeyVersion int    `mapstructure:"masterKeyVersion"`
}

// DecodeMasterKey decodes the hex-encoded 32-byte master key used for envelope
// encryption of credential blobs at rest.
func (c EncryptionConfig) DecodeMasterKey() ([]byte, error) {
	key, err := hex.DecodeString(c.MasterKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("server encryption master key is not configured (need hex-encoded 32 bytes)")
	}
	return key, nil
}
