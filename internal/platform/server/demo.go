package server

import (
	"context"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/platform/config"
	"github.com/NerdMeNot/flint/pkg/logsink"
	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/rs/zerolog/log"
)

// Demo mode runs the real server, engine, Postgres, API, RBAC and SSE — only the
// execution edge is faked. A fake StepExecutor simulates steps (synthetic logs +
// completion) so the UI can be driven end-to-end against the live contract with
// no Kubernetes. Dev/demo only; selected via `flint server --demo` / FLINT_DEMO.

// demoLogEmitter implements engine.LogEmitter over the same sink + broadcast the
// agent log-ingestion endpoint uses, so the existing log SSE and storage work
// unchanged for synthetic logs.
type demoLogEmitter struct {
	logs logsink.LogSink
	bc   LogStream
}

func (e demoLogEmitter) Emit(ctx context.Context, ref logsink.LogRef, lines []logsink.LogLine) {
	if e.logs != nil {
		_ = e.logs.Write(ctx, ref, lines)
	}
	if e.bc != nil {
		e.bc.Publish(ref.RunID, ref.StepName, lines)
	}
}

// StartDemo wires demo mode: an in-process engine Loop with a fake executor
// (the server normally runs no loop — the worker binary does), then seeds demo
// data. The loop runs in the background until ctx is cancelled; seeding is
// best-effort and idempotent. Call after deps are built, before srv.Run().
func StartDemo(ctx context.Context, cfg *config.Config, eng *engine.PgEngine, deps Deps) {
	emitter := demoLogEmitter{logs: deps.Logs, bc: deps.LogBroadcast}
	demo := engine.NewDemoExecutor(eng.CompleteStep, emitter)

	// Container exec types are faked; http still runs in-process for real.
	executors := engine.ExecutorRegistry{
		"run":   demo,
		"use":   demo,
		"steps": demo,
		"http":  engine.NewHTTPExecutor(eng.CompleteStep),
	}
	loop := engine.NewLoop(eng, executors, engine.LoopConfig{SigningKey: []byte(cfg.Auth.JWT.Secret)})
	go func() {
		if err := loop.Run(ctx); err != nil {
			log.Error().Err(err).Msg("demo: engine loop stopped")
		}
	}()
	log.Info().Msg("demo mode: in-process engine loop + fake executor started")

	if err := seedDemo(ctx, eng, deps); err != nil {
		log.Warn().Err(err).Msg("demo: seeding failed (continuing)")
	}
}

// seedDemo populates projects and runs so the UI has data immediately. Projects
// upsert (idempotent by repo). Runs use fixed IDs and ignore duplicate-insert
// errors, so a restart against the same DB doesn't duplicate; on a fresh DB the
// fake executor drives them live → terminal within seconds of startup.
func seedDemo(ctx context.Context, eng *engine.PgEngine, deps Deps) error {
	orgID, err := deps.Q.GetOrCreateDefaultOrg(ctx)
	if err != nil {
		return err
	}

	type proj struct {
		key, name, ws, colour, desc string
	}
	projects := []proj{
		{"github.com/acme/product-search", "Product Search", "platform", "#22d3ee", "Search API and indexing"},
		{"github.com/acme/checkout", "Checkout", "platform", "#a78bfa", "Payments and cart"},
		{"github.com/acme/web-app", "Web App", "frontend", "#f472b6", "Customer web frontend"},
		{"github.com/acme/data-pipeline", "Data Pipeline", "data", "#34d399", "ETL and analytics"},
		{"github.com/acme/identity", "Identity", "platform", "#fbbf24", "Auth and SSO"},
	}
	ids := map[string]string{}
	for _, p := range projects {
		id, err := deps.Q.UpsertProject(ctx, db.UpsertProjectParams{
			RepoPath:      p.key,
			RepoUrl:       "https://" + p.key,
			DisplayName:   dptr(p.name),
			Description:   dptr(p.desc),
			Colour:        p.colour,
			DefaultBranch: "main",
			ForgeRef:      "github",
			Workspace:     p.ws,
		})
		if err != nil {
			log.Warn().Err(err).Str("project", p.name).Msg("demo: upsert project failed")
			continue
		}
		ids[p.key] = id
	}

	type runSpec struct {
		id, project, branch, env, sha, msg, trigger string
		waves                                       [][]pipeline.Step
	}
	runs := []runSpec{
		{"demo-run-1", "github.com/acme/product-search", "main", "production", "c9d1e33", "chore: upgrade TanStack Router to v1.168", "push", demoPipelineGated()},
		{"demo-run-2", "github.com/acme/checkout", "main", "staging", "a1b2c3d", "fix: handle expired payment tokens", "push", demoPipelineFull()},
		{"demo-run-3", "github.com/acme/web-app", "feat/new-nav", "", "f4e5d6c", "feat: redesign primary navigation", "pull_request", demoPipelineSimple()},
		{"demo-run-4", "github.com/acme/data-pipeline", "main", "production", "9a8b7c6", "perf: parallelize nightly aggregation", "schedule", demoPipelineFull()},
		{"demo-run-5", "github.com/acme/identity", "main", "staging", "1f2e3d4", "feat: add WebAuthn enrolment", "push", demoPipelineSimple()},
		{"demo-run-6", "github.com/acme/checkout", "fix/tax-rounding", "", "5d6e7f8", "fix: tax rounding off-by-one", "pull_request", demoPipelineSimple()},
	}
	for _, r := range runs {
		projID, ok := ids[r.project]
		if !ok {
			continue
		}
		wf := "ci.yaml"
		var envPtr *string
		if r.env != "" {
			envPtr = dptr(r.env)
		}
		err := deps.Q.InsertPipelineRun(ctx, db.InsertPipelineRunParams{
			ID:            r.id,
			ProjectID:     &projID,
			OrgID:         orgID,
			WorkflowFile:  &wf,
			TriggerType:   r.trigger,
			TriggerRef:    dptr(r.branch),
			CommitSha:     dptr(r.sha),
			CommitMessage: dptr(r.msg),
			TriggeredBy:   dptr("demo@flint.dev"),
			Environment:   envPtr,
		})
		if err != nil {
			// Already seeded (duplicate ID) on a previous run — leave it be.
			continue
		}
		if _, err := eng.StartWorkflowWithWaves(ctx, engine.StartWorkflowInput{
			RunID:       r.id,
			OrgID:       orgID,
			ProjectID:   projID,
			Repo:        r.project,
			Ref:         r.branch,
			CommitSHA:   r.sha,
			TriggerType: r.trigger,
			TriggeredBy: "demo@flint.dev",
			Environment: r.env,
		}, r.waves); err != nil {
			log.Warn().Err(err).Str("run", r.id).Msg("demo: start workflow failed")
		}
	}

	log.Info().Int("projects", len(ids)).Int("runs", len(runs)).Msg("demo: seeded")
	return nil
}

// ── synthetic pipelines ─────────────────────────────────────────────────────

func demoPipelineGated() [][]pipeline.Step {
	return [][]pipeline.Step{
		{{Name: "install-deps", Run: pipeline.Cmd("npm ci"), Image: "node:20"}},
		{
			{Name: "lint", Run: pipeline.Cmd("npm run lint"), DependsOn: []string{"install-deps"}},
			{Name: "unit-tests", Run: pipeline.Cmd("npm test"), DependsOn: []string{"install-deps"}, Retry: &pipeline.RetrySpec{Attempts: 3}},
			{Name: "integration-tests", Run: pipeline.Cmd("npm run test:integration"), DependsOn: []string{"install-deps"}},
		},
		{{Name: "build", Run: pipeline.Cmd("npm run build"), DependsOn: []string{"lint", "unit-tests", "integration-tests"}}},
		{{Name: "docker-push", Run: pipeline.Cmd("docker push acme/app"), DependsOn: []string{"build"}}},
		{{Name: "deploy-staging", Run: pipeline.Cmd("flint deploy staging"), DependsOn: []string{"docker-push"}}},
		{{Name: "smoke-tests", Run: pipeline.Cmd("npm run test:smoke"), DependsOn: []string{"deploy-staging"}}},
		{{Name: "approve-production", Gate: &pipeline.Gate{Approvers: []string{"platform"}}, DependsOn: []string{"smoke-tests"}}},
		{{Name: "deploy-prod", Run: pipeline.Cmd("flint deploy prod"), DependsOn: []string{"approve-production"}}},
	}
}

func demoPipelineFull() [][]pipeline.Step {
	return [][]pipeline.Step{
		{{Name: "install-deps", Run: pipeline.Cmd("go mod download"), Image: "golang:1.26"}},
		{
			{Name: "vet", Run: pipeline.Cmd("go vet ./..."), DependsOn: []string{"install-deps"}},
			{Name: "test", Run: pipeline.Cmd("go test ./..."), DependsOn: []string{"install-deps"}, Retry: &pipeline.RetrySpec{Attempts: 2}},
		},
		{{Name: "build", Run: pipeline.Cmd("go build ./..."), DependsOn: []string{"vet", "test"}}},
		{{Name: "deploy", Run: pipeline.Cmd("flint deploy"), DependsOn: []string{"build"}}},
	}
}

func demoPipelineSimple() [][]pipeline.Step {
	return [][]pipeline.Step{
		{{Name: "install-deps", Run: pipeline.Cmd("npm ci"), Image: "node:20"}},
		{{Name: "lint", Run: pipeline.Cmd("npm run lint"), DependsOn: []string{"install-deps"}}},
		{{Name: "test", Run: pipeline.Cmd("npm test"), DependsOn: []string{"lint"}, Retry: &pipeline.RetrySpec{Attempts: 3}}},
		{{Name: "build", Run: pipeline.Cmd("npm run build"), DependsOn: []string{"test"}}},
	}
}

func dptr[T any](v T) *T { return &v }
