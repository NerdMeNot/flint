# Pipeline YAML Specification

## Overview

Flint pipelines are declared in YAML files stored in the repository (`.flint/` directory by default). A project can have multiple pipeline files — e.g. `ci.yaml` for pull request checks and `deploy.yaml` for deployments.

This is a living document. It defines the full pipeline YAML schema, environment model, trigger system, expression syntax, and validation rules.

## Minimal Example

```yaml
# .flint/ci.yaml — plain CI, no environment
triggers:
  pull_request: [main]

steps:
  - name: test
    run: make test
```

```yaml
# .flint/deploy.yaml — CD pipeline with environments
triggers:
  push:
    branches: [main]
    environments: [staging]
  promotion:
    from: staging
    environments: [production]

steps:
  - name: approve
    gate:
      approvers: [role:release-manager]
    environments: [production]
  - name: test
    run: make test
  - name: security-scan
    environments: [production]
    run: make security-scan
  - name: deploy
    dependsOn: [approve, test]
    run: make deploy
```

---

## Pipeline Types

A pipeline is either **environment-aware** or **plain**, determined by whether it references environments anywhere.

### Plain Pipeline (CI)

No environment references anywhere in the file. Runs without an environment context.

- `$FLINT_ENVIRONMENT` is empty
- Only global variables/secrets are available
- No gates fire
- No environment policies apply

Use for: PR checks, linting, tests, build verification.

### Environment-Aware Pipeline (CD)

References environments in any of: top-level `environments`, trigger `environments`, step `environments`, or environment-scoped secrets/variables.

- Every run **must** target an environment
- Manual triggers show an environment picker in the UI
- Automated triggers must have `environments` specified
- `$FLINT_ENVIRONMENT` is set to the target environment's slug
- Environment-scoped secrets/variables resolve for the target environment

Use for: deployments, releases, environment-specific workflows.

### Detection Rules

Flint determines a pipeline is environment-aware if **any** of these are true:

- Top-level `environments` key is present
- Any trigger has an `environments` field
- Any step has an `environments` field
- Any expression references `${{ secrets.* }}` where the secret is environment-scoped
- Any expression references `${{ env.* }}` (environment variables)
- Any expression references `$FLINT_ENVIRONMENT`

This is validated statically by `flint validate`. If a pipeline is environment-aware but a trigger has no `environments` field, validation fails.

---

## Top-Level Schema

```yaml
# Optional: restrict which environments this pipeline can target.
# Default: all environments (if pipeline is environment-aware).
environments: [string]

# Required: at least one trigger.
triggers:
  <trigger-type>: <trigger-config>

# Required: at least one step.
steps:
  - <step-definition>
```

### `environments` (optional)

Restricts which environments this pipeline can target. If omitted, the pipeline can target any environment (or none, if it's a plain pipeline).

```yaml
environments: [staging, production]
```

Validation: if specified, all trigger-level and step-level environment references must be subsets of this list.

---

## Triggers

Triggers define when and how a pipeline run is created. A pipeline must have at least one trigger. Flint supports seven trigger types.

### 1. Push

Runs when commits are pushed to matching branches.

```yaml
triggers:
  push:
    branches: [main, "release/*"]       # required: branch patterns (glob syntax)
    paths: ["src/**", "Makefile"]       # optional: only trigger on changes to these paths
    environments: [staging]             # optional: target these environments
```

| Field | Required | Description |
|-------|----------|-------------|
| `branches` | yes | Branch name patterns. Glob syntax (`*`, `**`). |
| `paths` | no | Path patterns. If set, only trigger when matching files change. |
| `environments` | no | Target environments for the run. |

**Example — deploy to staging on main, ignore docs changes:**
```yaml
triggers:
  push:
    branches: [main]
    paths: ["src/**", "cmd/**", "internal/**"]
    environments: [staging]
```

### 2. Pull Request

Runs when a pull request is opened, updated, or synchronized against matching base branches.

```yaml
triggers:
  pull_request:
    branches: [main, "release/*"]       # required: target branch patterns
    paths: ["src/**"]                   # optional: path filter
```

| Field | Required | Description |
|-------|----------|-------------|
| `branches` | yes | Target (base) branch patterns. |
| `paths` | no | Path patterns. If set, only trigger when matching files change. |

Pull request triggers **cannot** have `environments` — they are always plain CI runs. PRs test code, they don't deploy.

**Example — run tests on PRs to main:**
```yaml
triggers:
  pull_request:
    branches: [main]
```

### 3. Manual

Allows runs to be triggered by a user from the UI or CLI.

```yaml
triggers:
  manual:
    environments: [staging, production] # optional: restrict environment picker
    inputs:                             # optional: user-provided form inputs
      - name: version
        type: string
        description: "Version tag to deploy"
        required: true
      - name: dry_run
        type: boolean
        description: "Simulate without deploying"
        default: "false"
```

| Field | Required | Description |
|-------|----------|-------------|
| `environments` | no | Restricts the environment picker in the UI. |
| `inputs` | no | Form fields shown in the UI. Values available as `${{ inputs.NAME }}`. |

**Input fields:**

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Input identifier. Used in expressions. |
| `type` | yes | `string`, `boolean`, or `choice`. |
| `description` | no | Help text shown in the UI. |
| `required` | no | Whether the field must be filled. Default: `false`. |
| `default` | no | Default value. **Required** if the pipeline also has automated triggers (see Trigger Compatibility). |
| `options` | no | List of allowed values (for `type: choice`). |

**Example — deploy with version picker:**
```yaml
triggers:
  manual:
    environments: [production]
    inputs:
      - name: version
        type: string
        description: "Git tag or SHA to deploy"
        required: true
      - name: region
        type: choice
        description: "Target region"
        options: [us-east-1, eu-west-1, ap-southeast-1]
        default: us-east-1
```

### 4. Schedule

Runs on a cron schedule.

```yaml
triggers:
  schedule:
    cron: "0 2 * * 1-5"                # required: cron expression (UTC)
    environments: [staging]             # optional: target environment
```

| Field | Required | Description |
|-------|----------|-------------|
| `cron` | yes | Standard cron expression. All times UTC. |
| `environments` | no | Target environments for the run. |

The run uses the latest commit on the project's default branch.

**Example — nightly security scan in staging:**
```yaml
triggers:
  schedule:
    cron: "0 3 * * *"
    environments: [staging]
```

### 5. Tag

Runs when a Git tag matching the pattern is pushed.

```yaml
triggers:
  tag:
    patterns: ["v*", "release-*"]       # required: tag name patterns (glob syntax)
    environments: [production]          # optional: target environment
```

| Field | Required | Description |
|-------|----------|-------------|
| `patterns` | yes | Tag name patterns. Glob syntax. |
| `environments` | no | Target environments for the run. |

The tag value is available as `${{ tag }}` in expressions.

**Example — release to production on version tags:**
```yaml
triggers:
  tag:
    patterns: ["v*"]
    environments: [production]
```

### 6. Promotion

Runs when a previous environment run succeeds, enabling staged rollout flows.

```yaml
triggers:
  promotion:
    from: staging                       # required: source environment
    environments: [production]          # required: target environment(s)
    requireStatus: succeeded            # optional: default "succeeded"
```

| Field | Required | Description |
|-------|----------|-------------|
| `from` | yes | Source environment whose successful run triggers promotion. |
| `environments` | yes | Target environment(s) for the promoted run. |
| `requireStatus` | no | Required status of the source run. Default: `succeeded`. |

Promotion creates a new run for the **same commit** that succeeded in the source environment. The run is created immediately — use a gate step if approval is needed before deployment.

**Example — promote staging to production:**
```yaml
triggers:
  promotion:
    from: staging
    environments: [production]
```

**Example — multi-stage promotion:**
```yaml
# In a pipeline with environments: [dev, staging, production]
triggers:
  push:
    branches: [main]
    environments: [dev]

  promotion:
    from: dev
    environments: [staging]

  promotion:
    from: staging
    environments: [production]
```

Note: multiple promotion triggers are allowed if they have different `from` environments.

### 7. Webhook

Runs when an external HTTP request hits the pipeline's webhook endpoint.

```yaml
triggers:
  webhook:
    secret: ${{ secrets.WEBHOOK_SECRET }}   # optional: HMAC validation secret
    environments: [staging]                 # optional: target environment
```

| Field | Required | Description |
|-------|----------|-------------|
| `secret` | no | Shared secret for HMAC signature validation of incoming requests. |
| `environments` | no | Target environments for the run. |

Flint generates a unique webhook URL per pipeline: `https://flint.example.com/hooks/{projectId}/{pipelineSlug}`. The request body is available in expressions as `${{ webhook.body }}` and headers as `${{ webhook.headers }}`.

Use for: ChatOps (`/deploy` from Slack), external CI systems, custom integrations, third-party event sources.

**Example — Slack ChatOps deploy:**
```yaml
triggers:
  webhook:
    secret: ${{ secrets.SLACK_SIGNING_SECRET }}
    environments: [staging, production]
```

**Example — trigger from external system:**
```yaml
triggers:
  webhook:
    secret: ${{ secrets.WEBHOOK_SECRET }}
```

Trigger via:
```bash
curl -X POST https://flint.example.com/hooks/proj-123/deploy \
  -H "X-Flint-Signature: sha256=..." \
  -H "Content-Type: application/json" \
  -d '{"ref": "main", "environment": "staging"}'
```

---

### Trigger Summary

| Trigger | Creates env run? | Automatic? | Use case |
|---------|-----------------|------------|----------|
| `push` | optional | yes | Deploy on merge, CI on push |
| `pull_request` | never | yes | PR checks, test suites |
| `manual` | optional | no | On-demand deploys, ad-hoc runs |
| `schedule` | optional | yes | Nightly tests, periodic scans |
| `tag` | optional | yes | Release workflows |
| `promotion` | always | yes | Staged rollouts (staging → prod) |
| `webhook` | optional | yes | ChatOps, external integrations |

---

### Trigger Compatibility

Multiple triggers can coexist in a single pipeline. Most combinations are valid, with a few constraints.

**Compatibility matrix:**

| Trigger | push | pull_request | manual | schedule | tag | promotion | webhook |
|---------|------|-------------|--------|----------|-----|-----------|---------|
| **push** | — | yes | yes | yes | yes | yes | yes |
| **pull_request** | yes | — | yes | yes | yes | no* | yes |
| **manual** | yes | yes | — | yes | yes | yes | yes |
| **schedule** | yes | yes | yes | — | yes | yes | yes |
| **tag** | yes | yes | yes | yes | — | yes | yes |
| **promotion** | yes | no* | yes | yes | yes | yes** | yes |
| **webhook** | yes | yes | yes | yes | yes | yes | — |

\* `pull_request` + `promotion` is technically valid but unusual — PRs are CI, promotions are CD. Not prohibited, just uncommon.

\** Multiple `promotion` triggers are valid only if they have different `from` environments (e.g. dev→staging and staging→production in the same pipeline).

**Rules:**

1. **`pull_request` cannot have `environments`**. It's always a plain CI run. Combining it with environment-aware triggers is fine — the PR trigger creates plain runs, the other triggers create environment runs.

2. **`manual` with `inputs` + automated triggers**: if a pipeline has both `manual` (with inputs) and any automated trigger (push, schedule, tag, promotion, webhook), then all inputs **must have a `default` value**. Automated triggers can't prompt for input — they use the defaults.

3. **`promotion` requires `environments`**. It always creates an environment run.

4. **No duplicate triggers of the same type** except `promotion` (which can appear multiple times with different `from` values).

5. **Environment consistency**: if the pipeline has top-level `environments`, all trigger-level environment references must be subsets.

**Example — full lifecycle pipeline:**
```yaml
environments: [staging, production]

triggers:
  # CI: run tests on every PR
  pull_request:
    branches: [main]

  # CD: deploy to staging on merge
  push:
    branches: [main]
    environments: [staging]

  # CD: promote staging to production
  promotion:
    from: staging
    environments: [production]

  # Escape hatch: manual deploy to any environment
  manual:
    environments: [staging, production]

steps:
  - name: approve
    gate:
      approvers: [role:release-manager]
    environments: [production]

  - name: test
    run: make test

  - name: deploy
    dependsOn: [approve, test]
    run: make deploy
```

This pipeline handles four scenarios:
1. PR opened → plain run: test only (approve skipped, deploy skipped — no environment)
2. PR merged to main → staging run: test → deploy (approve skipped — not production)
3. Staging succeeds → production run: approve (waiting) + test in parallel → deploy after both
4. Manual trigger → user picks environment, runs accordingly

---

## Steps

A step is the unit of work in a pipeline. Each step runs in its own Kubernetes pod by default. A step has either `run:` (a shell command) or `steps:` (nested sub-steps that share a pod). Never both.

### Step Schema

```yaml
steps:
  # Command step — runs in its own pod
  - name: string                        # required, unique within pipeline
    run: string                         # shell command(s) to execute
    use: string                         # OR: step template name (mutually exclusive with run)
    with: {}                            # inputs for the step template (when using use)
    image: string                       # container image (name or preset name)
    runner: string                      # runner pool name
    shell: string                       # shell to use: sh (default), bash, python
    workingDir: string                  # working directory (default: /workspace)
    dependsOn: [string]                 # step names this depends on
    environments: [string]              # only run in these environments
    timeout: string                     # step timeout (default: 1h)
    if: string                          # conditional expression
    when: string                        # execution condition: onSuccess (default), onFailure, always
    continueOnError: boolean            # step failure doesn't fail the pipeline (default: false)
    retry:                              # retry on failure
      attempts: number                  # max retry count (default: 1 = no retry)
      delay: string                     # wait between retries (default: 0s)
    env: {}                             # environment variables
    secrets: {}                         # secret references (shorthand for env with secret values)
    inputs: []                          # artifacts from previous steps
    outputs: []                         # artifacts to pass to later steps
    services: []                        # sidecar containers
    cache:                              # dependency caching
      key: string                       # cache key (supports expressions)
      paths: [string]                   # paths to cache
    matrix:                             # run step across multiple values in parallel
      key: [values]                     # each key creates a dimension

  # Nested step — sub-steps share a single pod
  - name: string
    dependsOn: [string]
    environments: [string]
    inputs: []                          # artifacts downloaded once for all sub-steps
    steps:                              # sub-steps run sequentially in one pod
      - name: string
        run: string
        # ... same fields as command step (except runner, inputs, outputs)
```

### Command Step vs Nested Step

A step is one of two kinds:

| Kind | Has | Runs as | Use when |
|------|-----|---------|----------|
| **Command** | `run:` or `use:` | Own pod | Most steps — build, test, deploy |
| **Nested** | `steps:` | Single shared pod | Steps that need shared process state — Docker daemon, auth tokens, DB connections |

**Rule:** A step has `run:`, `use:`, or `steps:`. Exactly one. Never a combination.

Nested sub-steps:
- Run sequentially in the order listed (top to bottom)
- Share the pod's filesystem, network, and sidecar containers
- Cannot specify their own `runner` (the parent's runner applies to all)
- Cannot have their own `inputs`/`outputs` (declared on the parent)
- Can have their own `env`, `if`, `image`, `timeout`

### `name` (required)

Unique identifier within the pipeline. Used for `dependsOn` references, artifact `from` references, and display in the UI. Nested sub-step names are scoped to their parent — referenced as `parent.child` externally.

### `run` (required for command steps)

Shell command(s) to execute. Multi-line commands use YAML block scalars:

```yaml
- name: build
  run: |
    echo "Building..."
    make build
    make package
```

### `use` (alternative to `run`)

References a step template (see Step Templates section). Mutually exclusive with `run`.

```yaml
- name: ecr-login
  use: ecr-login
  with:
    registry: ${{ env.ECR_REGISTRY }}
    region: us-east-1
```

### `image` (optional)

Container image for this step. Can be a full image reference or an image preset name (see Image Presets section).

```yaml
- name: build-frontend
  image: node22                         # preset name
  run: npm ci && npm run build

- name: deploy
  image: bitnami/kubectl:1.30           # full image reference
  run: kubectl apply -f manifests/
```

If omitted, uses the runner pool's default image. When the org has `presetsOnly` enabled, only preset names are allowed.

### `runner` (optional)

Runner pool for this step. Allows different steps to use different node types, architectures, or resource allocations.

```yaml
- name: test
  runner: standard
  run: make test

- name: ml-validate
  runner: gpu-pool
  run: python validate.py
```

If omitted, uses the pipeline's default runner pool (configured at project level or globally).

### `shell` (optional)

Shell interpreter. Default: `sh`.

```yaml
- name: setup
  shell: bash
  run: |
    shopt -s globstar
    for f in **/*.go; do echo "$f"; done

- name: analyze
  shell: python
  run: |
    import json
    with open('results.json') as f:
        data = json.load(f)
    print(f"Total: {len(data)}")
```

Options: `sh`, `bash`, `python`.

### `workingDir` (optional)

Working directory for the step's `run` command. Default: `/workspace`.

```yaml
- name: build-frontend
  workingDir: /workspace/frontend
  run: npm ci && npm run build
```

### `dependsOn` (optional)

Declares execution order. Steps with no dependencies can run in parallel. Steps with dependencies wait for all listed steps to succeed.

```yaml
steps:
  - name: test
    run: make test

  - name: lint
    run: make lint

  - name: build
    dependsOn: [test, lint]
    run: make build

  - name: deploy
    dependsOn: [build]
    run: make deploy
```

### `environments` (optional)

Restricts this step to only run when the pipeline targets one of the listed environments.

```yaml
steps:
  - name: test
    run: make test

  - name: security-scan
    environments: [production]
    run: make security-scan

  - name: deploy
    dependsOn: [test]
    run: make deploy
```

When targeting staging: test → deploy (security-scan skipped).
When targeting production: test → security-scan → deploy.

Skipped steps are removed from the DAG — dependencies on skipped steps are ignored.

### `timeout` (optional)

Maximum execution time. Default: `1h`.

```yaml
- name: integration-tests
  timeout: 30m
  run: make test-integration
```

### `if` (optional)

Conditional execution. Expression must evaluate to `true` for the step to run. Evaluated before the step starts.

```yaml
- name: deploy
  if: ${{ branch == 'main' }}
  run: make deploy
```

### `when` (optional)

Controls when the step runs relative to the pipeline's status. Default: `onSuccess`.

| Value | Meaning |
|-------|---------|
| `onSuccess` | Run only if all dependencies succeeded (default) |
| `onFailure` | Run only if any previous step failed |
| `always` | Run regardless of pipeline status |

```yaml
- name: test
  run: make test

- name: deploy
  dependsOn: [test]
  run: make deploy

- name: notify-failure
  when: onFailure
  run: |
    curl -X POST ${{ env.SLACK_WEBHOOK }} \
      -d '{"text": "Pipeline failed for ${{ project.name }}"}'

- name: cleanup
  when: always
  run: make cleanup
```

### `continueOnError` (optional)

If `true`, the step's failure does not fail the pipeline. Downstream steps that depend on it still run. Default: `false`.

```yaml
- name: lint
  continueOnError: true
  run: make lint

- name: test
  run: make test

- name: build
  dependsOn: [lint, test]
  run: make build                       # runs even if lint failed
```

### `retry` (optional)

Retry the step on failure.

```yaml
- name: integration-test
  retry:
    attempts: 3                         # total attempts (including first)
    delay: 10s                          # wait between retries
  run: make test-integration
```

`attempts` defaults to 1 (no retry). `delay` defaults to `0s`.

### `env` (optional)

Environment variables injected into the step.

```yaml
- name: deploy
  env:
    CLUSTER: ${{ env.CLUSTER_URL }}
    VERSION: ${{ inputs.version }}
    COMMIT: ${{ commitSha }}
  run: deploy.sh
```

### `services` (optional)

Sidecar containers that run alongside the step. They start before the step's `run` command and are terminated after it completes. Services are accessible via their name as a hostname.

```yaml
- name: integration-test
  services:
    - name: postgres
      image: postgres:16
      env:
        POSTGRES_DB: testdb
        POSTGRES_PASSWORD: test
    - name: redis
      image: redis:7
  env:
    DATABASE_URL: postgres://postgres:test@postgres:5432/testdb
    REDIS_URL: redis://redis:6379
  run: make test-integration
```

Services are implemented as sidecar containers in the step's pod.

### `cache` (optional)

Cache directories between runs to speed up repeated operations. Caches are stored in object storage, keyed by project + key string.

```yaml
- name: install
  cache:
    key: npm-${{ hashFiles('package-lock.json') }}
    paths: [node_modules]
  run: npm ci

- name: build
  cache:
    key: go-${{ hashFiles('go.sum') }}
    paths: [/root/go/pkg/mod, /root/.cache/go-build]
  run: go build ./...
```

On cache hit: paths are restored before `run` executes. On cache miss: `run` executes, then paths are uploaded. The `hashFiles()` function hashes the listed files to generate a cache-busting key.

### `inputs` / `outputs` (optional)

Artifact passing between steps. Artifacts are stored in object storage (S3).

```yaml
- name: build
  run: make build
  outputs:
    - path: /workspace/dist

- name: deploy
  dependsOn: [build]
  inputs:
    - from: build
      path: /workspace/dist
  run: deploy.sh /workspace/dist
```

`outputs` are uploaded after the step completes. `inputs` are downloaded before the step starts.

### `matrix` (optional)

Run a step multiple times across a set of values. Each combination runs as a separate parallel pod.

```yaml
matrix:
  key: [value1, value2, ...]
```

Values are available in expressions as `${{ matrix.KEY }}`.

**Single dimension — test across versions:**

```yaml
- name: test
  matrix:
    node: [18, 20, 22]
  image: node${{ matrix.node }}
  run: npm test
```

Creates 3 parallel executions: `test (node=18)`, `test (node=20)`, `test (node=22)`.

**Multi-dimension — cartesian product of all combinations:**

```yaml
- name: build
  matrix:
    service: [api, worker, frontend]
    arch: [amd64, arm64]
  run: |
    docker build --platform linux/${{ matrix.arch }} \
      -t $REGISTRY/${{ matrix.service }}:${{ shortSha }}-${{ matrix.arch }} \
      -f services/${{ matrix.service }}/Dockerfile .
```

Creates 6 parallel executions: api/amd64, api/arm64, worker/amd64, worker/arm64, frontend/amd64, frontend/arm64.

**Skip specific combinations with `if`:**

```yaml
- name: build
  matrix:
    service: [api, worker, frontend]
    arch: [amd64, arm64]
  if: ${{ !(matrix.service == 'frontend' && matrix.arch == 'arm64') }}
  run: docker build --platform linux/${{ matrix.arch }} ...
```

5 executions — frontend/arm64 skipped. No special `exclude` syntax — use `if`, which is already a known concept.

**`dependsOn` a matrix step waits for ALL executions:**

```yaml
- name: test
  matrix:
    node: [18, 20, 22]
  run: npm test

- name: deploy
  dependsOn: [test]           # waits for all 3 to succeed
  run: make deploy
```

**Matrix with `use:`:**

```yaml
- name: deploy
  matrix:
    service: [api, worker, frontend]
  use: helm-deploy
  with:
    release: ${{ matrix.service }}
    chart: ./charts/${{ matrix.service }}
    namespace: ${{ environment }}
```

**Matrix with nested steps — each combination gets its own pod:**

```yaml
- name: push
  matrix:
    service: [api, worker, frontend]
  steps:
    - use: ecr-login
      with:
        registry: ${{ env.ECR_REGISTRY }}
    - use: docker-build-push
      with:
        dockerfile: services/${{ matrix.service }}/Dockerfile
        tag: ${{ env.ECR_REGISTRY }}/${{ matrix.service }}:${{ shortSha }}
```

3 pods. Each runs ecr-login then docker-build-push. Within each pod, nested steps share auth.

**Behavior:**
- All matrix executions run in parallel as separate pods
- If one execution fails, remaining executions are cancelled (same as any failed step)
- `dependsOn` a matrix step waits for all executions to complete
- Matrix works on command steps, `use:` steps, and nested steps

### Nested Steps

When steps need to share process state (Docker daemon, auth tokens, database connections), nest them under a parent step. Sub-steps run sequentially in a single pod.

```yaml
steps:
  - name: build-api
    image: golang122
    run: go build -o /workspace/api ./cmd/api
    outputs:
      - path: /workspace/api

  - name: build-worker
    image: golang122
    run: go build -o /workspace/worker ./cmd/worker
    outputs:
      - path: /workspace/worker

  - name: build-frontend
    image: node22
    run: cd frontend && npm ci && npm run build
    outputs:
      - path: /workspace/frontend/dist

  - name: push
    dependsOn: [build-api, build-worker, build-frontend]
    inputs:
      - from: build-api
        path: /workspace/api
      - from: build-worker
        path: /workspace/worker
      - from: build-frontend
        path: /workspace/frontend/dist
    steps:
      - use: ecr-login
        with:
          registry: ${{ env.ECR_REGISTRY }}
      - name: push-api
        use: docker-build-push
        with:
          dockerfile: Dockerfile.api
          tag: ${{ env.ECR_REGISTRY }}/api:${{ shortSha }}
      - name: push-worker
        use: docker-build-push
        with:
          dockerfile: Dockerfile.worker
          tag: ${{ env.ECR_REGISTRY }}/worker:${{ shortSha }}
      - name: push-frontend
        use: docker-build-push
        with:
          dockerfile: Dockerfile.frontend
          tag: ${{ env.ECR_REGISTRY }}/frontend:${{ shortSha }}

  - name: deploy
    dependsOn: [push]
    run: kubectl apply -f manifests/
```

Why nested works here:
- ECR login runs once — all push sub-steps share the auth token
- `dependsOn` and `inputs` are declared once on the parent
- Ordering of sub-steps is implicit (top to bottom)
- The grouping is visually clear from indentation

---

## Gates

Gates are approval checkpoints declared as steps. A gate step has no `run` command — it pauses the pipeline until the required approvals are received. It appears in the UI as a pending approval with the run details (commit, branch, environment) visible to the approver.

```yaml
steps:
  - name: approve-deploy
    gate:
      approvers: [role:release-manager]
      minApprovals: 1                   # default: 1
    environments: [production]          # only gates production runs

  - name: deploy
    dependsOn: [approve-deploy]
    run: make deploy
```

Gates are step-level only. This means:
- The run is created immediately (visible in the UI, auditable)
- Pre-gate steps (tests, scans) can run in parallel while waiting for approval
- The approver can see the full run context before deciding
- The same gate step can be environment-conditional (e.g. only gate production, skip in staging)

### Approver Syntax

Approvers can be roles, teams, or individual users:

- `role:slug` — any user with this role
- `team:slug` — any member of this team
- `user@email.com` — specific individual

All approver types can be mixed freely. Any approver in the list can satisfy an approval slot.

```yaml
# Single approver — anyone with the role
gate:
  approvers: [role:release-manager]

# Mixed — role, team, or specific person
gate:
  approvers: [role:release-manager, "team:security", "alice@acme.dev"]
  minApprovals: 1       # any one of them is enough

# Stricter — require 2 approvals from the pool
gate:
  approvers: ["team:security", "alice@acme.dev", "bob@acme.dev"]
  minApprovals: 2       # 2 out of the 3 must approve
```

`minApprovals` defaults to 1. The same person cannot approve twice — each approval must come from a different user.

### Gate with Parallel Pre-work

Gates don't block steps that don't depend on them. This allows tests to run while waiting for approval:

```yaml
steps:
  - name: approve
    gate:
      approvers: [role:release-manager]
    environments: [production]

  - name: test
    run: make test

  - name: security-scan
    environments: [production]
    run: make security-scan

  - name: deploy
    dependsOn: [approve, test, security-scan]
    run: make deploy
```

In production: `approve` (waiting) and `test` + `security-scan` run in parallel. Once all three complete, `deploy` runs. If the gate is rejected, the run is cancelled.

---

## Reuse

Flint has one keyword for reuse: `use:`. It works the same way everywhere — the format of the string determines where the template comes from.

### Resolution Rules

| Format | Source | Example |
|--------|--------|---------|
| Plain name | StepTemplate CRD (org-level) | `use: ecr-login` |
| Starts with `./` | Local file in the same repo | `use: ./fragments/setup.yaml` |
| `org/repo/path@ref` | File in another git repo | `use: acme/templates/go-build.yaml@v1` |

All three behave identically: they inline one or more steps at that point. All three support `with:` for passing inputs.

### The Black-Box Rule

When `use:` inlines multiple steps, **the parent `name` is the dependency target**. You never reference internal step names — the reusable file is a black box.

```yaml
steps:
  - name: setup
    use: ./fragments/setup.yaml           # may contain checkout, install, etc.

  - name: build
    dependsOn: [setup]                    # depends on the WHOLE block, not internal steps
    run: npm run build
```

`dependsOn: [setup]` means "wait for everything inside setup to finish." You don't need to know what's inside the reusable file. If the template author renames internal steps, nothing breaks.

For single-step CRD templates, the `name` on the step itself serves this purpose:

```yaml
steps:
  - name: login
    use: ecr-login
    with:
      registry: ${{ env.ECR_REGISTRY }}

  - name: push
    dependsOn: [login]
    run: docker push $IMAGE
```

### Step Templates (CRDs)

Step templates are reusable single-step definitions managed as Kubernetes CRDs. The platform team creates them, pipeline authors use them everywhere.

```yaml
apiVersion: flint.dev/v1
kind: StepTemplate
metadata:
  name: ecr-login
spec:
  description: "Authenticate with AWS ECR"
  inputs:
    - name: registry
      type: string
      required: true
      description: "ECR registry URL"
    - name: region
      type: string
      default: us-east-1
      description: "AWS region"
  image: amazon/aws-cli:2
  run: |
    aws ecr get-login-password --region ${{ inputs.region }} \
      | docker login --username AWS --password-stdin ${{ inputs.registry }}

---
apiVersion: flint.dev/v1
kind: StepTemplate
metadata:
  name: docker-build-push
spec:
  description: "Build and push a Docker image"
  inputs:
    - name: dockerfile
      type: string
      default: Dockerfile
    - name: context
      type: string
      default: "."
    - name: tag
      type: string
      required: true
  run: |
    docker build -t ${{ inputs.tag }} -f ${{ inputs.dockerfile }} ${{ inputs.context }}
    docker push ${{ inputs.tag }}

---
apiVersion: flint.dev/v1
kind: StepTemplate
metadata:
  name: helm-deploy
spec:
  description: "Deploy with Helm"
  inputs:
    - name: release
      type: string
      required: true
    - name: chart
      type: string
      required: true
    - name: namespace
      type: string
      default: default
    - name: values
      type: string
      description: "Path to values file"
  image: alpine/helm:3
  run: |
    helm upgrade --install ${{ inputs.release }} ${{ inputs.chart }} \
      --namespace ${{ inputs.namespace }} \
      ${{ inputs.values && '--values ' + inputs.values }}
```

Usage:

```yaml
steps:
  - name: login
    use: ecr-login
    with:
      registry: 123456789.dkr.ecr.us-east-1.amazonaws.com

  - name: deploy
    use: helm-deploy
    with:
      release: api-gateway
      chart: ./charts/api
      namespace: ${{ environment }}
      values: values/${{ environment }}.yaml
```

Step templates can be used inside nested steps:

```yaml
- name: push
  steps:
    - use: ecr-login
      with:
        registry: ${{ env.ECR_REGISTRY }}
    - name: push-api
      use: docker-build-push
      with:
        tag: ${{ env.ECR_REGISTRY }}/api:${{ shortSha }}
```

### Reusable Step Files (local and cross-repo)

For reusing groups of steps — either within a repo or across repos — define them in a YAML file with a `steps:` list and optional `inputs:` for parameterization.

```yaml
# .flint/fragments/setup.yaml (local) or in a shared repo
inputs:
  - name: node_version
    type: string
    default: "22"

steps:
  - name: checkout
    run: git clone ${{ project.repo }} /workspace/src
  - name: install
    image: node${{ inputs.node_version }}
    workingDir: /workspace/src
    cache:
      key: npm-${{ hashFiles('package-lock.json') }}
      paths: [node_modules]
    run: npm ci
```

**Local reuse (same repo):**

```yaml
# .flint/deploy.yaml
steps:
  - name: setup
    use: ./fragments/setup.yaml

  - name: build
    dependsOn: [setup]                    # depends on the block, not internal steps
    run: npm run build
```

**Cross-repo reuse:**

```yaml
# .flint/deploy.yaml
steps:
  - name: setup
    use: acme/pipeline-templates/steps/node-setup.yaml@v1
    with:
      node_version: "20"

  - name: build
    dependsOn: [setup]
    run: npm run build
```

Cross-repo references use the format `org/repo/path@ref` where `ref` is a git tag, branch, or SHA. Flint clones the referenced repo (cached) and resolves the file.

### Composing a Full Pipeline from Shared Steps

An org can maintain a templates repository. Individual repos compose their pipelines from shared pieces:

```yaml
# acme/api-gateway/.flint/deploy.yaml
environments: [staging, production]

triggers:
  pull_request:
    branches: [main]
  push:
    branches: [main]
    environments: [staging]
  promotion:
    from: staging
    environments: [production]

steps:
  - name: approve
    gate:
      approvers: [role:release-manager]
    environments: [production]

  - name: setup
    use: acme/pipeline-templates/steps/go-setup.yaml@v1

  - name: test
    dependsOn: [setup]
    use: acme/pipeline-templates/steps/go-test.yaml@v1

  - name: build
    dependsOn: [test]
    use: acme/pipeline-templates/steps/go-build.yaml@v1
    with:
      binary: api-gateway

  - name: push
    dependsOn: [build]
    steps:
      - use: ecr-login
        with:
          registry: ${{ env.ECR_REGISTRY }}
      - use: docker-build-push
        with:
          tag: ${{ env.ECR_REGISTRY }}/api-gateway:${{ shortSha }}

  - name: deploy
    dependsOn: [approve, push]
    use: helm-deploy
    with:
      release: api-gateway
      chart: ./charts/api
      namespace: ${{ environment }}
```

Triggers are declared in the repo (explicit, reviewable in PRs). Steps are composed from shared templates. Each `use:` block is a black box — `dependsOn` references the block name, not internal steps.

---

## Image Presets

Image presets are named, version-controlled container image references managed by the platform team. They simplify image selection and optionally enforce image policies.

### Defining Image Presets

```yaml
apiVersion: flint.dev/v1
kind: ImagePreset
metadata:
  name: node20
spec:
  image: node:20-alpine
  description: "Node.js 20 LTS"
  tags: [javascript, frontend]

---
apiVersion: flint.dev/v1
kind: ImagePreset
metadata:
  name: node22
spec:
  image: node:22-alpine
  description: "Node.js 22"
  tags: [javascript, frontend]

---
apiVersion: flint.dev/v1
kind: ImagePreset
metadata:
  name: golang122
spec:
  image: golang:1.22
  description: "Go 1.22"
  tags: [backend]

---
apiVersion: flint.dev/v1
kind: ImagePreset
metadata:
  name: rust179
spec:
  image: rust:1.79-slim
  description: "Rust 1.79"
  tags: [backend]

---
apiVersion: flint.dev/v1
kind: ImagePreset
metadata:
  name: kubectl
spec:
  image: bitnami/kubectl:1.30
  description: "Kubectl CLI"
  tags: [deploy, kubernetes]
```

### Using Image Presets

Reference the preset name in the `image` field:

```yaml
steps:
  - name: build
    image: node22
    run: npm ci && npm run build

  - name: deploy
    image: kubectl
    run: kubectl apply -f manifests/
```

### Presets-Only Mode

An org-level setting (configured in the admin UI, stored in the database) can restrict pipelines to only use preset images:

- **Off** (default): presets are suggestions. Pipeline authors can use any image — presets or custom references like `ubuntu:24.04`.
- **On**: only preset names are valid in the `image` field. Custom image references fail validation.

When presets-only is enabled, the platform team controls the exact images available. Version bumps happen in one place (the CRD) — all pipelines automatically get the update.

---

## Expression Syntax

Expressions use `${{ }}` delimiters within string values.

### Available Context

| Variable | Description | Example |
|----------|-------------|---------|
| `env.NAME` | Environment variable value | `${{ env.CLUSTER_URL }}` |
| `secrets.NAME` | Secret value (environment-scoped or global) | `${{ secrets.API_KEY }}` |
| `inputs.NAME` | Manual trigger input value | `${{ inputs.version }}` |
| `branch` | Git branch name | `${{ branch }}` |
| `commitSha` | Full commit SHA | `${{ commitSha }}` |
| `shortSha` | Short commit SHA (7 chars) | `${{ shortSha }}` |
| `tag` | Git tag (if tag-triggered) | `${{ tag }}` |
| `environment` | Target environment slug (empty if plain run) | `${{ environment }}` |
| `project.name` | Project name | `${{ project.name }}` |
| `project.repo` | Repository path | `${{ project.repo }}` |
| `run.id` | Run ID | `${{ run.id }}` |
| `triggeredBy` | User who triggered the run | `${{ triggeredBy }}` |
| `triggerType` | Trigger type (push, pull_request, manual, etc.) | `${{ triggerType }}` |
| `status` | Current run status (for `when` conditions) | `${{ status }}` |
| `steps.NAME.status` | Status of a specific step | `${{ steps.test.status }}` |
| `matrix.KEY` | Current matrix value (within a matrix step) | `${{ matrix.node }}` |

### Functions

| Function | Description | Example |
|----------|-------------|---------|
| `hashFiles(pattern)` | SHA-256 of file(s) matching pattern | `${{ hashFiles('package-lock.json') }}` |
| `contains(string, search)` | Check if string contains search | `${{ contains(branch, 'release') }}` |
| `startsWith(string, prefix)` | Check if string starts with prefix | `${{ startsWith(tag, 'v') }}` |
| `endsWith(string, suffix)` | Check if string ends with suffix | `${{ endsWith(branch, '-hotfix') }}` |

### Operators (for `if` expressions)

| Operator | Example |
|----------|---------|
| `==` | `${{ branch == 'main' }}` |
| `!=` | `${{ environment != 'production' }}` |
| `&&` | `${{ branch == 'main' && environment == 'staging' }}` |
| `\|\|` | `${{ triggerType == 'manual' \|\| triggerType == 'promotion' }}` |
| `!` | `${{ !inputs.dry_run }}` |

---

## Environment Interaction Model

### The Narrowing Funnel

Environments can be specified at three levels. Each level narrows the scope:

```
Top-level environments          → Pipeline can target these environments
  └─ Trigger-level environments → This trigger creates runs for these environments
      └─ Step-level environments → This step only runs in these environments
```

| Level | Meaning | Default |
|-------|---------|---------|
| Top-level `environments` | Pipeline can only target these | All environments |
| Trigger `environments` | Trigger fires for these | All allowed by top-level |
| Step `environments` | Step runs in these | All allowed by top-level |

### Validation Rules

1. If top-level `environments` is set, all trigger/step environment references must be subsets.
2. If a pipeline is environment-aware, every automated trigger must have `environments` specified.
3. Pull request triggers cannot have `environments` — they are always plain CI.
4. Promotion triggers must have `environments` and `from` (source environment).
5. A trigger's `environments` and its `from` (for promotion) cannot overlap.

### Resolution at Runtime

When a run is created:

1. Flint determines the target environment (from trigger config or manual selection).
2. Steps are filtered: steps with `environments` that don't include the target are **skipped**.
3. Skipped steps are removed from the DAG — dependencies on skipped steps are ignored.
4. Environment variables and secrets resolve for the target environment.
5. `$FLINT_ENVIRONMENT` is set to the environment slug.
6. Gates for the target environment are evaluated.

---

## Complete Examples

### Plain CI

```yaml
# .flint/ci.yaml
triggers:
  pull_request:
    branches: [main]

steps:
  - name: lint
    run: make lint
    continueOnError: true

  - name: test
    run: make test

  - name: build
    dependsOn: [test]
    run: make build
```

### Multi-service Build and Deploy

```yaml
# .flint/deploy.yaml
environments: [staging, production]

triggers:
  pull_request:
    branches: [main]

  push:
    branches: [main]
    environments: [staging]

  promotion:
    from: staging
    environments: [production]

  manual:
    environments: [staging, production]

steps:
  # Gate — production only
  - name: approve
    gate:
      approvers: [role:release-manager]
    environments: [production]

  # Shared setup from org templates repo
  - name: setup
    use: acme/pipeline-templates/steps/go-setup.yaml@v1

  # Tests — all environments
  - name: test
    image: golang122
    dependsOn: [setup]
    run: make test

  - name: lint
    image: golang122
    dependsOn: [setup]
    run: make lint
    continueOnError: true

  # Security scan — production only
  - name: security-scan
    environments: [production]
    dependsOn: [setup]
    run: make security-scan

  # Build all services in parallel
  - name: build-api
    image: golang122
    dependsOn: [test]
    run: go build -o /workspace/api ./cmd/api
    outputs:
      - path: /workspace/api

  - name: build-worker
    image: golang122
    dependsOn: [test]
    run: go build -o /workspace/worker ./cmd/worker
    outputs:
      - path: /workspace/worker

  - name: build-frontend
    image: node22
    run: cd frontend && npm ci && npm run build
    outputs:
      - path: /workspace/frontend/dist

  # Push all images — shared pod for ECR auth
  - name: push
    dependsOn: [build-api, build-worker, build-frontend]
    inputs:
      - from: build-api
        path: /workspace/api
      - from: build-worker
        path: /workspace/worker
      - from: build-frontend
        path: /workspace/frontend/dist
    steps:
      - use: ecr-login
        with:
          registry: ${{ env.ECR_REGISTRY }}
      - name: push-api
        use: docker-build-push
        with:
          dockerfile: Dockerfile.api
          tag: ${{ env.ECR_REGISTRY }}/api:${{ shortSha }}
      - name: push-worker
        use: docker-build-push
        with:
          dockerfile: Dockerfile.worker
          tag: ${{ env.ECR_REGISTRY }}/worker:${{ shortSha }}
      - name: push-frontend
        use: docker-build-push
        with:
          dockerfile: Dockerfile.frontend
          tag: ${{ env.ECR_REGISTRY }}/frontend:${{ shortSha }}

  # Deploy
  - name: deploy
    dependsOn: [approve, push]
    use: helm-deploy
    with:
      release: myapp
      chart: ./charts/myapp
      namespace: ${{ environment }}
      values: values/${{ environment }}.yaml

  # Notifications — always run
  - name: notify
    when: always
    run: |
      curl -X POST ${{ env.SLACK_WEBHOOK }} \
        -d '{"text": "${{ project.name }} ${{ environment }} deploy: ${{ status }}"}'
```

### Nightly Tests with Services

```yaml
# .flint/nightly.yaml
triggers:
  schedule:
    cron: "0 3 * * *"
    environments: [staging]

steps:
  - name: integration-test
    image: golang122
    timeout: 45m
    retry:
      attempts: 2
      delay: 30s
    services:
      - name: postgres
        image: postgres:16
        env:
          POSTGRES_DB: testdb
          POSTGRES_PASSWORD: test
      - name: redis
        image: redis:7
    env:
      DATABASE_URL: postgres://postgres:test@postgres:5432/testdb
      REDIS_URL: redis://redis:6379
    cache:
      key: go-${{ hashFiles('go.sum') }}
      paths: [/root/go/pkg/mod]
    run: go test -tags=integration ./...
```

---

## File Structure

Pipelines live in the `.flint/` directory at the repository root:

```
.flint/
  ci.yaml                   # PR checks
  deploy.yaml               # deployment pipeline
  nightly.yaml              # scheduled nightly tests
  fragments/                # reusable step fragments (local to repo)
    setup.yaml
    notify.yaml
```

Multiple pipeline files per project are supported. Each is independent — they have their own triggers, steps, and environment configurations. Flint discovers and validates all `.flint/*.yaml` files (not files in subdirectories like `fragments/`).

---

## Validation and Preview

Pipeline YAML should feel like writing code — crisp errors with line numbers, clear messages, and actionable suggestions. Flint provides two levels: automatic static validation and on-demand environment simulation.

### Static Validation (automatic)

Runs on demand — when a user views a project's pipelines in the UI, runs `flint validate` in the CLI, or triggers a run. Flint fetches the YAML from git, resolves all `use:` references, and validates. Results are not cached — always fresh from git.

**What it checks:**

| Category | Examples |
|----------|---------|
| Schema | Missing required fields, wrong types, `run` + `steps` on same step |
| DAG | Cycles, unknown step in `dependsOn`, unreachable steps |
| Environment | Unknown environment name, trigger missing `environments` on env-aware pipeline |
| Expression | Invalid `${{ }}` syntax, unknown context variable, unclosed expression |
| Template | Unknown step template, missing required input, extra input not in template spec |
| Image | Unknown preset name (when presetsOnly is enabled) |
| Trigger | Duplicate triggers, manual inputs without defaults alongside automated triggers |
| Nested steps | `runner`/`inputs`/`outputs` on sub-steps (not allowed) |

**Error format:**

Each error includes:
- Line and column number (exact location in the YAML)
- Clear message (what's wrong)
- Suggestion when possible ("did you mean `build`?", "available environments: staging, production")
- Severity: `error` (blocks runs) or `warning` (informational, doesn't block)

**How it displays on the project pipeline tab:**

```
deploy.yaml          ✓ Valid        3 triggers · 8 steps
ci.yaml              ✗ 2 errors
nightly.yaml         ⚠ 1 warning   1 trigger · 3 steps
```

Click into `ci.yaml` to see:

```
ci.yaml — 2 errors

  Line 14, col 17: Step "deploy" references unknown step "bild" in dependsOn
                   Did you mean "build"?

  Line 22, col 21: Environment "prodction" is not defined
                   Available environments: staging, production
```

**When validation runs:**

| Event | Action |
|-------|--------|
| UI: view project pipelines | Fetch from git, validate, render results |
| UI: environment simulation | Fetch, resolve templates, validate, render preview |
| CLI: `flint validate .flint/` | Read local files, resolve templates, validate |
| Run creation | Fetch at exact commit SHA, resolve all `use:`, validate |

**Blocking behavior:**

A run **cannot be created** if the pipeline has validation errors at the target commit SHA. The UI shows errors and disables the trigger button. Automated triggers (push, promotion) skip the run and surface an alert — "Pipeline validation failed on commit abc123."

Warnings don't block — they show in the UI and run log but the pipeline proceeds.

### Environment Simulation (on-demand)

A user picks an environment from a dropdown on the pipeline detail page and clicks "Preview." Flint resolves the pipeline for that environment and shows exactly what would happen — without running anything.

**What it shows:**

**Triggers** — which triggers are active for this environment:
```
Triggers active for production:
  ✓ promotion (from staging)
  ✓ manual
  · pull_request — not applicable (plain CI only)
  · push — environments: [staging] — not this environment
```

**Steps** — filtered DAG with active/skipped status:
```
Steps (5 active, 2 skipped):
  ✓ approve          gate · approvers: role:release-manager
  ✓ test             image: golang122
  ○ lint             skipped (environments: [staging])
  ✓ security-scan    environments: [production]
  ✓ push             nested: ecr-login → push-api → push-worker → push-frontend
  ○ canary           skipped (environments: [staging])
  ✓ deploy           depends on: approve, push
```

**Variables and secrets** — coverage check against the environment's configured values:
```
Variables:
  ✓ ECR_REGISTRY     = 123456789.dkr.ecr.us-east-1.amazonaws.com
  ✓ CLUSTER_URL      = api.acme.com
  ✓ SLACK_WEBHOOK    = ●●●●●●
  ✗ DEPLOY_KEY       ⚠ not set for production
```

**Templates** — resolved status of all `use:` references:
```
Templates:
  ✓ ecr-login            StepTemplate CRD (v1)
  ✓ docker-build-push    StepTemplate CRD (v1)
  ✓ helm-deploy          StepTemplate CRD (v1)
  ✓ go-setup.yaml        acme/pipeline-templates@v1 — resolved
```

**Resolved DAG visualization** — the pipeline DAG with only active steps, showing the actual execution graph for that environment.

Switch to a different environment and the entire preview updates — different steps, different variables, different triggers.

### No Caching — Always Fresh

Pipeline YAML is **not stored in the database**. Every time pipeline data is needed, Flint fetches it from git, resolves all `use:` references, and validates. Git is the only source of truth.

| Action | What happens |
|--------|-------------|
| UI: view project pipelines | Fetch `.flint/*.yaml` from git, validate, render |
| UI: environment simulation | Fetch + resolve templates + filter by environment |
| CLI: `flint validate` | Read local files, resolve templates, validate |
| Run creation | Fetch at exact commit SHA, resolve all `use:`, validate, then execute |

This means:
- No `Pipeline` table in the database
- No sync webhooks for pipeline YAML
- No staleness — what you see is always what's in git right now
- No consistency problems with template repo changes or CRD updates
- The UI shows a loading state while fetching — typically 200-500ms for simple pipelines

Template resolution (CRD lookups, cross-repo git fetches) can be cached briefly in memory for the duration of a single request to avoid redundant fetches within the same page load. But nothing is persisted.

---

## Open Questions

_To be resolved as we iterate:_

- **Concurrency**: Pipeline-level or environment-level concurrency limits (e.g., "only one production deploy at a time"). Syntax TBD.
- **Notifications**: Structured notification routing (Slack, PagerDuty, email) vs. inline `curl` commands. Whether to make this a first-class feature or leave it to step templates.
- **Secrets masking**: Automatically redacting secret values from step logs.

