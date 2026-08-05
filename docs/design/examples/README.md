# Pipeline examples

Two kinds of example live here.

## Runnable (`runnable/`)

These compile on the **current** engine with no external module registry and no
roadmap-only features. They are enforced by
`internal/products/ci/examples_runnable_test.go` — every file under `runnable/`
must `Parse` and `Compile` clean for the plain run and each declared environment,
so these examples can never silently rot.

- `failure-handling.ci.yaml` — job-level `when:` (onFailure/always) plus the
  `success()`/`failure()` status functions in `if:`.
- `parallel-quality-gate.ci.yaml` — a parallel quality gate over a shared build,
  with artifacts along `needs`, a service sidecar, cache, and `continueOnError`.
- `matrix-build.ci.yaml` — a matrix job fanning out one pod per combination.

Point `flint validate` / the CLI at any of these to see them accepted as-is.

## Design sketches (top level)

`payments-api.ci.yaml`, `orders-api.ci.yaml`, and `release.yaml` are the
aspirational, full-surface examples referenced by the spec. They exercise
features that are **not all built yet** and reference a **versioned module
registry** (`go-service-ci@2.3.1`, `trivy-scan@1.6.0`, …) that resolves at run
time on the server, not offline. They are documentation of the intended surface,
not runnable today. As the roadmap items land (containerized action modules,
matrix `failFast`), the relevant pieces graduate into `runnable/` and pick up the
compile guarantee above.
