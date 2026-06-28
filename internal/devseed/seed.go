// Package devseed populates a local Postgres with realistic CI data for local
// development and demos. It builds state through the REAL product paths — it
// upserts projects, inserts pipeline_runs, and starts workflows via the engine —
// so the running worker (with the sim executor, FLINT_EXECUTOR=sim) drives them
// to completion exactly as production would. No fixtures, no parallel query layer.
//
// It is idempotent: each run truncates prior seed data and rebuilds it. Intended
// for `task dev-sim` only — never run it against a real database.
package devseed

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/core/projectcfg"
	"github.com/NerdMeNot/flint/internal/core/runner"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/NerdMeNot/flint/internal/products/ci"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"
	corev1 "k8s.io/api/core/v1"
)

// Dev admin credentials — printed on seed so you can log in immediately.
const (
	AdminEmail    = "admin@flint.dev"
	AdminPassword = "flintdev123"
)

const forgeRef = "github" // forge_connections.display_name UpsertProject keys on

// Run seeds the database. signingKey must match the server/worker JWT secret so
// engine task tokens verify.
func Run(ctx context.Context, pool db.Pool, signingKey []byte) error {
	q := db.New(pool)

	orgID, err := q.GetOrCreateDefaultOrg(ctx)
	if err != nil {
		return fmt.Errorf("org: %w", err)
	}
	if err := q.EnsureDefaultWorkspace(ctx, orgID); err != nil {
		return fmt.Errorf("default workspace: %w", err)
	}
	if err := auth.SeedSystemRoles(ctx, q, orgID); err != nil {
		return fmt.Errorf("system roles: %w", err)
	}

	if err := reset(ctx, pool); err != nil {
		return fmt.Errorf("reset: %w", err)
	}
	if err := ensureAdmin(ctx, q, pool, orgID); err != nil {
		return fmt.Errorf("admin user: %w", err)
	}

	// Forge connection — UpsertProject requires one matching forgeRef. Idempotent:
	// only create it if absent (kept across reseeds so its id is stable). We never
	// call the forge (the seeder compiles pipelines inline), so a stub suffices.
	if sec, _ := q.GetWebhookSecret(ctx, "github"); sec == "" {
		if _, err := q.InsertForgeConnection(ctx, db.InsertForgeConnectionParams{
			ForgeType: "github", DisplayName: forgeRef, WebhookSecret: "dev-webhook-secret",
			CredentialsEnc: []byte("{}"), // stub: the seeder never calls the forge
		}); err != nil {
			return fmt.Errorf("forge connection: %w", err)
		}
	}

	// Workspaces/environments are created once and kept (stable ids). On reseed the
	// create hits a unique-slug conflict, which we ignore — the existing row stays.
	for _, w := range workspaces {
		desc := w.desc
		if _, err := q.CreateWorkspace(ctx, db.CreateWorkspaceParams{
			OrgID: orgID, Name: w.name, Slug: w.slug, Description: &desc,
		}); err != nil {
			log.Debug().Err(err).Str("slug", w.slug).Msg("devseed: workspace exists, keeping")
		}
	}
	for _, e := range environments {
		if _, err := q.CreateEnvironment(ctx, db.CreateEnvironmentParams{
			OrgID: orgID, Name: e.name, Slug: e.slug,
		}); err != nil {
			log.Debug().Err(err).Str("slug", e.slug).Msg("devseed: environment exists, keeping")
		}
	}
	// Tag registry — the curated key:value vocabulary the UI shows for filtering
	// and tagging. Without it the tag UI is empty even though projects carry tags.
	for _, t := range tagKeys {
		if _, err := q.CreateTagKey(ctx, db.CreateTagKeyParams{
			OrgID: orgID, Key: t.key, Label: t.label, AllowedValues: t.values, Color: t.color,
		}); err != nil {
			log.Debug().Err(err).Str("key", t.key).Msg("devseed: tag key exists, keeping")
		}
	}

	// Runner pools (reference mode) — a default + variety for the Runners UI and
	// pipeline validation. Kept across reseeds.
	for _, rp := range runnerPools {
		if _, err := q.GetRunnerPool(ctx, rp.name); err == nil {
			continue
		}
		p := db.UpsertRunnerPoolParams{
			Name: rp.name, Cpu: rp.cpu, Memory: rp.memory, Arch: rp.arch,
			WorkspaceMode: "agent", WorkspaceSize: "10Gi", Mode: "reference",
		}
		if rp.description != "" {
			d := rp.description
			p.Description = &d
		}
		if rp.gpuVendor != "" {
			v, m := rp.gpuVendor, rp.gpuModel
			p.GpuVendor, p.GpuModel = &v, &m
			p.GpuCount = pgtype.Int4{Int32: rp.gpuCount, Valid: true}
		}
		if rp.managed {
			p.Mode = "managed"
			ms, _ := json.Marshal(runner.ManagedSpec{CapacityType: "spot-preferred", ScaleToZero: true}.WithDefaults())
			p.ManagedSpec = ms
			p.NodeSelector, _ = json.Marshal(runner.PoolNodeSelector(rp.name))
			p.Tolerations, _ = json.Marshal([]corev1.Toleration{runner.PoolToleration(rp.name)})
		} else if len(rp.nodeSelector) > 0 {
			// Reference mode: store only the explicit node targeting. Arch is a
			// descriptor, not an auto-pinned selector.
			p.NodeSelector, _ = json.Marshal(rp.nodeSelector)
		}
		if err := q.UpsertRunnerPool(ctx, p); err != nil {
			return fmt.Errorf("runner pool %s: %w", rp.name, err)
		}
	}
	// Mark the standard pool the default (pipelines with no runner: use it).
	if err := q.SetDefaultRunnerPool(ctx, "standard"); err != nil {
		return fmt.Errorf("set default runner pool: %w", err)
	}

	eng := engine.New(pool, signingKey)
	defer eng.Close()

	var projectCount, runCount int
	for _, p := range projects {
		// Go through the SAME shared mapper the API/CRD create paths use, so the
		// seed exercises the real registration shape.
		projID, err := q.UpsertProject(ctx, projectcfg.ToUpsertParams(projectcfg.Spec{
			Repo:        p.repo,
			ForgeRef:    forgeRef,
			DisplayName: p.name,
			Colour:      p.colour,
			Workspace:   p.workspace,
		}))
		if err != nil {
			return fmt.Errorf("project %s: %w", p.repo, err)
		}
		if err := q.UpdateProjectTags(ctx, db.UpdateProjectTagsParams{Tags: p.tags, ID: projID}); err != nil {
			return fmt.Errorf("tags %s: %w", p.repo, err)
		}
		projectCount++

		for _, r := range runsFor(p) {
			if err := triggerRun(ctx, q, eng, orgID, projID, p.repo, r); err != nil {
				log.Warn().Err(err).Str("project", p.repo).Str("run", r.kind).Msg("devseed: trigger run failed")
				continue
			}
			runCount++
		}
	}

	if err := seedAccessControl(ctx, q, pool, orgID); err != nil {
		return fmt.Errorf("access control: %w", err)
	}

	log.Info().Int("projects", projectCount).Int("runs", runCount).Msg("devseed: complete")
	fmt.Printf("\n  ✔ Seeded %d projects, %d runs, %d teams, %d demo users.\n\n"+
		"    Web UI   http://localhost:3000\n"+
		"    Login    %s / %s\n"+
		"    (demo users carol/dave/erin/frank/grace@flint.dev share that password)\n\n",
		projectCount, runCount, len(seedTeams), len(seedUsers)+1, AdminEmail, AdminPassword)
	return nil
}

// reset refreshes ONLY the run/engine data each restart. Identities — orgs,
// projects, workspaces, environments, forge connections, users, teams — are left
// intact and re-ensured idempotently below, so their IDs stay STABLE across
// reseeds. That's deliberate: a developer's open browser session and its project
// links keep working after a restart instead of pointing at recreated rows.
func reset(ctx context.Context, pool db.Pool) error {
	_, err := pool.Exec(ctx,
		`TRUNCATE pipeline_runs, steps, workflows, timers, signals, flint_outbox RESTART IDENTITY CASCADE`)
	return err
}

// ensureAdmin creates the dev admin with known credentials and admin role, so the
// UI is loginable right after seeding. Idempotent across reseeds.
func ensureAdmin(ctx context.Context, q *db.Queries, pool db.Pool, orgID string) error {
	if _, err := q.GetUserByEmail(ctx, db.GetUserByEmailParams{OrgID: orgID, Email: AdminEmail}); err == nil {
		return nil // already exists
	}
	hash, err := auth.HashPassword(AdminPassword)
	if err != nil {
		return err
	}
	name := "Dev Admin"
	if _, err := q.CreateLocalUser(ctx, db.CreateLocalUserParams{
		OrgID: orgID, Email: AdminEmail, Name: &name, PasswordHash: &hash,
	}); err != nil {
		return err
	}
	adminRole, err := q.GetRoleBySlug(ctx, db.GetRoleBySlugParams{OrgID: orgID, Slug: "admin"})
	if err != nil {
		return err
	}
	if err := q.InsertRoleAssignment(ctx, db.InsertRoleAssignmentParams{
		Subject: AdminEmail, RoleID: adminRole.ID,
	}); err != nil {
		return err
	}
	enforcer, err := auth.NewEnforcer(pool)
	if err != nil {
		return err
	}
	return auth.RegeneratePolicies(ctx, q, pool, enforcer)
}

// seedAccessControl creates demo users and internal teams with role assignments,
// so the settings pages (Users / Teams / Roles) have realistic content. Idempotent:
// users skip-if-present, teams use get-or-create, memberships ignore duplicates.
func seedAccessControl(ctx context.Context, q *db.Queries, pool db.Pool, orgID string) error {
	roleID := func(slug string) (string, bool) {
		r, err := q.GetRoleBySlug(ctx, db.GetRoleBySlugParams{OrgID: orgID, Slug: slug})
		if err != nil {
			return "", false
		}
		return r.ID, true
	}

	hash, err := auth.HashPassword(AdminPassword) // shared demo password
	if err != nil {
		return err
	}

	userIDByEmail := make(map[string]string, len(seedUsers))
	for _, u := range seedUsers {
		id, err := ensureLocalUser(ctx, q, orgID, u.email, u.name, hash)
		if err != nil {
			log.Warn().Err(err).Str("email", u.email).Msg("devseed: create user failed")
			continue
		}
		userIDByEmail[u.email] = id
		if rid, ok := roleID(u.role); ok {
			_ = q.InsertRoleAssignment(ctx, db.InsertRoleAssignmentParams{Subject: u.email, RoleID: rid})
		}
	}

	for _, t := range seedTeams {
		tid, err := q.GetOrCreateTeamBySlug(ctx, db.GetOrCreateTeamBySlugParams{OrgID: orgID, Name: t.name, Slug: t.slug})
		if err != nil {
			log.Warn().Err(err).Str("team", t.slug).Msg("devseed: create team failed")
			continue
		}
		if rid, ok := roleID(t.role); ok {
			_ = q.InsertRoleAssignment(ctx, db.InsertRoleAssignmentParams{Subject: "team:" + t.slug, RoleID: rid})
		}
		for _, m := range t.members {
			if uid, ok := userIDByEmail[m]; ok {
				_ = q.AddTeamMember(ctx, db.AddTeamMemberParams{TeamID: tid, UserID: uid})
			}
		}
	}

	// Rebuild Casbin policy from the new assignments.
	if enforcer, err := auth.NewEnforcer(pool); err == nil {
		_ = auth.RegeneratePolicies(ctx, q, pool, enforcer)
	}
	return nil
}

// ensureLocalUser returns the user's ID, creating it if absent (idempotent).
func ensureLocalUser(ctx context.Context, q *db.Queries, orgID, email, name, passwordHash string) (string, error) {
	if u, err := q.GetUserByEmail(ctx, db.GetUserByEmailParams{OrgID: orgID, Email: email}); err == nil {
		return u.ID, nil
	}
	n := name
	return q.CreateLocalUser(ctx, db.CreateLocalUserParams{
		OrgID: orgID, Email: email, Name: &n, PasswordHash: &passwordHash,
	})
}

// triggerRun compiles the run's pipeline and starts it on the engine. The running
// worker advances it; the sim executor scripts each job's outcome and timing.
func triggerRun(ctx context.Context, q *db.Queries, eng engine.Engine, orgID, projID, repo string, r runSpec) error {
	p, err := ci.Parse([]byte(r.pipeline))
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if err := p.Validate(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	waves, err := ci.Compile(p, r.environment)
	if err != nil {
		return fmt.Errorf("compile: %w", err)
	}

	runID := uuid.NewString()
	file := r.workflowFile
	var envPtr *string
	if r.environment != "" {
		envPtr = &r.environment
	}
	if err := q.InsertPipelineRun(ctx, db.InsertPipelineRunParams{
		ID: runID, ProjectID: &projID, OrgID: orgID, WorkflowFile: &file,
		TriggerType: r.trigger, TriggerRef: &r.branch,
		CommitSha: &r.sha, CommitMessage: &r.message, TriggeredBy: &r.author,
		Environment: envPtr,
	}); err != nil {
		return fmt.Errorf("insert run: %w", err)
	}
	if _, err := eng.StartWorkflowWithWaves(ctx, engine.StartWorkflowInput{
		RunID: runID, OrgID: orgID, ProjectID: projID,
		Repo: repo, Ref: r.branch, CommitSHA: r.sha,
		TriggerType: r.trigger, TriggeredBy: r.author, Environment: r.environment,
	}, waves); err != nil {
		return fmt.Errorf("start workflow: %w", err)
	}
	return nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
