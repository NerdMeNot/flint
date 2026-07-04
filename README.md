# Flint

**Kubernetes-native CI that's tiny to run.** The whole control plane is **Postgres + one binary** — no message broker, no etcd-backed CRDs, no controller fleet, no Temporal. Steps run as Kubernetes Jobs; runners scale to zero between runs.

![Flint run detail](docs/images/workflows-detail.png)

## Why Flint

- **Tiny footprint.** One Deployment + your Postgres is a complete install. Durable state (workflows, steps, timers, signals, webhook outbox) lives in Postgres with `FOR UPDATE`/`SKIP LOCKED` claims and LISTEN/NOTIFY wakeups — the engine idles at a handful of queries per minute.
- **Fails at compile time, not four minutes into a run.** Pipelines are validated against your actual runner pools before anything is scheduled: unknown pool names, impossible CPU/memory asks, and GPU requests on non-GPU pools fail immediately with a named fix. Expressions are type-checked against the exact context the engine evaluates at runtime, with did-you-mean suggestions for typos.
- **No CRDs, no controllers.** Runner pools are rows in Postgres managed via the API/UI. Managed pools render Karpenter NodePools for your GitOps repo (`flint runner render`) — nodes scale to zero between runs.
- **A job is a pod.** Each job gets its own image, disk, and runner pool; steps inside a job share the pod. Files cross jobs via explicit `artifacts:` (object storage), values via `outputs:` — no hidden shared state.
- **One platform, multiple products.** CI and Workflows (generic cron/HTTP DAGs) share the same engine, auth (OIDC/SAML/SCIM/MFA), RBAC, and runner pools. One login, one deployment.

## Try it in 5 minutes (no Kubernetes needed)

```bash
git clone https://github.com/NerdMeNot/flint && cd flint
docker compose up
```

That starts Postgres and the single-binary control plane with **simulated step execution** — the real engine drives real runs through queue → dispatch → logs → completion without needing a cluster. Open http://localhost:8080.

> Using Podman? `podman compose up` works the same.

## Install on Kubernetes

```bash
helm install flint ./helm/flint \
  --set database.host=your-postgres \
  --set database.password=... \
  --set server.baseUrl=https://ci.example.com
```

Defaults to the single-binary topology (API + webhooks + embedded engine loop in one Deployment). For an evaluation cluster without external Postgres, add `--set postgresql.internal=true`. See [`helm/flint/values.yaml`](helm/flint/values.yaml) for split workers, S3 storage, and ingress.

First-run admin credentials are printed in the server log. Then: connect your forge (Settings → Connections), create a project, push a pipeline.

## A pipeline

```yaml
# .flint/ci.yaml
# yaml-language-server: $schema=https://ci.example.com/schemas/pipeline.json
image: golang:1.26
triggers:
  push: { branches: [main] }
  pull_request: { branches: [main], paths: ["src/**", "go.mod"] }
concurrency: { group: ci-${{ git.branch }}, cancelInProgress: true }

jobs:
  build:
    steps:
      - use: checkout
      - run: go build ./...
      - name: version
        run: echo "version=$(git describe --tags)" >> "$FLINT_OUTPUT"
    outputs: { version: "${{ steps.outputs.version }}" }
    artifacts: [bin/]

  test:
    needs: [build]
    matrix: { go: ["1.25", "1.26"] }
    image: golang:${{ matrix.go }}
    steps:
      - use: checkout
      - run: go test ./...

  deploy:
    needs: [build, test]
    if: ${{ git.branch == "main" }}
    runner: prod-deployers   # pod identity: IRSA / Workload Identity
    steps:
      - run: ./deploy.sh ${{ needs.build.outputs.version }}
```

Jobs are pods; `needs` edges carry artifacts and outputs; matrix jobs fan out with real value interpolation in `image`, `env`, `run`, and `if`. Gates (`gate: { approvers: [team:release] }`), per-step retries, caches, and service containers are all first-class — see [the pipeline spec](docs/design/pipeline-spec.md).

Validate locally before pushing:

```bash
flint validate            # rich errors with did-you-mean + line context
flint validate --env prod # preview which jobs run for an environment
```

Broken pipelines never vanish silently: parse and compile failures post a failing commit status with the reason.

## Architecture (short version)

```
products (ci, workflows)  →  compile YAML → DAG waves
engine (internal/core)    →  Postgres-backed durable executor
                             FOR UPDATE advancement · SKIP LOCKED claims
                             LISTEN/NOTIFY wakeups · transactional outbox
platform                  →  API (Hertz) · auth (OIDC/SAML/SCIM) · Casbin RBAC
execution                 →  one K8s Job per job-pod: agent sidecar (logs,
                             artifacts, cache, completion) + your image
```

| | Flint | Tekton | Argo Workflows | GitLab |
|---|---|---|---|---|
| Control plane | 1 binary + Postgres | controllers + webhooks + CRDs | controller (+ server) + CRDs | Rails + Sidekiq + Redis + Gitaly |
| Pipeline state | Postgres | etcd (CRDs) | etcd (or offload) | Postgres + Redis |
| Runners at rest | zero | zero | zero | standing runners |
| Compile-time pool/resource validation | ✅ | ❌ | ❌ | ❌ |

## Docs

- [Pipeline spec](docs/design/pipeline-spec.md) · [JSON Schema](schemas/flint-pipeline.schema.json)
- [Execution model](docs/design/execution-model.md) · [Runner pools](docs/design/runner-pools.md)
- [Auth & SSO](docs/auth.md) · [RBAC](docs/design/rbac.md) · [API keys](docs/design/api-keys.md)
- [Architecture & roadmap](docs/design/flint-family-architecture.md)
- [Contributing](CONTRIBUTING.md)

## License

[MIT](LICENSE)
