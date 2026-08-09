# Flint — Claude Code Guidelines

## Project

Flint is a CI platform running steps on raw machines (VMs/bare metal) via a
persistent `flint-agent` daemon, built on a custom Postgres-backed workflow
engine (`internal/core/engine/`). Cloud providers integrate through a small
compute-provider interface; a fleet manager makes legible economics decisions
(warm vs boot, spot vs on-demand) recorded in a decision ledger. Multiple
products (CI, Workflows; Load Testing planned) share the engine. MIT licensed,
OSS. **Not Kubernetes-based** — the k8s substrate was removed in July 2026;
don't reintroduce k8s.io dependencies (k8s may return later only as
just-another-compute-provider).

## Build & Test

```bash
task build-all          # Build both binaries to bin/ (flint, flint-agent)
task build BIN=flint    # Build a single binary
task build-web          # Build the web frontend (bun + Vite)
task test               # Run all unit tests
task check              # Run all checks (fmt, vet, lint, test)
task dev-local          # Postgres (Podman) + `flint server --mode all`
task dev-sim            # Local sim stack (Postgres + seed + server + dispatch + web)
                        # under mprocs (brew install mprocs): REAL engine, simulated
                        # step execution (no machines). Log in admin@flint.dev / flintdev123
```

Requires [Task](https://taskfile.dev/): `go install github.com/go-task/task/v3/cmd/task@latest`.
Also: `task generate` (proto + sqlc), `task migrate-up/down/status`.

## Conventions

- Go 1.26, module path: `github.com/NerdMeNot/flint`
- `internal/` for Flint implementation details, `pkg/` for public extension points
- Interfaces only where implementations genuinely vary (forge, logsink, wsfs, auth providers, StepExecutor, compute providers)
- Tests use `testify/assert` + `testify/require`, table-driven where appropriate
- Error handling: plain `fmt.Errorf("%w")` wrapping is the norm and is fine.
  `internal/core/flinterr` (typed errors with `ErrorKind`) is **not** used
  repo-wide — it is confined to `runner`, `secretstore`, `pkg/secret`, and
  `auth/session`, where a caller has to branch on the error's *kind*. Reach for
  it when the classification changes behaviour (retry vs fail, which HTTP status
  to map to); don't convert existing `fmt.Errorf` sites just for consistency.
  HTTP handlers pick their status explicitly via `internal/core/httpx`
- No hexagonal architecture — direct implementations, no ports/adapters pattern
- Web frontend uses bun (never npm/npx)
- Local container runtime is Podman, not Docker
- Pre-live: fold schema changes into the baseline migration 001 (no ALTER migrations)
- sqlc for all queries — no hand-written pgx SQL in handlers

## Architecture

Three layers; dependencies point downward only (products → core, platform → products via DI):

- **Engine** (`internal/core/engine/`) — product-agnostic. Single entry point
  `StartWorkflowWithWaves(input, waves)`: products compile their own DAGs into waves of
  `pipeline.Step`. Container steps (run/use/steps) dispatch to machines via
  `step_assignments` rows claimed by flint-agent daemons; http steps run
  in-process; `FLINT_EXECUTOR=sim` simulates container steps for dev. Durable
  state in Postgres (workflows/steps/timers/signals/outbox), `FOR UPDATE`
  per-workflow advancement, `SKIP LOCKED` claims, LISTEN/NOTIFY wakeups,
  transactional outbox for webhooks. Transitions go through a chokepoint with
  an append-only event log (engine_events, deliberately no FK).
- **Fleet** (`internal/core/fleet/`; machines/machine_pools/compute_providers/
  fleet_decisions tables) — machine lifecycle state machine (requested →
  provisioning → idle ⇄ busy → draining → terminated, + failed/lost), scheduler
  (bin-pack + same-run warmth affinity), provisioner (Quote → rank by pool
  objective → Create), idle scale-down, provider reconciliation. Every
  provision/terminate/drain decision is ledgered with inputs, chosen offer,
  alternatives, and backfilled outcome — economics must be explainable.
- **Products** (`internal/products/`) — `ci/` owns pipeline YAML, triggers, module
  resolution, matrix expansion, compile-to-waves; `workflows/` is the thinner second
  product (definitions, http steps, cron scheduler).
- **Platform** (`internal/platform/`) — HTTP API (Hertz), auth (OIDC/SAML/local/device
  flow), Casbin RBAC with workspace/environment scoping, config (Viper).

## Binaries (two)

- `flint` (`cmd/flint/`) — CLI/TUI client (default) + `flint server` control
  plane: `--mode all` (single-binary install: API + webhooks + embedded
  dispatch + IdP sync) | `api` | `webhook` | `dispatch` (scale-out engine
  loop) | `sync`. Composition root in `internal/boot/`.
- `flint-agent` (`cmd/flint-agent/`) — per-machine daemon (linux): registers
  with a pool join/bootstrap token, heartbeats, claims step assignments,
  executes container steps. Machines need nothing pre-installed.

## Package Layout

- `cmd/` — Binary entry points (flint, flint-agent)
- `internal/boot/` — Composition root for `flint server` (server/dispatch/sync wiring)
- `internal/core/engine/` — Workflow engine (advance/transition/loop, timers, signals, outbox, executors)
- `internal/core/fleet/` — Machine lifecycle, scheduler, provisioner, decision ledger
- `internal/core/db/` — sqlc-generated queries + models (`sqlc.yaml` at repo root)
- `internal/core/dbkit/` — pgx connection pool + goose migrations (001 is the baseline)
- `internal/core/runner/` — Machine pool registry + resolution. **No CRDs/controllers**:
  pools are DB/API-managed (`POST/PATCH/DELETE /runners`), loaded into the registry
  from the DB (`runner.LoadAll`). Pipelines pick a pool via `runner:`; pools carry
  the compute provider, machine shape, and economics policy (minWarm/maxMachines/
  idleTTL/capacityType/objective + per-branch overrides).
- `internal/core/agent/` — Step-execution helpers shared with the agent (steps driver, emits, hashFiles)
- `internal/core/secretstore/` — Envelope-encrypted secret storage
- `internal/core/flinterr/` — Shared error types + Clock interface
- `internal/core/httpx/`, `internal/core/observe/` — Error envelope, logging/tracing/metrics
- `internal/version/` — Build metadata (single ldflags path)
- `internal/products/ci/`, `internal/products/workflows/` — Product layers on the engine
- `internal/platform/server/` — API routes/handlers/middleware
- `internal/platform/auth/` — OIDC/SAML/JWT/RBAC (go-oidc, crewjam/saml, Casbin)
- `internal/platform/config/` — Viper config loading (`engine:` section configures the loop)
- `internal/tui/` — Bubble Tea TUI for the flint CLI
- `pkg/pipeline/` — YAML parser, validator, DAG resolver, expression evaluator (engine IR: `pipeline.Step`)
- `pkg/compute/` — Compute provider interface (Quote/Create/Destroy/List) + implementations
- `pkg/units/` — Quantity parsing ("500m", "8Gi") — replaces k8s resource.Quantity
- `pkg/forge/` — ForgeProvider interface + implementations (GitHub first)
- `pkg/wsfs/`, `pkg/workspace/` — Workspace filesystem (local/S3) + sync
- `pkg/artifact/`, `pkg/cache/`, `pkg/checkout/` — S3 artifacts, cache, git checkout
- `pkg/logsink/` — LogSink interface + implementations (filesystem, S3, HTTP shipper)
- `pkg/secret/` — Secret refs/scopes
- `proto/`, `protogen/` — gRPC contracts (agent, health); generated via buf
- `web/` — TanStack Start frontend (React, oRPC client, Tailwind)
- `docs/design/` — Design docs (pipeline spec, family architecture/roadmap)
- `_reference/` — Vendored external repos for reference only; never edit or import
