package server

import (
	"context"
	"encoding/json"
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

// One pipeline shape reused across runs; per-run statuses are derived from the
// run's state (and elapsed time for the live run).
var mockPipeline = []mockStep{
	{"install-deps", 0, false},
	{"lint", 1, false},
	{"unit-tests", 1, false},
	{"integration-tests", 1, false},
	{"build", 2, false},
	{"deploy-staging", 3, false},
	{"smoke-tests", 4, false},
	{"approve-production", 5, true},
	{"deploy-prod", 6, false},
}

type mockProject struct {
	id, name, repo, workspace, colour string
	tags                              []string
	recent                            []string // recent run statuses, newest first
	total, succeeded                  int
}

type mockRun struct {
	id, projectID, branch, sha, msg, by, trigger, env string
	status                                            string // base; live runs progress
	startedAgo                                        time.Duration
	durationMs                                        int32
	live                                              bool
	gate                                              bool // parked on the production gate
}

type mockData struct {
	boot     time.Time
	projects []mockProject
	runs     []mockRun
}

func newMockData() *mockData {
	return &mockData{
		boot: time.Now(),
		projects: []mockProject{
			{"proj-search", "Product Search", "github.com/acme/product-search", "platform", "#22d3ee", []string{"domain:search", "tier:1"}, []string{"running", "succeeded", "succeeded", "failed", "succeeded"}, 42, 38},
			{"proj-checkout", "Checkout", "github.com/acme/checkout", "platform", "#a78bfa", []string{"domain:payments", "tier:1"}, []string{"succeeded", "succeeded", "succeeded", "succeeded"}, 67, 65},
			{"proj-web", "Web App", "github.com/acme/web-app", "frontend", "#f472b6", []string{"lang:ts"}, []string{"failed", "failed", "succeeded", "failed"}, 30, 18},
			{"proj-data", "Data Pipeline", "github.com/acme/data-pipeline", "data", "#34d399", []string{"domain:analytics"}, []string{"waiting", "succeeded", "succeeded"}, 21, 19},
		},
		runs: []mockRun{
			{"mock-run-1", "proj-search", "main", "c9d1e33", "chore: upgrade TanStack Router to v1.168", "carol", "push", "production", "running", 30 * time.Second, 0, true, false},
			{"mock-run-2", "proj-checkout", "main", "a1b2c3d", "fix: handle expired payment tokens", "dave", "push", "staging", "succeeded", 8 * time.Minute, 252000, false, false},
			{"mock-run-3", "proj-web", "feat/new-nav", "f4e5d6c", "feat: redesign primary navigation", "erin", "pull_request", "", "failed", 22 * time.Minute, 90000, false, false},
			{"mock-run-4", "proj-search", "main", "9a8b7c6", "perf: cache search suggestions", "carol", "push", "production", "succeeded", 1 * time.Hour, 198000, false, false},
			{"mock-run-5", "proj-data", "main", "1f2e3d4", "feat: nightly aggregation v2", "frank", "schedule", "production", "waiting", 15 * time.Minute, 0, false, true},
			{"mock-run-6", "proj-checkout", "fix/tax", "5d6e7f8", "fix: tax rounding off-by-one", "dave", "pull_request", "", "succeeded", 2 * time.Hour, 141000, false, false},
		},
	}
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

// effectiveStatus resolves a run's current status, advancing the live run to
// succeeded once it has cleared the pipeline.
func (m *mockData) effectiveStatus(r *mockRun) string {
	if r.live && m.liveProgress() >= len(mockPipeline) {
		return "succeeded"
	}
	return r.status
}

// steps builds the per-step state for a run from its status (and elapsed time
// for the live run).
func (m *mockData) steps(r *mockRun) []engine.StepState {
	started := time.Now().Add(-r.startedAgo)
	out := make([]engine.StepState, 0, len(mockPipeline))
	prog := m.liveProgress()
	for i, st := range mockPipeline {
		s := engine.StepState{
			Name: st.name, Wave: st.wave, Attempt: 1, MaxAttempts: 1,
			ExecType: execTypeOf(st.gate),
		}
		switch {
		case r.gate:
			// Parked on the gate: everything up to it done, gate waiting, rest pending.
			if st.gate {
				s.Status = "waiting"
			} else if st.wave < 5 {
				s.Status = "succeeded"
			} else {
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
			if st.name == "build" {
				s.Status = "failed"
			} else if st.wave < 2 {
				s.Status = "succeeded"
			} else {
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

func (m *mockData) dagWaves() []byte {
	waves := map[int][]string{}
	maxWave := 0
	for _, st := range mockPipeline {
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
