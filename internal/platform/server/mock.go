package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

const mockOrgID = "org-mock"

// ── canned dataset ──────────────────────────────────────────────────────────

type mockStep struct {
	name string
	wave int
	gate bool
}

// A small library of pipeline shapes so step/waterfall/DAG views vary across
// runs. Per-run step statuses are derived from the run's state (and elapsed time
// for live runs); each run references a shape by key.
var pipelines = map[string][]mockStep{
	"deploy": {
		{"install-deps", 0, false}, {"lint", 1, false}, {"unit-tests", 1, false},
		{"integration-tests", 1, false}, {"build", 2, false}, {"deploy-staging", 3, false},
		{"smoke-tests", 4, false}, {"approve-production", 5, true}, {"deploy-prod", 6, false},
	},
	"library": {
		{"install-deps", 0, false}, {"lint", 1, false}, {"unit-tests", 1, false},
		{"typecheck", 1, false}, {"build", 2, false}, {"publish", 3, false},
	},
	"preview": {
		{"install-deps", 0, false}, {"lint", 1, false}, {"unit-tests", 1, false},
		{"build", 2, false}, {"deploy-preview", 3, false},
	},
	"infra": {
		{"tf-init", 0, false}, {"tf-fmt", 1, false}, {"tf-validate", 1, false},
		{"tf-plan", 2, false}, {"approve-apply", 3, true}, {"tf-apply", 4, false},
	},
	"mobile": {
		{"install-deps", 0, false}, {"lint", 1, false}, {"unit-tests", 1, false},
		{"build-ios", 2, false}, {"build-android", 2, false}, {"e2e", 3, false},
		{"upload-testflight", 4, false},
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
	status                                            string // base; live runs progress
	startedAgo                                        time.Duration
	durationMs                                        int32
	live                                              bool
	gate                                              bool   // parked on an approval gate
	pipe                                              string // pipeline shape key
	failStep                                          string // for failed runs: which step failed
}

type mockData struct {
	boot     time.Time
	projects []mockProject
	runs     []mockRun
}

func newMockData() *mockData {
	projects := []mockProject{
		{id: "proj-search", name: "Product Search", repo: "github.com/acme/product-search", workspace: "platform", colour: "#22d3ee", pipe: "deploy", tags: []string{"domain:search", "tier:1", "lang:go"}, recent: []string{"running", "succeeded", "succeeded", "failed", "succeeded"}, total: 142, succeeded: 128},
		{id: "proj-checkout", name: "Checkout", repo: "github.com/acme/checkout", workspace: "platform", colour: "#a78bfa", pipe: "deploy", tags: []string{"domain:payments", "tier:1", "compliance:pci"}, recent: []string{"waiting", "succeeded", "succeeded", "succeeded"}, total: 217, succeeded: 209},
		{id: "proj-payments", name: "Payments API", repo: "github.com/acme/payments", workspace: "platform", colour: "#f59e0b", pipe: "deploy", tags: []string{"domain:payments", "tier:1", "compliance:pci", "lang:go"}, recent: []string{"failed", "succeeded", "succeeded", "failed"}, total: 188, succeeded: 170},
		{id: "proj-auth", name: "Auth Service", repo: "github.com/acme/auth", workspace: "platform", colour: "#60a5fa", pipe: "deploy", tags: []string{"domain:identity", "tier:1", "lang:go"}, recent: []string{"succeeded", "succeeded", "succeeded", "succeeded"}, total: 96, succeeded: 94},
		{id: "proj-edge", name: "Edge Gateway", repo: "github.com/acme/edge-gateway", workspace: "platform", colour: "#2dd4bf", pipe: "deploy", tags: []string{"domain:platform", "tier:1", "lang:rust"}, recent: []string{"succeeded", "succeeded", "failed", "succeeded"}, total: 74, succeeded: 68},
		{id: "proj-billing", name: "Billing", repo: "github.com/acme/billing", workspace: "platform", colour: "#fb7185", pipe: "deploy", tags: []string{"domain:payments", "tier:2", "compliance:pci"}, recent: []string{"succeeded", "succeeded", "succeeded", "running"}, total: 53, succeeded: 50},
		{id: "proj-notifications", name: "Notifications", repo: "github.com/acme/notifications", workspace: "platform", colour: "#c084fc", pipe: "library", tags: []string{"domain:messaging", "tier:2", "lang:ts"}, recent: []string{"succeeded", "succeeded", "succeeded"}, total: 41, succeeded: 40},
		{id: "proj-web", name: "Web App", repo: "github.com/acme/web-app", workspace: "frontend", colour: "#f472b6", pipe: "preview", tags: []string{"lang:ts", "tier:1"}, recent: []string{"failed", "failed", "succeeded", "failed"}, total: 130, succeeded: 88},
		{id: "proj-dashboard", name: "Analytics Dashboard", repo: "github.com/acme/dashboard", workspace: "frontend", colour: "#a3e635", pipe: "preview", tags: []string{"lang:ts", "domain:analytics"}, recent: []string{"succeeded", "succeeded", "failed", "succeeded"}, total: 77, succeeded: 70},
		{id: "proj-design", name: "Design System", repo: "github.com/acme/design-system", workspace: "frontend", colour: "#e879f9", pipe: "library", tags: []string{"lang:ts", "tier:3"}, recent: []string{"succeeded", "succeeded", "succeeded", "succeeded"}, total: 64, succeeded: 63},
		{id: "proj-docs", name: "Docs Site", repo: "github.com/acme/docs", workspace: "frontend", colour: "#38bdf8", pipe: "preview", tags: []string{"lang:ts", "tier:3"}, recent: []string{"succeeded", "failed", "succeeded"}, total: 39, succeeded: 35},
		{id: "proj-data", name: "Data Pipeline", repo: "github.com/acme/data-pipeline", workspace: "data", colour: "#34d399", pipe: "deploy", tags: []string{"domain:analytics", "tier:2", "lang:python"}, recent: []string{"running", "succeeded", "succeeded"}, total: 121, succeeded: 112},
		{id: "proj-etl", name: "ETL Jobs", repo: "github.com/acme/etl", workspace: "data", colour: "#4ade80", pipe: "deploy", tags: []string{"domain:analytics", "tier:2", "lang:python"}, recent: []string{"succeeded", "failed", "succeeded", "succeeded"}, total: 88, succeeded: 79},
		{id: "proj-ml-train", name: "Model Training", repo: "github.com/acme/ml-training", workspace: "ml", colour: "#facc15", pipe: "deploy", tags: []string{"domain:ml", "tier:2", "lang:python"}, recent: []string{"running", "succeeded", "failed"}, total: 57, succeeded: 44},
		{id: "proj-ml-serve", name: "Model Serving", repo: "github.com/acme/ml-serving", workspace: "ml", colour: "#fbbf24", pipe: "deploy", tags: []string{"domain:ml", "tier:1", "lang:python"}, recent: []string{"succeeded", "succeeded", "succeeded"}, total: 49, succeeded: 47},
		{id: "proj-infra", name: "Infrastructure", repo: "github.com/acme/infrastructure", workspace: "infra", colour: "#818cf8", pipe: "infra", tags: []string{"domain:platform", "tier:1"}, recent: []string{"waiting", "succeeded", "succeeded", "succeeded"}, total: 203, succeeded: 195},
		{id: "proj-mobile-ios", name: "iOS App", repo: "github.com/acme/ios-app", workspace: "mobile", colour: "#f87171", pipe: "mobile", tags: []string{"lang:swift", "tier:1"}, recent: []string{"running", "succeeded", "failed", "succeeded"}, total: 68, succeeded: 58},
		{id: "proj-mobile-android", name: "Android App", repo: "github.com/acme/android-app", workspace: "mobile", colour: "#4ade80", pipe: "mobile", tags: []string{"lang:kotlin", "tier:1"}, recent: []string{"succeeded", "succeeded", "failed"}, total: 62, succeeded: 54},
	}
	return &mockData{
		boot:     time.Now(),
		projects: projects,
		runs:     buildMockRuns(projects),
	}
}

// buildMockRuns produces a handful of hand-authored "active" runs (live, gated,
// recently failed) followed by a long tail of historical runs, so lists feel
// lived-in and the run-detail/timeline views have plenty to show.
func buildMockRuns(projects []mockProject) []mockRun {
	runs := []mockRun{
		{id: "mock-run-1", projectID: "proj-search", branch: "main", sha: "c9d1e33", msg: "chore: upgrade TanStack Router to v1.168", by: "carol", trigger: "push", env: "production", status: "running", startedAgo: 30 * time.Second, live: true, pipe: "deploy"},
		{id: "mock-run-2", projectID: "proj-ml-train", branch: "main", sha: "7b3f9a1", msg: "feat: switch to bf16 mixed precision", by: "frank", trigger: "push", env: "production", status: "running", startedAgo: 45 * time.Second, live: true, pipe: "deploy"},
		{id: "mock-run-3", projectID: "proj-mobile-ios", branch: "main", sha: "2c4e6a8", msg: "feat: offline mode for the cart", by: "heidi", trigger: "push", env: "staging", status: "running", startedAgo: 70 * time.Second, live: true, pipe: "mobile"},
		{id: "mock-run-4", projectID: "proj-checkout", branch: "main", sha: "a1b2c3d", msg: "feat: one-click checkout for returning users", by: "dave", trigger: "push", env: "production", status: "waiting", startedAgo: 12 * time.Minute, gate: true, pipe: "deploy"},
		{id: "mock-run-5", projectID: "proj-infra", branch: "main", sha: "e5f6a7b", msg: "chore: scale node pool to 6 replicas", by: "oscar", trigger: "push", env: "production", status: "waiting", startedAgo: 25 * time.Minute, gate: true, pipe: "infra"},
		{id: "mock-run-6", projectID: "proj-web", branch: "feat/new-nav", sha: "f4e5d6c", msg: "feat: redesign primary navigation", by: "erin", trigger: "pull_request", status: "failed", startedAgo: 22 * time.Minute, durationMs: 90000, failStep: "unit-tests", pipe: "preview"},
		{id: "mock-run-7", projectID: "proj-payments", branch: "main", sha: "9a8b7c6", msg: "fix: retry Stripe webhook on 5xx", by: "grace", trigger: "push", env: "production", status: "failed", startedAgo: 40 * time.Minute, durationMs: 132000, failStep: "integration-tests", pipe: "deploy"},
		{id: "mock-run-8", projectID: "proj-billing", branch: "main", sha: "3d5f7b9", msg: "feat: usage-based invoicing", by: "ivan", trigger: "push", env: "production", status: "succeeded", startedAgo: 55 * time.Minute, durationMs: 204000, pipe: "deploy"},
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
	for i := 0; i < 48; i++ {
		seq++
		p := projects[i%len(projects)]
		status := "succeeded"
		if i%5 == 2 {
			status = "failed"
		}
		trigger := triggers[i%len(triggers)]
		dur := int32(96000 + (i%9)*22000)
		if status == "failed" {
			dur = int32(48000 + (i%5)*18000)
		}
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
			durationMs: dur,
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
	if trigger == "pull_request" || pipe == "library" || pipe == "preview" {
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

// liveProgress is how many pipeline steps a live run has cleared (one every ~6s
// since boot), so the run advances on its own and the SSE/poll shows it.
func (m *mockData) liveProgress() int {
	return int(time.Since(m.boot) / (6 * time.Second))
}

// pipe returns the run's pipeline shape (falling back to the deploy shape).
func (m *mockData) pipe(r *mockRun) []mockStep {
	if p, ok := pipelines[r.pipe]; ok {
		return p
	}
	return pipelines["deploy"]
}

// effectiveStatus resolves a run's current status, advancing the live run to
// succeeded once it has cleared the pipeline.
func (m *mockData) effectiveStatus(r *mockRun) string {
	if r.live && m.liveProgress() >= len(m.pipe(r)) {
		return "succeeded"
	}
	return r.status
}

// steps builds the per-step state for a run from its status (and elapsed time
// for the live run).
func (m *mockData) steps(r *mockRun) []engine.StepState {
	pipe := m.pipe(r)
	started := time.Now().Add(-r.startedAgo)
	out := make([]engine.StepState, 0, len(pipe))
	prog := m.liveProgress()

	gateWave := -1
	for _, st := range pipe {
		if st.gate {
			gateWave = st.wave
		}
	}
	failIdx := len(pipe) / 2 // default: fail around the middle
	if r.status == "failed" {
		for i, st := range pipe {
			if st.name == r.failStep {
				failIdx = i
				break
			}
		}
	}

	for i, st := range pipe {
		s := engine.StepState{
			Name: st.name, Wave: st.wave, Attempt: 1, MaxAttempts: 1,
			ExecType: execTypeOf(st.gate),
		}
		switch {
		case r.gate && gateWave >= 0:
			// Parked on the gate: everything up to it done, gate waiting, rest pending.
			switch {
			case st.gate:
				s.Status = "waiting"
			case st.wave < gateWave:
				s.Status = "succeeded"
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
		default:
			s.Status = "pending"
		}
		if s.Status != "pending" && s.Status != "skipped" {
			sched := started.Add(time.Duration(i) * 25 * time.Second)
			start := sched.Add(2 * time.Second)
			s.ScheduledAt = &sched
			s.StartedAt = &start
			if s.Status == "succeeded" || s.Status == "failed" {
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

func execTypeOf(gate bool) string {
	if gate {
		return "gate"
	}
	return "run"
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

func (m *mockData) stepSummaryJSON(r *mockRun) []byte {
	type sum struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	steps := m.steps(r)
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
	r := e.data.run(workflowID) // mock uses runID as workflowID
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
func (e *mockEngine) DeliverSignal(context.Context, string, string, any) error      { return nil }
func (e *mockEngine) CancelWorkflow(context.Context, string) error                  { return nil }
func (e *mockEngine) Close()                                                        {}

// ── in-memory log sink ──────────────────────────────────────────────────────

type mockLogSink struct{}

func (mockLogSink) Write(context.Context, logsink.LogRef, []logsink.LogLine) error { return nil }
func (mockLogSink) Tail(ctx context.Context, _ logsink.LogRef) (<-chan logsink.LogLine, error) {
	ch := make(chan logsink.LogLine)
	close(ch)
	return ch, nil
}
func (mockLogSink) Read(_ context.Context, ref logsink.LogRef) ([]logsink.LogLine, error) {
	now := time.Now()
	lines := []string{
		"$ flint run " + ref.StepName,
		"[" + ref.StepName + "] resolving dependencies…",
		"[" + ref.StepName + "] done in 1.2s",
		"✓ " + ref.StepName + " complete",
	}
	out := make([]logsink.LogLine, 0, len(lines))
	for i, l := range lines {
		out = append(out, logsink.LogLine{Timestamp: now.Add(time.Duration(i) * time.Second), Stream: "stdout", Content: l})
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
