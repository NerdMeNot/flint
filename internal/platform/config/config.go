// Package config provides unified configuration for all Flint components.
// Configuration is loaded from a YAML file with environment variable overrides
// using the FLINT_ prefix (e.g., FLINT_SERVER_PORT=9090).
package config

import (
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
}

// ProductsConfig toggles the family's products. CI is always on; other products
// are opt-in so a deployment only exposes what it runs.
type ProductsConfig struct {
	Workflows ProductToggle `mapstructure:"workflows"`
}

// ProductToggle enables a product surface (routes, etc.).
type ProductToggle struct {
	Enabled bool `mapstructure:"enabled"`
}

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

	// Executor selects how steps run: "k8s" (default, Kubernetes Jobs), "local"
	// (subprocess — cluster-free dev/test), or "docker" (local containers via
	// docker/podman). local/docker need no cluster and report completion
	// in-process.
	Executor string `mapstructure:"executor"`
	// WorkspaceRoot is the host base dir for per-run workspaces used by the
	// local/docker executors. Empty => the OS temp dir.
	WorkspaceRoot string `mapstructure:"workspaceRoot"`
}

// ExecutorOrDefault returns the configured executor kind, defaulting to "k8s".
func (c *WorkerConfig) ExecutorOrDefault() string {
	if c.Executor != "" {
		return c.Executor
	}
	return "k8s"
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
	IssuerURL    string `mapstructure:"issuerUrl"`
	ClientID     string `mapstructure:"clientId"`
	ClientSecret string `mapstructure:"clientSecret"`
}

type SAMLConfig struct {
	Enabled     bool   `mapstructure:"enabled"`
	MetadataURL string `mapstructure:"metadataUrl"`
	EntityID    string `mapstructure:"entityId"`
	CertFile    string `mapstructure:"certFile"`
	KeyFile     string `mapstructure:"keyFile"`
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
