package main

import (
	"context"
	"fmt"
	"os"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/dbkit"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/NerdMeNot/flint/internal/platform/config"
	flintserver "github.com/NerdMeNot/flint/internal/platform/server"
	"github.com/NerdMeNot/flint/internal/products/workflows"
	"github.com/NerdMeNot/flint/pkg/forge"
	"github.com/NerdMeNot/flint/pkg/logsink"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	version    = "dev"
	commit     = "unknown"
	configPath string
	mode       string
)

func main() {
	root := &cobra.Command{
		Use:     "flint-server",
		Short:   "Flint CI server — webhook ingestion, API, auth",
		Version: fmt.Sprintf("%s (%s)", version, commit),
		RunE:    run,
	}

	root.Flags().StringVar(&configPath, "config", "", "path to config file")
	root.Flags().StringVar(&mode, "mode", "all", "server mode: all | webhook | api")

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if err := config.Validate(cfg, "server"); err != nil {
		return fmt.Errorf("config validation: %w", err)
	}

	shutdown, err := observe.Init(ctx, observe.Config{
		ServiceName:    "flint-server",
		ServiceVersion: version,
		LogLevel:       "info",
	})
	if err != nil {
		return fmt.Errorf("observability init: %w", err)
	}
	defer shutdown(ctx)

	log.Info().
		Str("version", version).
		Int("port", cfg.Server.PortOrDefault()).
		Msg("flint-server starting")

	// Database.
	pool, err := dbkit.NewPool(ctx, dbkit.Config{
		Host:     cfg.Database.Host,
		Port:     cfg.Database.PortOrDefault(),
		Database: cfg.Database.Database,
		User:     cfg.Database.User,
		Password: cfg.Database.Password,
		SSLMode:  cfg.Database.SSLMode,
		MaxConns: cfg.Database.MaxConns,
		MinConns: cfg.Database.MinConns,
	})
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

	// Auto-create default org.
	q := db.New(pool)
	orgID, err := q.GetOrCreateDefaultOrg(ctx)
	if err != nil {
		return fmt.Errorf("creating default org: %w", err)
	}
	log.Info().Str("orgID", orgID).Msg("default org ready")

	// Casbin RBAC enforcer.
	enforcer, err := auth.NewEnforcer(pool)
	if err != nil {
		return fmt.Errorf("creating casbin enforcer: %w", err)
	}

	// Seed system roles.
	if err := auth.SeedSystemRoles(ctx, q, orgID); err != nil {
		return fmt.Errorf("seeding system roles: %w", err)
	}

	// Seed admin users from config.
	if len(cfg.Auth.AdminUsers) > 0 {
		if err := auth.SeedAdminUsers(ctx, q, orgID, cfg.Auth.AdminUsers); err != nil {
			return fmt.Errorf("seeding admin users: %w", err)
		}
		log.Info().Int("count", len(cfg.Auth.AdminUsers)).Msg("admin users seeded")
	}

	// Auto-bootstrap: create initial admin if no users exist.
	userCount, _ := q.CountUsers(ctx, orgID)
	if userCount == 0 {
		bootstrapEmail := cfg.Bootstrap.EmailOrDefault()
		bootstrapPassword := cfg.Bootstrap.Password
		if bootstrapPassword == "" {
			bootstrapPassword = auth.GenerateRandomPassword()
		}
		hash, hashErr := auth.HashPassword(bootstrapPassword)
		if hashErr == nil {
			userID, createErr := q.CreateLocalUser(ctx, db.CreateLocalUserParams{
				OrgID: orgID, Email: bootstrapEmail, Name: strPtr("Admin"),
				PasswordHash: &hash,
			})
			if createErr == nil {
				// Assign admin role.
				adminRole, roleErr := q.GetRoleBySlug(ctx, db.GetRoleBySlugParams{
					OrgID: orgID, Slug: "admin",
				})
				if roleErr == nil {
					_ = q.InsertRoleAssignment(ctx, db.InsertRoleAssignmentParams{
						Subject: bootstrapEmail, RoleID: adminRole.ID,
					})
				}
				// Mark for forced password change.
				_, _ = pool.Exec(ctx,
					`UPDATE users SET force_password_change = true WHERE id = $1`, userID)

				log.Warn().
					Str("email", bootstrapEmail).
					Msg("⚡ BOOTSTRAP: initial admin created (password change required on first login)")

				// Print the temporary password to stdout ONLY when we generated
				// it — never via the structured logger, which would persist the
				// credential to log aggregation. Shown once.
				if cfg.Bootstrap.Password == "" {
					fmt.Printf("\n⚡ BOOTSTRAP admin %q — temporary password: %s\n   Save it now; it will not be shown again and a change is required on first login.\n\n",
						bootstrapEmail, bootstrapPassword)
				}
			}
		}
	}

	// Regenerate Casbin policies from DB state.
	if err := auth.RegeneratePolicies(ctx, q, pool, enforcer); err != nil {
		return fmt.Errorf("regenerating RBAC policies: %w", err)
	}
	log.Info().Msg("RBAC policies regenerated")

	// OIDC provider (optional).
	var oidcProvider *auth.OIDCProvider
	if auth.OIDCConfigured(cfg.Auth.OIDC.IssuerURL, cfg.Auth.OIDC.ClientID) {
		oidcProvider, err = auth.NewOIDCProvider(ctx, auth.OIDCProviderConfig{
			IssuerURL:    cfg.Auth.OIDC.IssuerURL,
			ClientID:     cfg.Auth.OIDC.ClientID,
			ClientSecret: cfg.Auth.OIDC.ClientSecret,
			RedirectURL:  cfg.Server.BaseURL + "/auth/oidc/callback",
		})
		if err != nil {
			return fmt.Errorf("initializing OIDC provider: %w", err)
		}
		log.Info().Str("issuer", cfg.Auth.OIDC.IssuerURL).Msg("OIDC provider initialized")
	}

	// SAML provider (optional).
	var samlProvider *auth.SAMLProvider
	if cfg.Auth.SAML.Enabled && cfg.Auth.SAML.MetadataURL != "" {
		samlCfg := auth.SAMLProviderConfig{
			MetadataURL: cfg.Auth.SAML.MetadataURL,
			EntityID:    cfg.Auth.SAML.EntityID,
			ACSURL:      cfg.Server.BaseURL + "/auth/saml/acs",
		}
		// Load SP cert/key from files if configured.
		if cfg.Auth.SAML.CertFile != "" && cfg.Auth.SAML.KeyFile != "" {
			certPEM, err := os.ReadFile(cfg.Auth.SAML.CertFile)
			if err != nil {
				return fmt.Errorf("reading SAML cert file: %w", err)
			}
			keyPEM, err := os.ReadFile(cfg.Auth.SAML.KeyFile)
			if err != nil {
				return fmt.Errorf("reading SAML key file: %w", err)
			}
			samlCfg.CertPEM = string(certPEM)
			samlCfg.KeyPEM = string(keyPEM)
		}
		samlProvider, err = auth.NewSAMLProvider(samlCfg)
		if err != nil {
			return fmt.Errorf("initializing SAML provider: %w", err)
		}
		log.Info().Str("metadata", cfg.Auth.SAML.MetadataURL).Msg("SAML provider initialized")
	}

	// Forge provider.
	forgeProvider := forge.NewGitHub("", nil)

	// Engine — replaces Temporal. Embedded, Postgres-backed.
	// The JWT secret signs/verifies task tokens (server-side only, never injected
	// into step pods — unlike the internal token).
	eng := engine.New(pool, forgeProvider, []byte(cfg.Auth.JWT.Secret))
	defer eng.Close()

	deps := flintserver.Deps{
		Config:       cfg,
		DB:           pool,
		Q:            q,
		Engine:       eng,
		Forge:        forgeProvider,
		Logs:         &logsink.FilesystemSink{BaseDir: cfg.Storage.FS.Path},
		LogBroadcast: flintserver.NewLogStream(),
		Mode:         mode,
		Sessions: auth.NewSessionManager(auth.SessionConfig{
			SigningKey: []byte(cfg.Auth.JWT.Secret),
			Issuer:     cfg.Server.BaseURL,
		}),
		OIDCProvider: oidcProvider,
		SAMLProvider: samlProvider,
		Enforcer:     enforcer,
	}

	// Mount product route surfaces here (composition root), so the platform
	// server never imports product packages.
	if cfg.Products.WorkflowsEnabled() {
		deps.APIRoutes = append(deps.APIRoutes, workflows.NewAPI(eng, q).Register)
		log.Info().Msg("product enabled: workflows")
	}

	srv := flintserver.New(deps)
	srv.Run()

	return nil
}

func strPtr(s string) *string { return &s }
