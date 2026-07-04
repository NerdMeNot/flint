package boot

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/rs/zerolog/log"

	// Register the built-in compute provider types.
	_ "github.com/NerdMeNot/flint/pkg/compute/awsec2"
	_ "github.com/NerdMeNot/flint/pkg/compute/staticpool"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/dbkit"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/core/fleet"
	"github.com/NerdMeNot/flint/internal/core/secretstore"
	"github.com/NerdMeNot/flint/internal/platform/agentgrpc"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/NerdMeNot/flint/internal/platform/config"
	flintserver "github.com/NerdMeNot/flint/internal/platform/server"
	"github.com/NerdMeNot/flint/internal/products/ci"
	"github.com/NerdMeNot/flint/internal/products/workflows"
	"github.com/NerdMeNot/flint/pkg/forge"
	"github.com/NerdMeNot/flint/pkg/logsink"
	"github.com/NerdMeNot/flint/pkg/secret"
)

// Run starts the requested control-plane mode and blocks until it exits:
//
//	all      — API + webhooks + embedded dispatch loop + IdP sync (the
//	           single-binary install: Postgres + one process)
//	api      — HTTP API only (scale-out; pair with dispatch replicas)
//	webhook  — webhook ingestion only
//	dispatch — the engine loop only (replaces the old flint-worker binary)
//	sync     — the IdP session/group sync loop only (replaces flint-syncd)
func Run(ctx context.Context, cfg *config.Config, mode string) error {
	switch mode {
	case "all", "api", "webhook":
		return RunServer(ctx, cfg, mode)
	case "dispatch":
		return RunDispatch(ctx, cfg)
	case "sync":
		return RunIdPSync(ctx, cfg)
	default:
		return fmt.Errorf("unknown mode %q (want all, api, webhook, dispatch, or sync)", mode)
	}
}

// RunServer wires and runs the HTTP control plane (api/webhook/all modes).
// Moved from cmd/server — cmd/flint's server subcommand is a thin shim over
// this composition root.
func RunServer(ctx context.Context, cfg *config.Config, mode string) error {
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
	if err := bootstrapAdmin(ctx, cfg, q, pool, orgID); err != nil {
		return fmt.Errorf("bootstrapping admin: %w", err)
	}

	// Regenerate Casbin policies from DB state.
	if err := auth.RegeneratePolicies(ctx, q, pool, enforcer); err != nil {
		return fmt.Errorf("regenerating RBAC policies: %w", err)
	}
	log.Info().Msg("RBAC policies regenerated")

	// Compute providers: seed the built-in static provider (so zero-config
	// installs can create pools immediately), then upsert any config-file
	// providers: entries by name. The DB (managed via the API) is the source
	// of truth; the file is a bootstrap convenience for IaC installs.
	if err := seedComputeProviders(ctx, cfg, q); err != nil {
		return fmt.Errorf("seeding compute providers: %w", err)
	}

	// SSO providers. Configuration is sourced from the DB first (managed via the
	// API and hot-reloaded at runtime), falling back to the config file. This
	// makes API-configured SSO survive restarts. The auth callback routes are
	// registered unconditionally and guard on these being non-nil at request time.
	masterKey, masterKeyErr := cfg.Encryption.DecodeMasterKey()

	oidcProvider, err := buildOIDCProvider(ctx, cfg, q, masterKey, masterKeyErr == nil)
	if err != nil {
		return err
	}
	samlProvider, err := buildSAMLProvider(ctx, cfg, q, masterKey, masterKeyErr == nil)
	if err != nil {
		return err
	}

	// Forge provider. In demo mode, a stub forge serves a canned pipeline so the
	// UI's trigger/retry actions work without a real GitHub connection (the
	// counterpart to the simulated step executor).
	var forgeProvider forge.ForgeProvider = forge.NewGitHub("", nil)
	if os.Getenv("FLINT_DEMO") != "" {
		forgeProvider = forge.NewStubForge()
		log.Info().Msg("forge: stub (demo mode — canned pipelines)")
	}

	// Engine — embedded, Postgres-backed. The JWT secret signs/verifies task
	// tokens (server-side only, never handed to step containers — unlike the
	// internal token).
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
	// batches to the server; this sink is where they land. S3 mode is the
	// production choice (survives server restarts, horizontally scalable);
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

	// Single-binary mode: --mode all runs the dispatch loop (executors, timers,
	// cron) in-process — the whole control plane is Postgres + this one process.
	// Deployments that scale dispatch independently run `flint server --mode
	// api|webhook` plus `flint server --mode dispatch` replicas.
	if mode == "all" && os.Getenv("FLINT_NO_EMBEDDED_DISPATCH") == "" {
		w, werr := StartWorker(ctx, cfg, pool, eng)
		if werr != nil {
			return fmt.Errorf("starting embedded dispatch: %w", werr)
		}
		go func() {
			if err := w.Loop.Run(ctx); err != nil {
				log.Error().Err(err).Msg("embedded dispatch loop stopped with error")
			}
		}()
		log.Info().Msg("embedded dispatch: engine loop running in-process")
	}

	// AgentService gRPC — the flint-agent control channel (registration, work
	// claiming, heartbeats, log streaming, completion). Serves in all/api
	// modes alongside HTTP.
	if mode == "all" || mode == "api" {
		fl := fleet.New(pool, eng)
		agentSrv := agentgrpc.New(fl, eng, logs, secretStore, agentgrpc.Config{
			Port:          cfg.Server.GRPCPortOrDefault(),
			ServerHTTPURL: cfg.Server.BaseURL,
		})
		go func() {
			if err := agentSrv.ListenAndServe(ctx); err != nil {
				log.Error().Err(err).Msg("agentgrpc: server stopped with error")
			}
		}()
	}

	// IdP session-validation / group sync runs in-process for all/api modes
	// (a 15-minute loop never deserved its own daemon); `--mode sync` remains
	// for deployments that want it isolated.
	if cfg.Sync.Enabled && (mode == "all" || mode == "api") && oidcProvider != nil {
		go func() {
			if err := auth.RunIdPSyncLoop(ctx, idpSyncConfig(cfg), pool, oidcProvider, masterKey, enforcer); err != nil {
				log.Error().Err(err).Msg("IdP sync loop stopped with error")
			}
		}()
		log.Info().Msg("IdP sync: running in-process")
	}

	srv := flintserver.New(deps)
	srv.StartAuthStoreCleanup(ctx) // prune expired device codes + MFA tokens
	srv.Run()

	return nil
}

// bootstrapAdmin creates the initial admin when no users exist, printing the
// generated temporary password once to stdout (never to the structured logger,
// which would persist the credential to log aggregation).
func bootstrapAdmin(ctx context.Context, cfg *config.Config, q *db.Queries, pool db.Pool, orgID string) error {
	userCount, _ := q.CountUsers(ctx, orgID)
	if userCount != 0 {
		return nil
	}
	bootstrapEmail := cfg.Bootstrap.EmailOrDefault()
	bootstrapPassword := cfg.Bootstrap.Password
	if bootstrapPassword == "" {
		bootstrapPassword = auth.GenerateRandomPassword()
	}
	hash, hashErr := auth.HashPassword(bootstrapPassword)
	if hashErr != nil {
		return nil // non-fatal, matches prior behavior: server still comes up
	}
	userID, createErr := q.CreateLocalUser(ctx, db.CreateLocalUserParams{
		OrgID: orgID, Email: bootstrapEmail, Name: strPtr("Admin"),
		PasswordHash: &hash,
	})
	if createErr != nil {
		return nil
	}
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

	// Print the temporary password to stdout ONLY when we generated it. Shown once.
	if cfg.Bootstrap.Password == "" {
		fmt.Printf("\n⚡ BOOTSTRAP admin %q — temporary password: %s\n   Save it now; it will not be shown again and a change is required on first login.\n\n",
			bootstrapEmail, bootstrapPassword)
	}
	return nil
}

// buildOIDCProvider sources OIDC config DB-first (API-managed, hot-reloaded),
// falling back to the config file. Non-fatal on an unreachable IdP: discovery
// requires a live network call, and a transient outage must not crash-loop the
// server — health, local login, and break-glass admin access still come up.
func buildOIDCProvider(ctx context.Context, cfg *config.Config, q *db.Queries, masterKey []byte, haveMasterKey bool) (*auth.OIDCProvider, error) {
	var pc *auth.ProviderConfig
	var err error
	if haveMasterKey {
		if pc, err = auth.LoadProviderConfig(ctx, q, masterKey, "oidc"); err != nil {
			return nil, fmt.Errorf("loading OIDC provider config: %w", err)
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
	if pc == nil || !pc.HasOIDC() {
		return nil, nil
	}
	provider, err := auth.BuildOIDCProvider(ctx, *pc, cfg.Server.BaseURL)
	if err != nil {
		log.Error().Err(err).Str("issuer", pc.IssuerURL).
			Msg("OIDC provider init failed — SSO unavailable until reachable; server continues")
		return nil, nil
	}
	log.Info().Str("issuer", pc.IssuerURL).Msg("OIDC provider initialized")
	return provider, nil
}

// buildSAMLProvider mirrors buildOIDCProvider for SAML (DB-first, file
// fallback, non-fatal on unreachable metadata).
func buildSAMLProvider(ctx context.Context, cfg *config.Config, q *db.Queries, masterKey []byte, haveMasterKey bool) (*auth.SAMLProvider, error) {
	var pc *auth.ProviderConfig
	var err error
	if haveMasterKey {
		if pc, err = auth.LoadProviderConfig(ctx, q, masterKey, "saml"); err != nil {
			return nil, fmt.Errorf("loading SAML provider config: %w", err)
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
				return nil, fmt.Errorf("reading SAML cert file: %w", rerr)
			}
			keyPEM, rerr := os.ReadFile(cfg.Auth.SAML.KeyFile)
			if rerr != nil {
				return nil, fmt.Errorf("reading SAML key file: %w", rerr)
			}
			fileCfg.SPCertPEM = string(certPEM)
			fileCfg.SPKeyPEM = string(keyPEM)
		}
		pc = fileCfg
	}
	if pc == nil || !pc.HasSAML() {
		return nil, nil
	}
	provider, err := auth.BuildSAMLProvider(*pc, cfg.Server.BaseURL)
	if err != nil {
		log.Error().Err(err).
			Msg("SAML provider init failed — SSO unavailable until reachable; server continues")
		return nil, nil
	}
	log.Info().Msg("SAML provider initialized")
	return provider, nil
}

// seedComputeProviders ensures the built-in static provider row exists and
// upserts config-file providers: entries. Credentials are never sourced from
// the file (COALESCE in the upsert preserves API-set credentials).
func seedComputeProviders(ctx context.Context, cfg *config.Config, q *db.Queries) error {
	if _, err := q.UpsertComputeProvider(ctx, db.UpsertComputeProviderParams{
		Name: "static", ProviderType: "static", Config: []byte("{}"),
	}); err != nil {
		return fmt.Errorf("static provider: %w", err)
	}
	for _, p := range cfg.Providers {
		if p.Name == "" || p.Type == "" {
			log.Warn().Str("name", p.Name).Msg("providers: entry missing name or type — skipped")
			continue
		}
		cfgJSON, err := json.Marshal(p.Config)
		if err != nil || p.Config == nil {
			cfgJSON = []byte("{}")
		}
		if _, err := q.UpsertComputeProvider(ctx, db.UpsertComputeProviderParams{
			Name: p.Name, ProviderType: p.Type, Config: cfgJSON,
		}); err != nil {
			return fmt.Errorf("provider %s: %w", p.Name, err)
		}
		log.Info().Str("provider", p.Name).Str("type", p.Type).Msg("compute provider bootstrapped from config")
	}
	return nil
}

func strPtr(s string) *string { return &s }
