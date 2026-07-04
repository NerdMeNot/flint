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
	Engine     EngineConfig     `mapstructure:"engine"`
	Database   DatabaseConfig   `mapstructure:"database"`
	Auth       AuthConfig       `mapstructure:"auth"`
	Bootstrap  BootstrapConfig  `mapstructure:"bootstrap"`
	Sync       SyncConfig       `mapstructure:"sync"`
	Storage    StorageConfig    `mapstructure:"storage"`
	Costs      CostConfig       `mapstructure:"costs"`
	Forge      ForgeConfig      `mapstructure:"forge"`
	Encryption EncryptionConfig `mapstructure:"encryption"`
	Products   ProductsConfig   `mapstructure:"products"`
	// Providers optionally bootstraps compute providers from the config file:
	// each entry is upserted by name into the DB at startup, so IaC/first-boot
	// installs stay one-file. The DB (managed via the API) is the source of
	// truth; credentials never live in this file — use the provider's ambient
	// chain (instance role / env) or set them via the API.
	Providers []ProviderBootstrap `mapstructure:"providers"`
}

// ProviderBootstrap is one config-file compute provider entry.
type ProviderBootstrap struct {
	Name   string         `mapstructure:"name"`
	Type   string         `mapstructure:"type"` // static | aws
	Config map[string]any `mapstructure:",remain"`
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

// ServerConfig configures the flint server HTTP API + agent gRPC endpoint.
type ServerConfig struct {
	Port int `mapstructure:"port"`
	// GRPCPort is the AgentService listener (flint-agent control channel).
	GRPCPort int    `mapstructure:"grpcPort"`
	BaseURL  string `mapstructure:"baseUrl"`
	// WebDist serves a built web UI (static assets + SPA index fallback)
	// straight from this process — no separate web deployment or reverse
	// proxy. Empty disables static serving.
	WebDist string `mapstructure:"webDist"`
}

func (c *ServerConfig) PortOrDefault() int {
	if c.Port > 0 {
		return c.Port
	}
	return 8080
}

func (c *ServerConfig) GRPCPortOrDefault() int {
	if c.GRPCPort > 0 {
		return c.GRPCPort
	}
	return 9443
}

// EngineConfig configures the dispatch loop (embedded in --mode all, or the
// dedicated --mode dispatch scale-out process).
type EngineConfig struct {
	// DefaultPool is the machine pool jobs use when they set no runner:.
	DefaultPool   string        `mapstructure:"defaultPool"`
	SweepInterval time.Duration `mapstructure:"sweepInterval"`
	// RunRetentionDays bounds how long finished runs are kept (0 = default 90,
	// negative = keep forever).
	RunRetentionDays int `mapstructure:"runRetentionDays"`
}

// SweepIntervalOrDefault returns the observer sweep interval.
// Default: 5m. Minimum: 30s.
func (c *EngineConfig) SweepIntervalOrDefault() time.Duration {
	if c.SweepInterval >= 30*time.Second {
		return c.SweepInterval
	}
	if c.SweepInterval > 0 {
		return 30 * time.Second // enforce minimum
	}
	return 5 * time.Minute
}

func (c *EngineConfig) DefaultPoolOrDefault() string {
	if c.DefaultPool != "" {
		return c.DefaultPool
	}
	return "standard"
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
	// AutoMigrate applies pending schema migrations on server startup
	// (default true — single-binary installs shouldn't need a separate
	// migration job). Set false when migrations are operated externally.
	AutoMigrate *bool `mapstructure:"autoMigrate"`
}

// AutoMigrateOrDefault reports whether startup migrations are enabled (default true).
func (c *DatabaseConfig) AutoMigrateOrDefault() bool {
	return c.AutoMigrate == nil || *c.AutoMigrate
}

func (c *DatabaseConfig) PortOrDefault() int {
	if c.Port > 0 {
		return c.Port
	}
	return 5432
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

// CostConfig sets the compute rates behind cost-per-run estimates. Defaults
// approximate on-demand general-purpose cloud pricing; set your negotiated
// rates for accurate numbers.
type CostConfig struct {
	CPUCoreHour float64 `mapstructure:"cpuCoreHour"` // USD per vCPU-hour
	MemoryGBHr  float64 `mapstructure:"memoryGbHour"`
}

func (c *CostConfig) CPUCoreHourOrDefault() float64 {
	if c.CPUCoreHour > 0 {
		return c.CPUCoreHour
	}
	return 0.0416 // ~m5 on-demand per-vCPU share
}

func (c *CostConfig) MemoryGBHourOrDefault() float64 {
	if c.MemoryGBHr > 0 {
		return c.MemoryGBHr
	}
	return 0.0046 // ~m5 on-demand per-GB share
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
