package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/pkg/logsink"
	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// Mock mode runs the whole server with NO database and NO Kubernetes: in-memory
// implementations of the db.Querier and engine.Engine seams serve canned data so
// the frontend always talks to the Go backend, which itself answers in mock or
// live mode. Selected via `flint server --mock` / FLINT_API_MODE=mock. Auth and
// RBAC are bypassed (see authMiddleware + a nil Enforcer), so no users/sessions
// tables are needed.
//
// CONSISTENCY CONTRACT: a run's per-step states are the single source of truth.
// The run's overall status (shown in lists) is DERIVED from those steps via
// statusFromSteps, so the "outside" (list) can never disagree with the "inside"
// (detail / steps / logs). Everything that needs a status calls steps(r) first.

const mockOrgID = "org-mock"

// ── canned dataset ──────────────────────────────────────────────────────────

type mockStep struct {
	name        string
	wave        int
	gate        bool
	maxAttempts int // 0 == 1; >1 means the step may show retries
}

// A small library of pipeline shapes so step/waterfall/DAG views vary across
// runs. Per-run step statuses are derived from the run's state (and elapsed time
// for live runs); each run references a shape by key.
var pipelines = map[string][]mockStep{
	"deploy": {
		{name: "install-deps", wave: 0}, {name: "lint", wave: 1}, {name: "unit-tests", wave: 1, maxAttempts: 2},
		{name: "integration-tests", wave: 1, maxAttempts: 3}, {name: "build", wave: 2}, {name: "deploy-staging", wave: 3},
		{name: "smoke-tests", wave: 4, maxAttempts: 2}, {name: "approve-production", wave: 5, gate: true}, {name: "deploy-prod", wave: 6},
	},
	"library": {
		{name: "install-deps", wave: 0}, {name: "lint", wave: 1}, {name: "unit-tests", wave: 1, maxAttempts: 2},
		{name: "typecheck", wave: 1}, {name: "build", wave: 2}, {name: "publish", wave: 3},
	},
	"preview": {
		{name: "install-deps", wave: 0}, {name: "lint", wave: 1}, {name: "unit-tests", wave: 1, maxAttempts: 2},
		{name: "build", wave: 2}, {name: "deploy-preview", wave: 3},
	},
	"infra": {
		{name: "tf-init", wave: 0}, {name: "tf-fmt", wave: 1}, {name: "tf-validate", wave: 1},
		{name: "tf-plan", wave: 2}, {name: "approve-apply", wave: 3, gate: true}, {name: "tf-apply", wave: 4},
	},
	"mobile": {
		{name: "install-deps", wave: 0}, {name: "lint", wave: 1}, {name: "unit-tests", wave: 1, maxAttempts: 2},
		{name: "build-ios", wave: 2}, {name: "build-android", wave: 2}, {name: "e2e", wave: 3, maxAttempts: 3},
		{name: "upload-testflight", wave: 4},
	},
	// A wide matrix wave to stress the DAG / waterfall layout.
	"matrix": {
		{name: "install-deps", wave: 0},
		{name: "test-node-18", wave: 1, maxAttempts: 2}, {name: "test-node-20", wave: 1, maxAttempts: 2},
		{name: "test-node-22", wave: 1, maxAttempts: 2}, {name: "test-py-3.10", wave: 1}, {name: "test-py-3.11", wave: 1},
		{name: "test-windows-latest", wave: 1}, {name: "test-macos-latest", wave: 1},
		{name: "build", wave: 2}, {name: "deploy-staging", wave: 3},
	},
	// Single-step pipeline — exercises the minimal timeline.
	"lint-only": {
		{name: "lint", wave: 0},
	},
	// Workflows product (http-step DAG, no gate).
	"workflow": {
		{name: "snapshot-db", wave: 0}, {name: "export-orders", wave: 1}, {name: "export-users", wave: 1},
		{name: "transform", wave: 2}, {name: "load-warehouse", wave: 3}, {name: "notify-slack", wave: 4},
	},
}

type mockProject struct {
	id, name, repo, workspace, colour, pipe string
	tags                                    []string
	recent                                  []string // recent run statuses, newest first
	total, succeeded                        int
}

type mockRun struct {
	id, projectID, branch, sha, msg, by, trigger, env string
	status                                            string // base intent: succeeded|failed|running|pending|cancelled (gate runs use gate=true)
	startedAgo                                        time.Duration
	durationMs                                        int32
	live                                              bool   // self-advances on the boot clock
	gate                                              bool   // parked on an approval gate until acted on
	pipe                                              string // pipeline shape key
	failStep                                          string // for failed runs: which step failed (else the middle)
}

// runOverride captures a live mutation (gate approve/reject, cancel) applied at
// runtime through the engine seam, so a demo click actually changes the run.
type runOverride struct {
	approved  bool
	rejected  bool
	cancelled bool
	at        time.Time
}

// mockWorkflowSchedule is a cron-scheduled workflow (Workflows product).
type mockWorkflowSchedule struct {
	id, name, cron string
	enabled        bool
	nextInMin      int // next run, minutes from now
	lastAgoMin     int // last run, minutes ago (0 == never)
}

type mockData struct {
	boot         time.Time
	projects     []mockProject
	runs         []mockRun
	workflowRuns []mockRun // Workflows product runs (separate from CI runs)
	wfSchedules  []mockWorkflowSchedule
	mu           sync.RWMutex
	overrides    map[string]*runOverride
}

func newMockData() *mockData {
	projects := []mockProject{
		{id: "proj-search", name: "Product Search", repo: "github.com/acme/product-search", workspace: "platform", colour: "#22d3ee", pipe: "deploy", tags: []string{"domain:search", "tier:1", "lang:go"}, recent: []string{"running", "succeeded", "succeeded", "failed", "succeeded"}, total: 142, succeeded: 128},
		{id: "proj-checkout", name: "Checkout", repo: "github.com/acme/checkout", workspace: "platform", colour: "#a78bfa", pipe: "deploy", tags: []string{"domain:payments", "tier:1", "compliance:pci"}, recent: []string{"running", "succeeded", "succeeded", "succeeded"}, total: 217, succeeded: 209},
		{id: "proj-payments", name: "Payments API", repo: "github.com/acme/payments", workspace: "platform", colour: "#f59e0b", pipe: "deploy", tags: []string{"domain:payments", "tier:1", "compliance:pci", "lang:go"}, recent: []string{"failed", "succeeded", "succeeded", "failed"}, total: 188, succeeded: 170},
		{id: "proj-auth", name: "Auth Service", repo: "github.com/acme/auth", workspace: "platform", colour: "#60a5fa", pipe: "deploy", tags: []string{"domain:identity", "tier:1", "lang:go"}, recent: []string{"pending", "succeeded", "succeeded", "succeeded"}, total: 96, succeeded: 94},
		{id: "proj-edge", name: "Edge Gateway", repo: "github.com/acme/edge-gateway", workspace: "platform", colour: "#2dd4bf", pipe: "matrix", tags: []string{"domain:platform", "tier:1", "lang:rust"}, recent: []string{"running", "succeeded", "failed", "succeeded"}, total: 74, succeeded: 68},
		{id: "proj-billing", name: "Billing", repo: "github.com/acme/billing", workspace: "platform", colour: "#fb7185", pipe: "deploy", tags: []string{"domain:payments", "tier:2", "compliance:pci"}, recent: []string{"succeeded", "succeeded", "succeeded", "cancelled"}, total: 53, succeeded: 50},
		{id: "proj-notifications", name: "Notifications", repo: "github.com/acme/notifications", workspace: "platform", colour: "#c084fc", pipe: "library", tags: []string{"domain:messaging", "tier:2", "lang:ts"}, recent: []string{"succeeded", "succeeded", "succeeded"}, total: 41, succeeded: 40},
		{id: "proj-web", name: "Web App", repo: "github.com/acme/web-app", workspace: "frontend", colour: "#f472b6", pipe: "preview", tags: []string{"lang:ts", "tier:1"}, recent: []string{"failed", "failed", "succeeded", "failed"}, total: 130, succeeded: 88},
		{id: "proj-dashboard", name: "Analytics Dashboard", repo: "github.com/acme/dashboard", workspace: "frontend", colour: "#a3e635", pipe: "preview", tags: []string{"lang:ts", "domain:analytics"}, recent: []string{"succeeded", "succeeded", "failed", "succeeded"}, total: 77, succeeded: 70},
		{id: "proj-design", name: "Design System", repo: "github.com/acme/design-system", workspace: "frontend", colour: "#e879f9", pipe: "library", tags: []string{"lang:ts", "tier:3"}, recent: []string{"succeeded", "succeeded", "succeeded", "succeeded"}, total: 64, succeeded: 63},
		{id: "proj-docs", name: "Docs Site", repo: "github.com/acme/docs", workspace: "frontend", colour: "#38bdf8", pipe: "lint-only", tags: []string{"lang:ts", "tier:3"}, recent: []string{"succeeded", "failed", "succeeded"}, total: 39, succeeded: 35},
		{id: "proj-i18n", name: "Internationalization & Localization Platform (Legacy + Next-Gen Migration Service)", repo: "github.com/acme/internationalization-and-localization-platform-next-gen", workspace: "frontend", colour: "#818cf8", pipe: "deploy", tags: []string{"lang:ts", "tier:2", "domain:platform"}, recent: []string{"succeeded", "failed", "succeeded", "succeeded"}, total: 28, succeeded: 24},
		{id: "proj-data", name: "Data Pipeline", repo: "github.com/acme/data-pipeline", workspace: "data", colour: "#34d399", pipe: "deploy", tags: []string{"domain:analytics", "tier:2", "lang:python"}, recent: []string{"running", "succeeded", "succeeded"}, total: 121, succeeded: 112},
		{id: "proj-etl", name: "ETL Jobs", repo: "github.com/acme/etl", workspace: "data", colour: "#4ade80", pipe: "deploy", tags: []string{"domain:analytics", "tier:2", "lang:python"}, recent: []string{"succeeded", "failed", "succeeded", "succeeded"}, total: 88, succeeded: 79},
		{id: "proj-ml-train", name: "Model Training", repo: "github.com/acme/ml-training", workspace: "ml", colour: "#facc15", pipe: "deploy", tags: []string{"domain:ml", "tier:2", "lang:python"}, recent: []string{"running", "succeeded", "failed"}, total: 57, succeeded: 44},
		{id: "proj-ml-serve", name: "Model Serving", repo: "github.com/acme/ml-serving", workspace: "ml", colour: "#fbbf24", pipe: "deploy", tags: []string{"domain:ml", "tier:1", "lang:python"}, recent: []string{"succeeded", "succeeded", "succeeded"}, total: 49, succeeded: 47},
		{id: "proj-infra", name: "Infrastructure", repo: "github.com/acme/infrastructure", workspace: "infra", colour: "#a78bfa", pipe: "infra", tags: []string{"domain:platform", "tier:1"}, recent: []string{"running", "succeeded", "succeeded", "succeeded"}, total: 203, succeeded: 195},
		{id: "proj-mobile-ios", name: "iOS App", repo: "github.com/acme/ios-app", workspace: "mobile", colour: "#f87171", pipe: "mobile", tags: []string{"lang:swift", "tier:1"}, recent: []string{"running", "succeeded", "failed", "succeeded"}, total: 68, succeeded: 58},
		{id: "proj-mobile-android", name: "Android App", repo: "github.com/acme/android-app", workspace: "mobile", colour: "#4ade80", pipe: "mobile", tags: []string{"lang:kotlin", "tier:1"}, recent: []string{"succeeded", "succeeded", "failed"}, total: 62, succeeded: 54},
		// A project with zero runs — exercises empty-state rendering.
		{id: "proj-sandbox", name: "Sandbox", repo: "github.com/acme/sandbox", workspace: "unsorted", colour: "#94a3b8", pipe: "lint-only", tags: nil, recent: nil, total: 0, succeeded: 0},
	}
	return &mockData{
		boot:         time.Now(),
		projects:     projects,
		runs:         buildMockRuns(projects),
		workflowRuns: buildMockWorkflowRuns(),
		wfSchedules: []mockWorkflowSchedule{
			{id: "wfs-nightly", name: "Nightly warehouse load", cron: "0 2 * * *", enabled: true, nextInMin: 540, lastAgoMin: 900},
			{id: "wfs-hourly", name: "Hourly metrics rollup", cron: "0 * * * *", enabled: true, nextInMin: 35, lastAgoMin: 25},
			{id: "wfs-weekly", name: "Weekly compliance export", cron: "0 6 * * 1", enabled: false, nextInMin: 4320, lastAgoMin: 0},
		},
		overrides: map[string]*runOverride{},
	}
}

// buildMockWorkflowRuns seeds the Workflows product's run feed (http-step DAGs,
// no gates), kept separate from CI runs so the two lists don't bleed together.
func buildMockWorkflowRuns() []mockRun {
	return []mockRun{
		{id: "wf-run-1", branch: "—", sha: "—", msg: "Nightly warehouse load", by: "scheduler", trigger: "schedule", status: "running", startedAgo: 40 * time.Second, live: true, pipe: "workflow"},
		{id: "wf-run-2", branch: "—", sha: "—", msg: "Hourly metrics rollup", by: "scheduler", trigger: "schedule", status: "succeeded", startedAgo: 25 * time.Minute, pipe: "workflow"},
		{id: "wf-run-3", branch: "—", sha: "—", msg: "Manual backfill 2024-Q4", by: "api", trigger: "manual", status: "failed", startedAgo: 2 * time.Hour, failStep: "transform", pipe: "workflow"},
		{id: "wf-run-4", branch: "—", sha: "—", msg: "Nightly warehouse load", by: "scheduler", trigger: "schedule", status: "succeeded", startedAgo: 15 * time.Hour, pipe: "workflow"},
		{id: "wf-run-5", branch: "—", sha: "—", msg: "Hourly metrics rollup", by: "scheduler", trigger: "schedule", status: "succeeded", startedAgo: 85 * time.Minute, pipe: "workflow"},
	}
}

// buildMockRuns produces hand-authored "active" runs (live, gated, edge cases)
// followed by a long historical tail, so lists feel lived-in and the run-detail
// / timeline views have plenty — and plenty of variety — to show.
func buildMockRuns(projects []mockProject) []mockRun {
	const longBranch = "feature/PLAT-48217-introduce-experimental-multi-region-active-active-failover-and-replication-subsystem"
	const longMsg = "refactor(search): replace the bespoke in-memory inverted index with a sharded, write-ahead-logged segment store; migrate all read paths behind a feature flag; backfill historical documents in batches of 10k; add comprehensive metrics, tracing spans, and a kill-switch — see RFC-204 for the full rollout and rollback plan"

	runs := []mockRun{
		// Active / notable.
		{id: "mock-run-1", projectID: "proj-search", branch: "main", sha: "c9d1e33", msg: "chore: upgrade TanStack Router to v1.168", by: "carol", trigger: "push", env: "production", status: "running", startedAgo: 30 * time.Second, live: true, pipe: "deploy"},
		{id: "mock-run-2", projectID: "proj-ml-train", branch: "main", sha: "7b3f9a1", msg: "feat: switch to bf16 mixed precision", by: "frank", trigger: "push", env: "production", status: "running", startedAgo: 45 * time.Second, live: true, pipe: "deploy"},
		{id: "mock-run-3", projectID: "proj-mobile-ios", branch: "main", sha: "2c4e6a8", msg: "feat: offline mode for the cart", by: "heidi", trigger: "push", env: "staging", status: "running", startedAgo: 70 * time.Second, live: true, pipe: "mobile"},
		{id: "mock-run-4", projectID: "proj-checkout", branch: "main", sha: "a1b2c3d", msg: "feat: one-click checkout for returning users", by: "dave", trigger: "push", env: "production", status: "running", startedAgo: 12 * time.Minute, gate: true, pipe: "deploy"},
		{id: "mock-run-5", projectID: "proj-infra", branch: "main", sha: "e5f6a7b", msg: "chore: scale node pool to 6 replicas", by: "oscar", trigger: "push", env: "production", status: "running", startedAgo: 25 * time.Minute, gate: true, pipe: "infra"},
		{id: "mock-run-6", projectID: "proj-web", branch: "feat/new-nav", sha: "f4e5d6c", msg: "feat: redesign primary navigation", by: "erin", trigger: "pull_request", status: "failed", startedAgo: 22 * time.Minute, durationMs: 90000, failStep: "unit-tests", pipe: "preview"},
		{id: "mock-run-7", projectID: "proj-payments", branch: "main", sha: "9a8b7c6", msg: "fix: retry Stripe webhook on 5xx", by: "grace", trigger: "push", env: "production", status: "failed", startedAgo: 40 * time.Minute, durationMs: 132000, failStep: "integration-tests", pipe: "deploy"},
		{id: "mock-run-8", projectID: "proj-billing", branch: "main", sha: "3d5f7b9", msg: "feat: usage-based invoicing", by: "ivan", trigger: "push", env: "production", status: "succeeded", startedAgo: 55 * time.Minute, durationMs: 204000, pipe: "deploy"},

		// Edge cases — long strings, unusual states, unusual shapes.
		{id: "mock-run-9", projectID: "proj-search", branch: longBranch, sha: "ab12cd34", msg: longMsg, by: "this-is-a-deliberately-long-service-account-name-bot", trigger: "pull_request", status: "failed", startedAgo: 3 * time.Hour, durationMs: 318000, failStep: "integration-tests", pipe: "deploy"},
		{id: "mock-run-10", projectID: "proj-data", branch: "main", sha: "cc44dd55", msg: "feat: nightly aggregation v2", by: "frank", trigger: "schedule", env: "production", status: "cancelled", startedAgo: 90 * time.Minute, durationMs: 61000, pipe: "deploy"},
		{id: "mock-run-11", projectID: "proj-auth", branch: "main", sha: "ee66ff77", msg: "chore: rotate signing keys", by: "carol", trigger: "manual", env: "production", status: "pending", startedAgo: 20 * time.Second, pipe: "deploy"},
		{id: "mock-run-12", projectID: "proj-edge", branch: "feat/h3-routing", sha: "11aa22bb", msg: "feat: HTTP/3 routing across the matrix", by: "oscar", trigger: "push", env: "staging", status: "running", startedAgo: 50 * time.Second, live: true, pipe: "matrix"},
		{id: "mock-run-13", projectID: "proj-docs", branch: "main", sha: "33cc44dd", msg: "docs: fix broken anchor links", by: "judy", trigger: "push", status: "succeeded", startedAgo: 4 * time.Hour, durationMs: 22000, pipe: "lint-only"},
		{id: "mock-run-14", projectID: "proj-mobile-android", branch: "fix/crash-on-launch", sha: "55ee66ff", msg: "fix: NPE on cold launch", by: "heidi", trigger: "pull_request", status: "failed", startedAgo: 5 * time.Hour, durationMs: 41000, failStep: "install-deps", pipe: "mobile"},
		{id: "mock-run-15", projectID: "proj-notifications", branch: "main", sha: "77aa88bb", msg: "feat: 🎉 emoji reactions + 日本語 localization for push titles", by: "mallory", trigger: "push", status: "succeeded", startedAgo: 6 * time.Hour, durationMs: 88000, pipe: "library"},
		{id: "mock-run-16", projectID: "proj-i18n", branch: "main", sha: "99cc00dd", msg: "perf: lazy-load locale bundles", by: "judy", trigger: "push", env: "production", status: "succeeded", startedAgo: 9 * time.Hour, durationMs: 642000, pipe: "deploy"},
	}

	authors := []string{"carol", "dave", "erin", "frank", "grace", "heidi", "ivan", "judy", "mallory", "oscar"}
	messages := []string{
		"refactor: extract shared retry helper", "fix: null check on empty cart", "feat: add structured request logging",
		"chore: bump dependencies", "perf: cache search suggestions", "test: cover the expiry edge case",
		"fix: handle timezone in scheduled jobs", "feat: paginate the audit log", "docs: update runbook",
		"fix: off-by-one in tax rounding", "feat: dark mode polish", "chore: tighten lint rules",
		"fix: race in connection pool", "feat: webhook signature verification", "perf: batch DB writes",
	}
	branchPool := []string{"main", "main", "main", "fix/flaky-test", "feat/api-v2", "chore/deps", "fix/memory-leak", "feat/rate-limit"}
	triggers := []string{"push", "push", "pull_request", "schedule", "manual"}

	seq := len(runs)
	for i := 0; i < 44; i++ {
		seq++
		p := projects[i%len(projects)]
		if p.id == "proj-sandbox" { // keep the empty-state project empty
			p = projects[0]
		}
		status := "succeeded"
		if i%5 == 2 {
			status = "failed"
		}
		trigger := triggers[i%len(triggers)]
		r := mockRun{
			id:         fmt.Sprintf("mock-run-%d", seq),
			projectID:  p.id,
			branch:     branchPool[i%len(branchPool)],
			sha:        shaFor(i),
			msg:        messages[i%len(messages)],
			by:         authors[i%len(authors)],
			trigger:    trigger,
			env:        envFor(p.pipe, trigger, i),
			status:     status,
			startedAgo: time.Duration(i+1) * 71 * time.Minute,
			pipe:       p.pipe,
		}
		runs = append(runs, r)
	}
	return runs
}

func shaFor(i int) string {
	const hex = "0123456789abcdef"
	b := make([]byte, 7)
	x := i*2654435761 + 40503
	for j := range b {
		b[j] = hex[(x>>(uint(j)*4))&0xf]
	}
	return string(b)
}

func envFor(pipe, trigger string, i int) string {
	if trigger == "pull_request" || pipe == "library" || pipe == "preview" || pipe == "lint-only" {
		return ""
	}
	if i%3 == 0 {
		return "staging"
	}
	return "production"
}

func (m *mockData) project(id string) *mockProject {
	for i := range m.projects {
		if m.projects[i].id == id {
			return &m.projects[i]
		}
	}
	return nil
}

func (m *mockData) run(id string) *mockRun {
	for i := range m.runs {
		if m.runs[i].id == id {
			return &m.runs[i]
		}
	}
	return nil
}

// anyRun looks up a CI run OR a Workflows-product run by id.
func (m *mockData) anyRun(id string) *mockRun {
	if r := m.run(id); r != nil {
		return r
	}
	for i := range m.workflowRuns {
		if m.workflowRuns[i].id == id {
			return &m.workflowRuns[i]
		}
	}
	return nil
}

// override returns a copy of the runtime mutation for a run (zero value if none).
func (m *mockData) override(runID string) runOverride {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if o, ok := m.overrides[runID]; ok {
		return *o
	}
	return runOverride{}
}

// applyOverride records a runtime mutation (gate decision / cancel).
func (m *mockData) applyOverride(runID string, o runOverride) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o.at = time.Now()
	m.overrides[runID] = &o
}

// liveProgress is how many pipeline steps a live run has cleared (one every ~6s
// since boot), so the run advances on its own and the SSE/poll shows it.
func (m *mockData) liveProgress() int {
	return int(time.Since(m.boot) / (6 * time.Second))
}

func execTypeOf(gate bool) string {
	if gate {
		return "gate"
	}
	return "run"
}

func gateIndexOf(pipe []mockStep) int {
	for i, st := range pipe {
		if st.gate {
			return i
		}
	}
	return -1
}

// statusFromSteps is the SINGLE place a run's overall status is decided. Both the
// list and the detail derive from this, so they can never disagree.
func statusFromSteps(steps []engine.StepState) string {
	anyActive, anyDone, allTerminal := false, false, true
	for _, s := range steps {
		switch s.Status {
		case "failed":
			return "failed"
		case "cancelled":
			return "cancelled"
		case "running", "waiting", "queued":
			anyActive, allTerminal = true, false
			anyDone = true
		case "pending":
			allTerminal = false
		case "succeeded", "skipped":
			anyDone = true
		}
	}
	switch {
	case anyActive:
		return "running"
	case allTerminal:
		return "succeeded" // everything succeeded/skipped
	case anyDone:
		return "running" // some done, rest pending, none active → in progress
	default:
		return "pending" // nothing started yet
	}
}

func (m *mockData) pipe(r *mockRun) []mockStep {
	if p, ok := pipelines[r.pipe]; ok {
		return p
	}
	return pipelines["deploy"]
}

// effectiveStatus is derived from the run's steps — see the consistency contract.
func (m *mockData) effectiveStatus(r *mockRun) string {
	return statusFromSteps(m.steps(r))
}

// steps builds the per-step state for a run. This is the source of truth; the run
// status is whatever statusFromSteps makes of it.
func (m *mockData) steps(r *mockRun) []engine.StepState {
	pipe := m.pipe(r)
	started := time.Now().Add(-r.startedAgo)
	out := make([]engine.StepState, 0, len(pipe))
	gateIdx := gateIndexOf(pipe)
	ov := m.override(r.id)

	// Resolve which kind of run this is and the "cut" index that drives statuses.
	prog := m.liveProgress()
	failIdx := len(pipe) / 2
	if r.status == "failed" {
		for i, st := range pipe {
			if st.name == r.failStep {
				failIdx = i
				break
			}
		}
	}
	// Approved gate runs advance past the gate on a per-decision clock.
	postGate := 0
	if r.gate && ov.approved {
		postGate = int(time.Since(ov.at) / (4 * time.Second))
	}
	// Cancelled runs stop at a cut index.
	cancelIdx := len(pipe) / 2
	if ov.cancelled {
		// cancel a live/gated run roughly where it was.
		switch {
		case r.gate && gateIdx >= 0:
			cancelIdx = gateIdx
		case r.live:
			if prog < len(pipe) {
				cancelIdx = prog
			}
		}
	}

	for i, st := range pipe {
		s := engine.StepState{Name: st.name, Wave: st.wave, ExecType: execTypeOf(st.gate)}
		switch {
		case ov.cancelled || r.status == "cancelled":
			switch {
			case i < cancelIdx:
				s.Status = "succeeded"
			case i == cancelIdx:
				s.Status = "cancelled"
			default:
				s.Status = "skipped"
			}
		case r.gate && ov.rejected:
			switch {
			case i < gateIdx:
				s.Status = "succeeded"
			case i == gateIdx:
				s.Status = "failed" // rejected approval
			default:
				s.Status = "skipped"
			}
		case r.gate && ov.approved:
			switch {
			case i <= gateIdx, i-gateIdx <= postGate:
				s.Status = "succeeded"
			case i-gateIdx == postGate+1:
				s.Status = "running"
			default:
				s.Status = "pending"
			}
		case r.gate && gateIdx >= 0:
			switch {
			case i < gateIdx:
				s.Status = "succeeded"
			case i == gateIdx:
				s.Status = "waiting" // parked awaiting approval
			default:
				s.Status = "pending"
			}
		case r.live:
			switch {
			case i < prog:
				s.Status = "succeeded"
			case i == prog:
				s.Status = "running"
			default:
				s.Status = "pending"
			}
		case r.status == "failed":
			switch {
			case i < failIdx:
				s.Status = "succeeded"
			case i == failIdx:
				s.Status = "failed"
			default:
				s.Status = "skipped"
			}
		case r.status == "succeeded":
			s.Status = "succeeded"
		default: // pending
			s.Status = "pending"
		}

		// Attempts: flaky-capable steps occasionally show a retry once they've run.
		maxA := st.maxAttempts
		if maxA == 0 {
			maxA = 1
		}
		s.MaxAttempts = maxA
		s.Attempt = 1
		if maxA > 1 && (s.Status == "succeeded" || s.Status == "failed" || s.Status == "running") {
			if (len(r.id)+i)%3 == 0 {
				s.Attempt = 2
			}
		}

		// Timestamps for anything that has started.
		if s.Status != "pending" && s.Status != "skipped" {
			sched := started.Add(time.Duration(i) * 25 * time.Second)
			start := sched.Add(2 * time.Second)
			s.ScheduledAt = &sched
			s.StartedAt = &start
			switch s.Status {
			case "succeeded", "failed", "cancelled":
				fin := start.Add(20 * time.Second)
				s.FinishedAt = &fin
				if s.Status == "failed" {
					code := 1
					s.ExitCode = &code
					s.Error = "command failed with exit code 1"
				}
			}
		}
		out = append(out, s)
	}
	return out
}

func (m *mockData) dagWaves(r *mockRun) []byte {
	waves := map[int][]string{}
	maxWave := 0
	for _, st := range m.pipe(r) {
		waves[st.wave] = append(waves[st.wave], st.name)
		if st.wave > maxWave {
			maxWave = st.wave
		}
	}
	ordered := make([][]string, 0, maxWave+1)
	for w := 0; w <= maxWave; w++ {
		ordered = append(ordered, waves[w])
	}
	b, _ := json.Marshal(ordered)
	return b
}

func stepSummaryJSON(steps []engine.StepState) []byte {
	type sum struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	out := make([]sum, 0, len(steps))
	for _, s := range steps {
		out = append(out, sum{s.Name, s.Status})
	}
	b, _ := json.Marshal(out)
	return b
}

// ── in-memory engine ────────────────────────────────────────────────────────

type mockEngine struct {
	data *mockData
}

func (e *mockEngine) QueryWorkflow(_ context.Context, workflowID string) (*engine.WorkflowState, error) {
	r := e.data.anyRun(workflowID) // mock uses runID as workflowID (CI or workflow run)
	if r == nil {
		return &engine.WorkflowState{WorkflowID: workflowID, RunID: workflowID, Status: "pending"}, nil
	}
	return &engine.WorkflowState{
		WorkflowID: workflowID,
		RunID:      r.id,
		Status:     e.data.effectiveStatus(r),
		Steps:      e.data.steps(r),
	}, nil
}

func (e *mockEngine) StartWorkflowWithWaves(context.Context, engine.StartWorkflowInput, [][]pipeline.Step) (string, error) {
	return "", nil
}
func (e *mockEngine) CompleteStep(context.Context, string, engine.StepResult) error { return nil }

// DeliverSignal makes gate approval/rejection actually move the run, so a demo
// click changes state (the SSE poll then reflects it within ~1s).
func (e *mockEngine) DeliverSignal(_ context.Context, workflowID, signal string, _ any) error {
	switch {
	case strings.HasPrefix(signal, "gate-reject-"):
		e.data.applyOverride(workflowID, runOverride{rejected: true})
	case strings.HasPrefix(signal, "gate-"):
		e.data.applyOverride(workflowID, runOverride{approved: true})
	}
	return nil
}

// CancelWorkflow marks the run cancelled so the cancel button has an effect.
func (e *mockEngine) CancelWorkflow(_ context.Context, workflowID string) error {
	e.data.applyOverride(workflowID, runOverride{cancelled: true})
	return nil
}
func (e *mockEngine) Close() {}

// ── in-memory log sink ──────────────────────────────────────────────────────

// mockLogSink synthesizes per-step logs that MATCH the step's derived status, so
// the logs pane is consistent with the timeline (a failed step shows an error,
// a skipped step says skipped, a running step has no completion line, etc.).
type mockLogSink struct {
	data *mockData
}

func (mockLogSink) Write(context.Context, logsink.LogRef, []logsink.LogLine) error { return nil }
func (mockLogSink) Tail(ctx context.Context, _ logsink.LogRef) (<-chan logsink.LogLine, error) {
	ch := make(chan logsink.LogLine)
	close(ch)
	return ch, nil
}

func (s mockLogSink) Read(_ context.Context, ref logsink.LogRef) ([]logsink.LogLine, error) {
	status := "succeeded"
	if s.data != nil {
		if r := s.data.run(ref.RunID); r != nil {
			for _, st := range s.data.steps(r) {
				if st.Name == ref.StepName {
					status = st.Status
					break
				}
			}
		}
	}

	var lines []string
	switch status {
	case "pending":
		lines = nil // not started — no logs yet
	case "skipped":
		lines = []string{"[" + ref.StepName + "] skipped — an upstream step did not succeed"}
	case "waiting":
		lines = []string{"[" + ref.StepName + "] ⏸ awaiting manual approval before continuing"}
	case "cancelled":
		lines = []string{
			"$ flint run " + ref.StepName,
			"[" + ref.StepName + "] starting…",
			"[" + ref.StepName + "] ⚠ run cancelled by user — aborting",
		}
	case "running":
		lines = []string{
			"$ flint run " + ref.StepName,
			"[" + ref.StepName + "] resolving dependencies…",
			"[" + ref.StepName + "] compiling…",
			"[" + ref.StepName + "] still running…",
		}
	case "failed":
		lines = []string{
			"$ flint run " + ref.StepName,
			"[" + ref.StepName + "] resolving dependencies…",
			"[" + ref.StepName + "] running tests…",
			"  ✗ expected status 200 but got 500",
			"  2 passing, 1 failing",
			"npm ERR! Tests failed with exit code 1",
			"✗ " + ref.StepName + " failed (exit code 1)",
		}
	default: // succeeded
		lines = []string{
			"$ flint run " + ref.StepName,
			"[" + ref.StepName + "] resolving dependencies…",
			"[" + ref.StepName + "] running…",
			"[" + ref.StepName + "] done in 1.2s",
			"✓ " + ref.StepName + " complete",
		}
	}

	now := time.Now()
	out := make([]logsink.LogLine, 0, len(lines))
	for i, l := range lines {
		stream := "stdout"
		if strings.Contains(l, "ERR") || strings.HasPrefix(l, "  ✗") || strings.Contains(l, "failed") {
			stream = "stderr"
		}
		out = append(out, logsink.LogLine{Timestamp: now.Add(time.Duration(i) * time.Second), Stream: stream, Content: l})
	}
	return out, nil
}

// ── in-memory run creator ───────────────────────────────────────────────────

type mockRunCreator struct{}

func (mockRunCreator) HandleWebhook(context.Context, http.Header, []byte, string) ([]string, error) {
	return []string{"mock-run-new"}, nil
}
func (mockRunCreator) TriggerManual(context.Context, string, string, string, string) (string, string, error) {
	return "mock-run-new", "mock-run-new", nil
}
func (mockRunCreator) Rerun(context.Context, string) (string, string, error) {
	return "mock-run-new", "mock-run-new", nil
}
