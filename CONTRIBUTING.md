# Contributing to Flint

## Prerequisites

- Go 1.26
- [Task](https://taskfile.dev/): `go install github.com/go-task/task/v3/cmd/task@latest`
- Podman (container runtime for the local Postgres) — Docker works too with minor Taskfile tweaks
- [bun](https://bun.sh/) for the web frontend (never npm/npx)
- `mprocs` for the sim stack: `brew install mprocs`
- Optional: a local Kubernetes cluster (kind/minikube/Docker Desktop) for real step execution

## Development stacks

```bash
task dev-sim     # RECOMMENDED: full stack with SIMULATED step execution.
                 # Postgres + seeded demo data + server + worker + web under
                 # mprocs, hot reload via air. No Kubernetes needed.
                 # Log in: admin@flint.dev / flintdev123

task dev-local   # Postgres + server + worker; steps dispatch to your local
                 # Kubernetes cluster as real Jobs.
```

## Build & test

```bash
task build-all       # all binaries to bin/
task test            # unit tests (integration tests spin a throwaway Postgres
                     # via initdb; set FLINT_TEST_DSN to reuse a running one)
task check           # fmt + vet + lint + test — run before pushing
task generate        # proto (buf) + sqlc after changing .sql/.proto files
```

## Repository layout

- `internal/core/engine/` — the Postgres-backed durable engine (the scary code:
  small, reviewed changes only)
- `internal/products/ci|workflows/` — product layers; products never import
  each other
- `internal/platform/` — API server, auth, config
- `pkg/` — public extension points (pipeline language, forge providers, log
  sinks, artifact/cache/checkout)
- `web/` — TanStack Start frontend (bun)

## Conventions

- Postgres access via sqlc: add named queries under
  `internal/core/db/queries/*.sql`, then `task generate`. No hand-written SQL
  in handlers.
- Errors through `internal/core/flinterr` with `ErrorKind` classification.
- Tests: `testify/assert` + `require`, table-driven where it fits.
- Interfaces only where implementations genuinely vary.
- **Parse it → run it, or refuse it loudly.** A pipeline field the engine does
  not enforce must be a validation error, never a silent no-op.

## Pull requests

Keep engine changes small and independently revertable. Every PR: `task check`
green, plus a regression test for any bug fix.
