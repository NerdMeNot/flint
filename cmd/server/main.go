package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/NerdMeNot/flint/internal/boot"
	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/dbkit"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/internal/core/secretstore"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/NerdMeNot/flint/internal/platform/config"
	flintserver "github.com/NerdMeNot/flint/internal/platform/server"
	"github.com/NerdMeNot/flint/internal/products/ci"
	"github.com/NerdMeNot/flint/internal/products/workflows"
	"github.com/NerdMeNot/flint/pkg/forge"
	"github.com/NerdMeNot/flint/pkg/logsink"
	"github.com/NerdMeNot/flint/pkg/secret"
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
	root.Flags().StringVar(&mode, "mode", "all", "server mode: all (api+webhook+worker, the single-binary install) | webhook | api")

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
	dbCfg := dbkit.Config{
		Host:     cfg.Database.Host,
		Port:     cfg.Database.PortOrDefault(),
		Database: cfg.Database.Database,
		User:     cfg.Database.User,
		Password: cfg.Database.Password,
		SSLMode:  cfg.Database.SSLMode,
		MaxConns: cfg.Database.MaxConns,
		MinConns: cfg.Database.MinConns,
	}

	// Apply pending schema migrations before opening the pool (default on) —
	// a single-binary install must come up from an empty database with no
	// separate migrate step. Operators running migrations out-of-band set
	// database.autoMigrate: false.
	if cfg.Database.AutoMigrateOrDefault() {
		if err := dbkit.RunMigrations(dbCfg.DSN(), dbkit.Migrations, "migrations"); err != nil {
			return fmt.Errorf("running migrations: %w", err)
		}
		log.Info().Msg("database migrations applied")
	}

	pool, err := dbkit.NewPool(ctx, dbCfg)
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
	// Every org has a default workspace so new projects always have a home
	// (projects.workspace_id is NOT NULL).
	if err := q.EnsureDefaultWorkspace(ctx, orgID); err != nil {
		return fmt.Errorf("ensuring default workspace: %w", err)
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

	// SSO providers. Configuration is sourced from the DB first (managed via the
	// API and hot-reloaded at runtime), falling back to the config file. This
	// makes API-configured SSO survive restarts. The auth callback routes are
	// registered unconditionally and guard on these being non-nil at request time.
	masterKey, masterKeyErr := cfg.Encryption.DecodeMasterKey()

	// OIDC provider (optional).
	var oidcProvider *auth.OIDCProvider
	{
		var pc *auth.ProviderConfig
		if masterKeyErr == nil {
			if pc, err = auth.LoadProviderConfig(ctx, q, masterKey, "oidc"); err != nil {
				return fmt.Errorf("loading OIDC provider config: %w", err)
			}
		}
		if pc == nil && auth.OIDCConfigured(cfg.Auth.OIDC.IssuerURL, cfg.Auth.OIDC.ClientID) {
			pc = &auth.ProviderConfig{
				IssuerURL:    cfg.Auth.OIDC.IssuerURL,
				ClientID:     cfg.Auth.OIDC.ClientID,
				ClientSecret: cfg.Auth.OIDC.ClientSecret,
				Scopes:       cfg.Auth.OIDC.Scopes,
				EmailClaim:   cfg.Auth.OIDC.EmailClaim,
				NameClaim:    cfg.Auth.OIDC.NameClaim,
				GroupsClaim:  cfg.Auth.OIDC.GroupsClaim,
			}
		}
		if pc != nil && pc.HasOIDC() {
			// Non-fatal: a transiently-unreachable IdP (discovery requires a live
			// network call) must not crash-loop the server — health, local login,
			// and break-glass admin access still need to come up. SSO activates on
			// the next config hot-reload once the IdP is reachable.
			if oidcProvider, err = auth.BuildOIDCProvider(ctx, *pc, cfg.Server.BaseURL); err != nil {
				log.Error().Err(err).Str("issuer", pc.IssuerURL).
					Msg("OIDC provider init failed — SSO unavailable until reachable; server continues")
				oidcProvider = nil
			} else {
				log.Info().Str("issuer", pc.IssuerURL).Msg("OIDC provider initialized")
			}
		}
	}

	// SAML provider (optional).
	var samlProvider *auth.SAMLProvider
	{
		var pc *auth.ProviderConfig
		if masterKeyErr == nil {
			if pc, err = auth.LoadProviderConfig(ctx, q, masterKey, "saml"); err != nil {
				return fmt.Errorf("loading SAML provider config: %w", err)
			}
		}
		if pc == nil && cfg.Auth.SAML.Enabled && cfg.Auth.SAML.MetadataURL != "" {
			fileCfg := &auth.ProviderConfig{
				MetadataURL:  cfg.Auth.SAML.MetadataURL,
				EntityID:     cfg.Auth.SAML.EntityID,
				NameIDFormat: cfg.Auth.SAML.NameIDFormat,
				EmailAttrs:   cfg.Auth.SAML.EmailAttrs,
				NameAttrs:    cfg.Auth.SAML.NameAttrs,
				GroupsAttrs:  cfg.Auth.SAML.GroupsAttrs,
			}
			// Load SP cert/key from files if configured.
			if cfg.Auth.SAML.CertFile != "" && cfg.Auth.SAML.KeyFile != "" {
				certPEM, rerr := os.ReadFile(cfg.Auth.SAML.CertFile)
				if rerr != nil {
					return fmt.Errorf("reading SAML cert file: %w", rerr)
				}
				keyPEM, rerr := os.ReadFile(cfg.Auth.SAML.KeyFile)
				if rerr != nil {
					return fmt.Errorf("reading SAML key file: %w", rerr)
				}
				fileCfg.SPCertPEM = string(certPEM)
				fileCfg.SPKeyPEM = string(keyPEM)
			}
			pc = fileCfg
		}
		if pc != nil && pc.HasSAML() {
			// Non-fatal, as with OIDC: metadata-URL configs require a live fetch,
			// so a transiently-unreachable IdP must not crash-loop the server.
			if samlProvider, err = auth.BuildSAMLProvider(*pc, cfg.Server.BaseURL); err != nil {
				log.Error().Err(err).
					Msg("SAML provider init failed — SSO unavailable until reachable; server continues")
				samlProvider = nil
			} else {
				log.Info().Msg("SAML provider initialized")
			}
		}
	}

	// Forge provider. In demo mode, a stub forge serves a canned pipeline so the
	// UI's trigger/retry actions work without a real GitHub connection (the
	// counterpart to the simulated step executor).
	var forgeProvider forge.ForgeProvider = forge.NewGitHub("", nil)
	if os.Getenv("FLINT_DEMO") != "" {
		forgeProvider = forge.NewStubForge()
		log.Info().Msg("forge: stub (demo mode — canned pipelines)")
	}

	// Engine — replaces Temporal. Embedded, Postgres-backed.
	// The JWT secret signs/verifies task tokens (server-side only, never injected
	// into step pods — unlike the internal token).
	eng := engine.New(pool, []byte(cfg.Auth.JWT.Secret))
	defer eng.Close()

	// Flint-managed secret store: env-variables flagged secret are encrypted at
	// rest with the server master key and decrypted server-side for the agent
	// injection path. Wired only when a 32-byte hex master key is configured;
	// otherwise secret injection is unavailable (the agent endpoint reports it).
	var secretStore secret.SecretStore
	if mk, err := hex.DecodeString(cfg.Encryption.MasterKey); err == nil && len(mk) == 32 {
		store, err := secretstore.NewEnvVarStore(db.New(pool), mk)
		if err != nil {
			return fmt.Errorf("initializing secret store: %w", err)
		}
		secretStore = store
		log.Info().Msg("secret store: env-var backend enabled")
	} else {
		log.Warn().Msg("secret store disabled: encryption.masterKey not configured (32-byte hex)")
	}

	// Log sink — the server-side durable store for step logs. Agents ship log
	// batches to POST /internal/logs; this sink is where they land. S3 mode is
	// the production choice (survives server restarts, horizontally scalable);
	// filesystem mode suits single-node and dev installs.
	var logs logsink.LogSink
	if cfg.Storage.Mode == "s3" {
		s3sink, err := logsink.NewS3Sink(ctx, cfg.Storage.S3.Bucket, cfg.Storage.S3.Region, cfg.Storage.S3.Endpoint)
		if err != nil {
			return fmt.Errorf("initializing S3 log sink: %w", err)
		}
		logs = s3sink
		log.Info().Str("bucket", cfg.Storage.S3.Bucket).Msg("log sink: s3")
	} else {
		logs = &logsink.FilesystemSink{BaseDir: cfg.Storage.FS.Path}
		log.Info().Str("path", cfg.Storage.FS.Path).Msg("log sink: filesystem")
	}

	deps := flintserver.Deps{
		Config:         cfg,
		DB:             pool,
		Q:              q,
		Engine:         eng,
		Forge:          forgeProvider,
		Secrets:        secretStore,
		Logs:           logs,
		LogBroadcast:   flintserver.NewLogStream(),
		StateBroadcast: flintserver.NewStateStream(),
		Mode:           mode,
		Demo:           os.Getenv("FLINT_DEMO") != "",
		Sessions: auth.NewSessionManager(auth.SessionConfig{
			SigningKey: []byte(cfg.Auth.JWT.Secret),
			Issuer:     cfg.Server.BaseURL,
		}),
		Enforcer: enforcer,
	}
	// Assign SSO providers only when actually constructed. Boxing a nil
	// *auth.OIDCProvider/*auth.SAMLProvider into the interface field would make
	// it a non-nil typed-nil interface, defeating the `!= nil` guards in the
	// auth handlers (and panicking when a method is then called on it).
	if oidcProvider != nil {
		deps.OIDCProvider = oidcProvider
	}
	if samlProvider != nil {
		deps.SAMLProvider = samlProvider
	}

	// Mount product surfaces here (composition root), so the platform server
	// never imports product packages.
	var onRunTerminal func(ctx context.Context, runID, status string)
	if cfg.Products.CIEnabled() {
		ciSvc := ci.NewService(eng, forgeProvider, q)
		deps.Runs = ciSvc
		// Completion reporting: post success/failure to the forge when a run
		// reaches a terminal state (the counterpart of the "queued" status).
		onRunTerminal = ciSvc.ReportRunFinished
		log.Info().Msg("product enabled: ci")
	}

	// Push run-state changes to SSE subscribers on each committed transition
	// (step completion / cancellation happen in this process via the agent
	// callback and cancel handler; the SSE handler polls for the rest), and
	// report terminal runs to the forge.
	flintserver.WireStateObserver(eng, q, deps.StateBroadcast, onRunTerminal)
	if cfg.Products.WorkflowsEnabled() {
		deps.APIRoutes = append(deps.APIRoutes, workflows.NewAPI(eng, q).Register)
		log.Info().Msg("product enabled: workflows")
	}

	// Single-binary mode: --mode all runs the worker loop (dispatch, timers,
	// informer, cron) in-process — the whole control plane is Postgres + this
	// one process. Deployments that want to scale dispatch independently run
	// flint-server --mode api|webhook plus dedicated flint-worker replicas.
	if mode == "all" && os.Getenv("FLINT_DISABLE_EMBEDDED_WORKER") == "" {
		w, werr := boot.StartWorker(ctx, cfg, pool, eng)
		if werr != nil {
			return fmt.Errorf("starting embedded worker: %w", werr)
		}
		go func() {
			if err := w.Loop.Run(ctx); err != nil {
				log.Error().Err(err).Msg("embedded worker loop stopped with error")
			}
		}()
		log.Info().Msg("embedded worker: engine loop running in-process")
	}

	srv := flintserver.New(deps)
	srv.StartAuthStoreCleanup(ctx) // prune expired device codes + MFA tokens
	srv.Run()

	return nil
}

func strPtr(s string) *string { return &s }
