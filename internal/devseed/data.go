package devseed

import "fmt"

type workspaceSpec struct{ name, slug, desc string }

var workspaces = []workspaceSpec{
	{"Platform", "platform", "Core platform services"},
	{"Frontend", "frontend", "Web and mobile clients"},
	{"Data", "data", "Pipelines and warehousing"},
	{"ML", "ml", "Model training and serving"},
	{"Infra", "infra", "Terraform and cluster tooling"},
}

// tagKeySpec seeds the tag registry (the curated key:value vocabulary the UI
// offers for filtering and tagging). Matches the key:value tags on projects.
type tagKeySpec struct {
	key, label, color string
	values            []string
}

var tagKeys = []tagKeySpec{
	{"domain", "Domain", "#6366f1", []string{"payments", "identity", "orders", "messaging", "data", "ml", "infra"}},
	{"tier", "Tier", "#f59e0b", []string{"1", "2", "3"}},
	{"lang", "Language", "#10b981", []string{"go", "ts", "py"}},
	{"platform", "Platform", "#06b6d4", []string{"ios"}},
	{"compliance", "Compliance", "#ef4444", []string{"pci", "soc2"}},
}

// runnerPoolSpec seeds the machine-pool catalog so dev-sim has a default pool +
// variety for the Runners UI and pipeline validation. All seed pools are static
// (BYO machines); the policy fields show the economics knobs in the editor.
type runnerPoolSpec struct {
	name, description, cpu, memory, arch string
	gpuVendor, gpuModel                  string
	gpuCount                             int32
	capacityType, objective              string // "" → on_demand / balanced
	minWarm                              int32
	idleTTLSeconds                       int32 // 0 → default 900
}

var runnerPools = []runnerPoolSpec{
	{name: "standard", description: "General-purpose CI", cpu: "2", memory: "4Gi", arch: "amd64", minWarm: 1},
	{name: "cpu-large", description: "Compute-heavy builds/tests", cpu: "8", memory: "16Gi", arch: "amd64", objective: "latency"},
	{name: "arm64", description: "ARM builds", cpu: "4", memory: "8Gi", arch: "arm64"},
	{name: "gpu", description: "GPU training/inference", cpu: "8", memory: "32Gi", arch: "amd64", gpuVendor: "nvidia", gpuModel: "a100", gpuCount: 1},
	{name: "spot-batch", description: "Cost-optimized batch pool (spot, zero standing infra)", cpu: "4", memory: "8Gi", arch: "amd64", capacityType: "spot", objective: "cost", idleTTLSeconds: 300},
}

type environmentSpec struct{ name, slug string }

var environments = []environmentSpec{
	{"Production", "production"},
	{"Staging", "staging"},
	{"Dev", "dev"},
}

type projectSpec struct {
	name, repo, workspace, colour string
	tags                          []string
	archetype                     string // drives which runs are generated
}

var projects = []projectSpec{
	{"Payments API", "acme/payments-api", "platform", "#6366f1", []string{"domain:payments", "tier:1", "lang:go"}, "deploy"},
	{"Auth Service", "acme/auth-service", "platform", "#8b5cf6", []string{"domain:identity", "tier:1", "lang:go"}, "deploy"},
	{"Orders API", "acme/orders-api", "platform", "#ec4899", []string{"domain:orders", "tier:2", "lang:go"}, "library"},
	{"Notifications", "acme/notifications", "platform", "#f43f5e", []string{"domain:messaging", "tier:3"}, "flaky"},
	{"Web App", "acme/web-app", "frontend", "#06b6d4", []string{"lang:ts", "tier:1"}, "deploy"},
	{"Design System", "acme/design-system", "frontend", "#14b8a6", []string{"lang:ts", "library"}, "library"},
	{"Mobile iOS", "acme/mobile-ios", "frontend", "#10b981", []string{"platform:ios"}, "library"},
	{"Admin Console", "acme/admin-console", "frontend", "#84cc16", []string{"lang:ts", "tier:2"}, "failing"},
	{"ETL Pipeline", "acme/etl-pipeline", "data", "#eab308", []string{"domain:data", "lang:py"}, "library"},
	{"Data Warehouse", "acme/data-warehouse", "data", "#f59e0b", []string{"domain:data", "tier:1"}, "deploy"},
	{"Events Ingest", "acme/events-ingest", "data", "#f97316", []string{"domain:data", "lang:go"}, "flaky"},
	{"Recommender", "acme/recommender", "ml", "#ef4444", []string{"domain:ml", "lang:py", "gpu"}, "library"},
	{"Model Registry", "acme/model-registry", "ml", "#a855f7", []string{"domain:ml", "tier:2"}, "library"},
	{"Feature Store", "acme/feature-store", "ml", "#d946ef", []string{"domain:ml", "lang:py"}, "failing"},
	{"Terraform Modules", "acme/terraform-modules", "infra", "#0ea5e9", []string{"domain:infra", "compliance:soc2"}, "deploy"},
	{"Cluster Bootstrap", "acme/cluster-bootstrap", "infra", "#3b82f6", []string{"domain:infra", "tier:1"}, "deploy"},
	{"Observability", "acme/observability", "infra", "#6366f1", []string{"domain:infra"}, "library"},
	{"Secrets Operator", "acme/secrets-operator", "infra", "#8b5cf6", []string{"domain:infra", "compliance:pci"}, "flaky"},
	{"Billing Worker", "acme/billing-worker", "platform", "#ec4899", []string{"domain:payments", "tier:2", "lang:go"}, "library"},
	{"Sandbox", "acme/sandbox", "platform", "#64748b", []string{"experimental"}, "empty"},
}

// seedUser is a demo local user. All share AdminPassword so they're loginable.
type seedUser struct{ email, name, role string }

var seedUsers = []seedUser{
	{"carol@flint.dev", "Carol Diaz", "developer"},
	{"dave@flint.dev", "Dave Okafor", "developer"},
	{"erin@flint.dev", "Erin Park", "viewer"},
	{"frank@flint.dev", "Frank Lee", "admin"},
	{"grace@flint.dev", "Grace Huang", "viewer"},
}

// seedTeam is an internal team with a role grant and member emails.
type seedTeam struct {
	name, slug, role string
	members          []string
}

var seedTeams = []seedTeam{
	{"Platform Team", "platform-team", "developer", []string{"carol@flint.dev", "dave@flint.dev"}},
	{"Frontend Team", "frontend-team", "developer", []string{"erin@flint.dev"}},
	{"Data Team", "data-team", "viewer", []string{"frank@flint.dev", "grace@flint.dev"}},
}

type runSpec struct {
	kind         string
	pipeline     string
	workflowFile string
	trigger      string
	branch       string
	sha          string
	message      string
	author       string
	environment  string
}

var (
	authors  = []string{"carol", "dave", "erin", "frank", "grace", "heidi", "ivan", "judy"}
	// Deliberately includes names long enough to overflow a card footer. Real
	// teams generate branches like these from ticket templates, and a fixture
	// that only ever emits "main" lets truncation bugs ship — the projects grid
	// wrapped mid-word for exactly that reason.
	branches = []string{
		"main", "main", "main",
		"feat/checkout-v2", "fix/race-condition", "chore/bump-deps",
		"renovate/all-minor-patch-dependencies",
		"feature/PLAT-4821-migrate-billing-to-new-ledger",
		"revert-2291-hotfix/disable-legacy-webhook-retry-path",
	}
	messages = []string{
		"Fix nil deref in webhook handler",
		"Add retry to flaky upstream call",
		"Bump dependencies to latest patch",
		"Refactor checkout flow for clarity",
		"Tighten validation on request body",
		"Improve log structure for tracing",
	}
)

// runsFor builds the spread of runs for a project based on its archetype. The
// "empty" archetype yields none (exercises the empty-state UI).
func runsFor(p projectSpec) []runSpec {
	if p.archetype == "empty" {
		return nil
	}

	var out []runSpec
	// A tail of completed history (varied success) for scale/pagination/filtering.
	for i := 0; i < 4; i++ {
		out = append(out, mkRun(i, "history", libraryPipeline("2s"), "push", ""))
	}

	switch p.archetype {
	case "deploy":
		// Parked at an approval gate (gates scenario) and a fresh in-flight run.
		out = append(out, mkRun(10, "gated", deployPipeline(), "push", "production"))
		out = append(out, mkRun(11, "live", longPipeline(), "manual", "staging"))
	case "failing":
		// Failure cascade: a failing job skips everything downstream.
		out = append(out, mkRun(12, "failed", failingPipeline(), "push", ""))
		out = append(out, mkRun(13, "live", longPipeline(), "push", ""))
	case "flaky":
		// Flaky-then-pass: exercises the engine retry path.
		out = append(out, mkRun(14, "flaky", flakyPipeline(), "push", ""))
		out = append(out, mkRun(15, "live", longPipeline(), "pull_request", ""))
	case "library":
		out = append(out, mkRun(16, "live", longPipeline(), "push", ""))
	}
	return out
}

func mkRun(i int, kind, pipeline, trigger, env string) runSpec {
	return runSpec{
		kind:     kind,
		pipeline: pipeline,
		// Must match the pipeline filename the project page filters runs by (the
		// stub forge's GetDirectory key), else the project Runs tab shows nothing.
		workflowFile: "ci.yaml",
		trigger:      trigger,
		branch:       branches[i%len(branches)],
		sha:          randHex(20),
		message:      messages[i%len(messages)],
		author:       authors[i%len(authors)],
		environment:  env,
	}
}

// --- Pipeline archetypes -------------------------------------------------
//
// Each job's `env:` carries sim hints (SIM_DURATION / SIM_OUTCOME /
// SIM_FLAKY_UNTIL) that the sim executor reads to script timing and outcome.

const baseTriggers = `triggers:
  push:
    branches: [main]
  pull_request: {}
  manual: {}
`

// libraryPipeline: install → test → build, all succeed. dur sets the test phase.
func libraryPipeline(dur string) string {
	return baseTriggers + fmt.Sprintf(`image: node:20
jobs:
  install:
    steps:
      - name: install deps
        run: npm ci
  test:
    needs: [install]
    env:
      SIM_DURATION: %q
    steps:
      - name: run tests
        run: npm test
  build:
    needs: [test]
    steps:
      - name: build
        run: npm run build
`, dur)
}

// deployPipeline: build → approve(gate) → deploy. Parks at the gate.
func deployPipeline() string {
	return baseTriggers + `image: golang:1.26
environments: [production, staging]
jobs:
  build:
    steps:
      - name: build
        run: go build ./...
  approve-prod:
    needs: [build]
    gate:
      approvers: [admin@flint.dev]
      minApprovals: 1
  deploy:
    needs: [approve-prod]
    steps:
      - name: deploy
        run: ./deploy.sh
`
}

// failingPipeline: test fails → build is skipped (real cascade).
func failingPipeline() string {
	return baseTriggers + `image: node:20
jobs:
  install:
    steps:
      - name: install
        run: npm ci
  test:
    needs: [install]
    env:
      SIM_OUTCOME: fail
    steps:
      - name: run tests
        run: npm test
  build:
    needs: [test]
    steps:
      - name: build
        run: npm run build
`
}

// flakyPipeline: test fails on attempt 1, passes on attempt 2 (retry path).
func flakyPipeline() string {
	return baseTriggers + `image: node:20
jobs:
  test:
    env:
      SIM_FLAKY_UNTIL: "2"
    steps:
      - name: run tests
        run: npm test
        retry:
          attempts: 2
  build:
    needs: [test]
    steps:
      - name: build
        run: npm run build
`
}

// longPipeline: a long-running job so the run stays "running" for live/SSE demos.
func longPipeline() string {
	return baseTriggers + `image: node:20
jobs:
  install:
    steps:
      - name: install
        run: npm ci
  integration:
    needs: [install]
    env:
      SIM_DURATION: 5m
    steps:
      - name: integration tests
        run: npm run test:integration
`
}
