# Flint — Claude Code Guidelines

## Project

Flint is a Kubernetes-native CI platform built on a custom Postgres-backed workflow engine (`internal/engine/`). MIT licensed, OSS.

## Build & Test

```bash
task build-all          # Build all 5 binaries to bin/
task build BIN=server   # Build a single binary
task test               # Run all unit tests
task lint               # Run golangci-lint
task fmt                # Format code
task vet                # Run go vet
task check              # Run all checks (fmt, vet, lint, test)
```

Requires [Task](https://taskfile.dev/): `go install github.com/go-task/task/v3/cmd/task@latest`

## Conventions

- Go 1.26, module path: `github.com/NerdMeNot/flint`
- `internal/` for Flint implementation details, `pkg/` for public extension points
- Interfaces only where implementations genuinely vary (forge, logsink, auth providers)
- Tests use `testify/assert` + `testify/require`, table-driven where appropriate
- Error handling via `internal/flinterr` — typed errors with `ErrorKind` classification
- No hexagonal architecture — direct implementations, no ports/adapters pattern

## Package Layout

- `cmd/` — Binary entry points (server, worker, agent, controller, syncd, flint CLI)
- `internal/engine/` — Postgres-backed workflow engine (DAG advance/dispatch/loop, timers, outbox)
- `internal/dbkit/` — pgx connection pool + goose migrations
- `internal/db/` — sqlc-generated queries + models (`sqlc.yaml` at repo root)
- `internal/flinterr/` — Shared error types + Clock interface
- `internal/auth/` — OIDC/SAML/JWT/RBAC (thin layer over go-oidc + crewjam/saml)
- `internal/secret/` — Envelope encryption
- `internal/runner/` — RunnerPool resolution
- `internal/config/` — Viper config loading
- `pkg/forge/` — ForgeProvider interface + implementations (GitHub first)
- `pkg/pipeline/` — YAML parser, validator, DAG resolver, expression evaluator
- `pkg/logsink/` — LogSink interface + implementations (filesystem, S3)
