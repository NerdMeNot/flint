# Flint — Claude Code Guidelines

## Project

Flint is a Kubernetes-native CI platform built on a custom Postgres-backed workflow engine (`internal/core/engine/`). Multiple products (CI, Workflows; Load Testing planned) share that engine. MIT licensed, OSS.

## Build & Test

```bash
task build-all          # Build all 6 binaries to bin/
task build BIN=server   # Build a single binary
task build-web          # Build the web frontend (bun + Vite)
task test               # Run all unit tests
task check              # Run all checks (fmt, vet, lint, test)
task dev-local          # Full local stack: Postgres (Podman) + server + worker;
                        # steps dispatch to your local Kubernetes cluster
```

Requires [Task](https://taskfile.dev/): `go install github.com/go-task/task/v3/cmd/task@latest`.
Also: `task generate` (proto + sqlc + CRDs), `task migrate-up/down/status`.

## Conventions

- Go 1.26, module path: `github.com/NerdMeNot/flint`
- `internal/` for Flint implementation details, `pkg/` for public extension points
- Interfaces only where implementations genuinely vary (forge, logsink, wsfs, auth providers, StepExecutor)
- Tests use `testify/assert` + `testify/require`, table-driven where appropriate
- Error handling via `internal/core/flinterr` — typed errors with `ErrorKind` classification
- No hexagonal architecture — direct implementations, no ports/adapters pattern
- Web frontend uses bun (never npm/npx)
- Local container runtime is Podman, not Docker

## Architecture

Three layers; dependencies point downward only (products → core, platform → products via DI):

- **Engine** (`internal/core/engine/`) — product-agnostic. Single entry point
  `StartWorkflowWithWaves(input, waves)`: products compile their own DAGs into waves of
  `pipeline.Step`. Steps run only as Kubernetes Jobs (k8s `StepExecutor`) or in-process
  (http executor) — there is no local/docker executor. Durable state in Postgres
  (workflows/steps/timers/signals/outbox), `FOR UPDATE` per-workflow advancement,
  `SKIP LOCKED` claims, LISTEN/NOTIFY wakeups, transactional outbox for webhooks.
- **Products** (`internal/products/`) — `ci/` owns pipeline YAML, triggers, module
  resolution, matrix expansion, compile-to-waves; `workflows/` is the thinner second
  product (definitions, http steps, cron scheduler).
- **Platform** (`internal/platform/`) — HTTP API (Hertz), auth (OIDC/SAML/local/device
  flow), Casbin RBAC with workspace/environment scoping, config (Viper).

## Package Layout

- `cmd/` — Binary entry points (server, worker, agent, controller, syncd, flint CLI/TUI)
- `internal/core/engine/` — Workflow engine (advance/dispatch/loop, timers, signals, outbox, executors)
- `internal/core/db/` — sqlc-generated queries + models (`sqlc.yaml` at repo root)
- `internal/core/dbkit/` — pgx connection pool + goose migrations
- `internal/core/agent/` — Per-job agent: init container (checkout, secrets, cache, artifacts) + native sidecar (logs, completion)
- `internal/core/wsagent/` — Per-run gRPC workspace server pod
- `internal/core/controller/`, `internal/core/crd/` — RunnerPool/Project CRD reconciler
- `internal/core/runner/` — RunnerPool registry/resolution
- `internal/core/worker/` — K8s Job informer (completion fallback)
- `internal/core/secretstore/` — Envelope-encrypted secret storage
- `internal/core/flinterr/` — Shared error types + Clock interface
- `internal/core/httpx/`, `internal/core/observe/` — Error envelope, logging/tracing/metrics
- `internal/products/ci/`, `internal/products/workflows/` — Product layers on the engine
- `internal/platform/server/` — API routes/handlers/middleware
- `internal/platform/auth/` — OIDC/SAML/JWT/RBAC (go-oidc, crewjam/saml, Casbin)
- `internal/platform/config/` — Viper config loading
- `internal/tui/` — Bubble Tea TUI for the flint CLI
- `pkg/pipeline/` — YAML parser, validator, DAG resolver, expression evaluator (engine IR: `pipeline.Step`)
- `pkg/forge/` — ForgeProvider interface + implementations (GitHub first)
- `pkg/wsfs/`, `pkg/workspace/` — Workspace filesystem (local/S3/remote gRPC) + sync
- `pkg/artifact/`, `pkg/cache/`, `pkg/checkout/` — S3 artifacts, cache, git checkout
- `pkg/logsink/` — LogSink interface + implementations (filesystem, S3)
- `pkg/secret/` — Secret refs/scopes
- `proto/`, `protogen/` — gRPC contracts (agent, workspace, health); generated via buf
- `web/` — TanStack Start frontend (React, oRPC client, Tailwind)
- `docs/design/` — Design docs (pipeline spec, family architecture/roadmap)
- `_reference/` — Vendored external repos for reference only; never edit or import
