# Competitive analysis: Buildkite (July 2026)

Research snapshot of Buildkite's offering and what it implies for Flint.
Sources: buildkite.com docs/changelog/pricing, the elastic-ci-stack-for-aws /
buildkite-agent-scaler / agent-stack-k8s / agent repos and their issue
trackers, HN threads (2016–2026), customer engineering blogs (Canva, Hasura,
Wix, Shopify), and hands-on browsing of the current product UI via public
pipelines (rails/rails, bazel).

## Why Buildkite matters to Flint

Buildkite is the closest architectural cousin: SaaS control plane + a
persistent agent daemon on customer machines, queue-targeted dispatch, warm
agents preferred. It validates Flint's substrate bet at extreme scale
(Shopify ~10k concurrent agents; Uber, Canva; OpenAI/Anthropic/Cursor as
customers). The differences are the strategy:

| | Buildkite | Flint |
|---|---|---|
| Control plane | Closed SaaS | OSS (MIT), self-hostable |
| Agent | OSS (MIT) | OSS (MIT) |
| Fleet scaling | Per-cloud glue the customer operates | Product core (fleet manager) |
| Economics | Customer's problem (or buy hosted vCPU-minutes) | Decision ledger, explainable |
| Pricing | $30/active-user/mo, 30-seat Enterprise floor | n/a (OSS) |

## The validated gap: nobody ships the fleet manager

Buildkite's official autoscaling coverage: AWS (CloudFormation Elastic CI
Stack + Lambda scaler), GCP (Terraform stack, shipped ~2025 after ~8 years of
community stopgaps), Kubernetes (agent-stack-k8s — scales pods, **not
nodes**), ECS/Fargate (officially unmaintained), Azure (docs say "contact
support"). Everyone else composes `buildkite-agent-metrics` + their own
scaler.

Customer evidence that this costs real money:

- **Canva**: cold caches cost "300+ hours of compute per build before any
  actual work began"; self-built EBS-snapshot warm boots saved ~$1.8M/yr.
- **Hasura**: found the Elastic Stack overwhelming; hand-rolled a Lambda
  scaler + secrets + artifacts.
- **Wix**: built their own concurrency layer to cut 40–60 min queues.
- Years-open issues that read as Flint's roadmap: spot→on-demand fallback
  (elastic-ci-stack #851), warm pools (#822), utilization-based scaling
  (scaler #322), warm caches on ephemeral machines (#74, since 2016).

Buildkite's answer was to *sell hosted compute* (2024, metered vCPU-minutes,
US-East only) — hedging away from BYO economics rather than automating them.
Flint automating BYO ("bring your own machines, but through the fleet
manager") is the road they didn't take.

### Prebuilt fleets (direction)

Ship named compute-provider bundles — provider + curated image + sane pool
policy — instead of Buildkite's one-CloudFormation-stack-per-queue sprawl
(pools are already DB rows in Flint):

1. **ec2-fleet** — table stakes; spot mix, Graviton, snapshot-warmed volumes.
2. **hetzner-fleet** — the happiest Buildkite users on HN run huge suites on
   cheap Hetzner boxes with zero official support; cost-obsessed,
   OSS-friendly early adopters.
3. **k8s-fleet** — k8s as just-another-compute-provider; beats
   agent-stack-k8s by requesting *nodes* through the same economics engine
   instead of punting to Karpenter.
4. **gce-fleet / azure-fleet** — cheap credibility (Azure has literally
   nothing official).
5. **fargate-fleet** — true scale-to-zero, one task per step; their
   equivalent (`buildkite/on-demand`) is abandoned.
6. **static-fleet** — fixed BYO machines (the refurb-Mac-mini rack), plus the
   existing localdev provider.
7. Later: **macos-fleet** (EC2 Mac/MacStadium) — Buildkite gates hosted Macs
   behind Pro/Enterprise with no custom images.

### Fleet mechanics worth stealing (battle-tested lessons)

- **Scale-in by agent self-termination**: central scale-in kills running jobs
  (ASG lifecycle races), so their instances drain themselves — idle timeout →
  agent exits → `ExecStopPost` terminates the instance with an atomic
  capacity decrement. Plus: dangling-instance reaping (machine alive, agent
  OOM-killed) and an availability-threshold guard for instances that boot but
  never register. Maps to fleet reconciliation.
- **Spot handling, safe by default**: their retry-on-agent-loss is opt-in
  YAML distinguishing `exit_status: -1` (lost) from `signal_reason:
  agent_stop` (graceful drain) — users begged for years. Flint owns the
  machine lifecycle: a step killed by fleet drain/spot reclaim should requeue
  automatically and be ledgered, no YAML.
- **Dispatch latency**: they went ~10s HTTP polling → streaming push
  (ConnectRPC, "under 1s acceptance", GA Apr 2026). Flint's LISTEN/NOTIFY +
  gRPC should make sub-second claim-to-start a measured headline number.
- **Warmth is dispatch-level**: their queues order agents by most-recent
  successful job (warm caches). Flint's same-run affinity is a scheduler
  guarantee — keep and advertise.

## Product surface: table stakes Flint is measured against

Highest value-to-effort first:

- **Log groups** — `--- ` collapsed / `+++ ` expanded / `~~~ ` muted markers,
  live per-group durations. Their renderer (`terminal-to-html`) is MIT and
  actively maintained.
- **Annotations** — `buildkite-agent annotate`: build-scoped markdown panels
  (style, context-keyed upsert, priority). Beloved, small engine surface.
- **Per-step timeline** — dispatch lifecycle with ms deltas (created →
  scheduled → assigned agent → accepted → started → finished). Flint's
  `step_assignments` + `engine_events` + placement ledger can show what they
  can't: *why this machine*.
- **Retry rules** with granular causes (exit codes, signals, signal reasons);
  retries create new jobs, history immutable.
- **Dynamic pipelines** — steps emit more YAML mid-build (500 jobs/upload,
  4,000 jobs/build); their most-praised power feature. Flint's compile-to-
  waves is static at trigger; an append-waves-at-runtime API deserves its own
  design doc.
- Also: `if_changed` path filtering (monorepos), matrix `adjustments`,
  concurrency groups/gates, priority, block/input steps with typed form
  fields, build meta-data KV, signed pipelines (JWS, KMS), inline HTML
  artifact rendering, job metrics (CPU/mem/IO — they only have it on hosted;
  Flint owns every machine).

**Do not copy**: plugins executing before step-level `if`; unscoped GraphQL
tokens patched by "Portals"; group steps that can't nest; one-queue-per-stack.

## Positioning window

- Closed control plane is their oldest structural complaint (HN 2017→);
  Semaphore open-sourced in 2025 as a survival move.
- $30/active-user ("active" includes anyone who triggers a build) + 30-seat
  Enterprise floor is shedding mid-market users (HN Sept 2025: "60 users =
  $1,800/mo — the same as the hardware").
- Squeezed from below by runner-replacement vendors (Depot, Blacksmith,
  Namespace); retreating upmarket ("Scale-Out Delivery Platform").
- Founder-CEO left Mar 2025; new C-suite Aug 2025; no funding since 2022.

## UI notes (from public pipelines; screenshots in session scratchpad)

Build page: six tabs — Summary (+ "Step uploads"), List (step tree, queue
chips), Table, Canvas (DAG, follow mode), Waterfall, Annotations. Job drawer:
Annotations / Log / Artifacts / Timeline / Environment tabs, dockable
side/bottom/center. Log pane: dark terminal, collapsible groups with live
durations, timestamp toggle, search, download. Engineered for 500–10,000-step
builds. Their 2016–2019 production frontend is readable at
github.com/buildkite/frontend (archived); `terminal-to-html` and the emoji
set are the maintained OSS pieces. Live references: buildkite.com/rails/rails,
buildkite.com/bazel.

## Prototype landed with this doc (web + sim)

- `web/src/lib/ansi.ts` + `log-model.ts` (+ tests): ANSI SGR rendering with
  cross-line state, `\r` overwrite handling, and group parsing (`---`/`+++`/
  `~~~`, plus `::group::` for ported scripts) with per-group durations from
  sink timestamps.
- `web/src/components/pipeline/log-view.tsx`: shared log surface (groups,
  timestamp gutter, search) used by the step Log panel and the Output view.
- `runs.stepLogs` now passes structured `{timestamp, stream, content}` lines
  through (the backend always returned them; the old router joined to a
  string).
- **Live logs**: `use-step-log-stream.ts` consumes the pre-existing—but
  previously unused—SSE endpoint `GET /runs/:id/steps/:step/logs/stream`,
  with a 3s polling fallback while running.
- **Step Timeline tab** in the log panel: engine events for the step with
  deltas + placement (machine link, instance type, spot/on-demand, $/hr,
  queue wait). All real data.
- **Annotations (mock-first)**: `runs.annotations` oRPC procedure hits
  `GET /runs/:id/annotations`; until that endpoint exists, demo deployments
  fall back to representative samples (live deployments fall back to none).
- Sim executor now emits grouped, ANSI-colored output across the simulated
  duration so `task dev-sim` exercises all of the above.

### Backend reconciliation list

1. `GET /runs/:id/annotations` + storage + an agent emit (`annotate` in the
   steps driver: style, context upsert, markdown body ≤ 1 MiB).
2. Document `--- `/`+++ `/`~~~ ` as Flint's log-group convention for step
   authors (agent already passes raw output through).
3. Consider `hasMore`/pagination on step logs for very large logs (UI caps at
   what the sink returns today).
