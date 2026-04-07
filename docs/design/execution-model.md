# Execution Model

## Overview

Flint runs each pipeline step as an independent Kubernetes pod. Steps can run on different runner pools (node types, architectures, GPU vs CPU), with artifacts passed between them via object storage. Steps that need shared process state (e.g. a Docker daemon) can be grouped into a single pod.

## Core Principles

1. **Pod-per-step** — Each step is an isolated pod. Different steps can use different runner pools, images, and resource requests.
2. **Artifact passing via S3** — Files flow between steps through object storage. Each step has fast local disk (`emptyDir`) for its own work.
3. **Pod identity for cloud auth** — Runner pools are associated with Kubernetes service accounts. Steps inherit cloud credentials automatically via IRSA (EKS), Workload Identity (GKE), or Pod Identity (AKS).
4. **Step groups for shared state** — Steps that need process-level sharing (Docker daemon, database connection) run in the same pod.

## Step Execution

### Independent Steps (default)

Each step runs as a standalone pod:

```yaml
steps:
  - name: checkout
    run: git clone $REPO /workspace/src
    outputs:
      - path: /workspace/src

  - name: build
    runner: large-builders
    inputs:
      - from: checkout
        path: /workspace/src
    run: |
      cd /workspace/src
      go build -o /workspace/bin/server ./cmd/server
    outputs:
      - path: /workspace/bin

  - name: test
    runner: gpu-pool
    inputs:
      - from: build
        path: /workspace/bin
    run: ./workspace/bin/server --self-test
```

Under the hood for each step:

1. Controller creates a pod on the runner pool's node group
2. Agent (init container or sidecar) downloads declared `inputs` from S3 to the specified paths
3. The step's `run` command executes
4. Agent uploads declared `outputs` to S3
5. Pod terminates

No PVCs, no node affinity, no RWX storage requirements. Each step pod is fully independent and can schedule on any node.

### Grouped Steps (shared pod)

Steps in the same `group` run as containers in a single pod, sharing filesystem and process namespace:

```yaml
steps:
  - name: build-image
    group: docker
    run: |
      docker build -t $IMAGE .
    inputs:
      - from: checkout
        path: /workspace/src

  - name: push-image
    group: docker
    dependsOn: [build-image]
    run: |
      docker push $IMAGE

  - name: scan-image
    group: docker
    dependsOn: [build-image]
    run: |
      trivy image $IMAGE
```

Within a group:
- Steps share an `emptyDir` volume at `/workspace`
- Steps share sidecar containers (e.g. Docker-in-Docker)
- Steps execute sequentially within the pod based on `dependsOn` ordering
- All steps in the group use the same runner pool

Grouped steps do not need artifact declarations between each other — they share the filesystem directly.

## Artifact Passing

### How It Works

Artifacts are stored in the Flint artifact bucket at `s3://{bucket}/artifacts/{runId}/{stepName}/{path-hash}`.

**Upload (after step completes):**
- Agent tars each declared output path
- Uploads to S3 with the run ID and step name as the key prefix
- Records the artifact manifest (paths, sizes, checksums) in the step result

**Download (before step starts):**
- Agent reads the `inputs` declarations
- Downloads and extracts artifacts from the referenced step's outputs
- Places them at the declared `path`

**Lifecycle:**
- Artifacts are created when a step completes
- Artifacts are deleted when the run is cleaned up (configurable retention, default 7 days)
- Failed runs retain artifacts for debugging (configurable)

### Performance Considerations

- S3 transfers within the same region are fast (typically 1-5 seconds for typical build artifacts)
- Large artifacts (>1GB) may benefit from parallel multipart upload/download
- Docker images should be pushed to a registry, not passed as artifacts — the pipeline should `docker push` and later `docker pull`
- For monorepo checkouts, the checkout step's output can be large. Consider shallow clones or sparse checkout to minimize artifact size.

### Artifact Declaration

```yaml
steps:
  - name: build
    run: make build
    outputs:
      - path: /workspace/dist          # directory — tarred and uploaded
      - path: /workspace/coverage.xml   # single file

  - name: deploy
    inputs:
      - from: build                     # step name
        path: /workspace/dist           # where to extract
    run: deploy.sh /workspace/dist
```

If a step declares no `inputs`, nothing is downloaded. If a step declares no `outputs`, nothing is uploaded. Steps with no artifact declarations are fully self-contained (e.g. notification steps, cleanup steps).

## Cloud Authentication

### Pod Identity (recommended)

Runner pools are associated with Kubernetes service accounts. The service account carries cloud provider annotations:

```yaml
# Flint runner pool configuration
apiVersion: flint.dev/v1
kind: RunnerPool
metadata:
  name: production-deployers
spec:
  serviceAccount: flint-prod-deploy
  resources:
    cpu: "2"
    memory: 4Gi
```

```yaml
# Kubernetes service account with IRSA
apiVersion: v1
kind: ServiceAccount
metadata:
  name: flint-prod-deploy
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789:role/flint-prod-deploy
```

When a step runs on the `production-deployers` pool, its pod uses the `flint-prod-deploy` service account. AWS SDKs, `aws` CLI, and `kubectl` automatically pick up the projected token. No explicit credential steps needed.

**Supported providers:**

| Provider | Mechanism | Annotation |
|----------|-----------|------------|
| AWS EKS | IRSA / EKS Pod Identity | `eks.amazonaws.com/role-arn` |
| GCP GKE | Workload Identity | `iam.gke.io/gcp-service-account` |
| Azure AKS | Azure Workload Identity | `azure.workload.identity/client-id` |

### Explicit Credentials (escape hatch)

For third-party services or cases where pod identity isn't configured, use Flint secrets injected as environment variables:

```yaml
steps:
  - name: notify
    env:
      SLACK_WEBHOOK: ${{ secrets.SLACK_WEBHOOK }}
    run: |
      curl -X POST $SLACK_WEBHOOK -d '{"text": "Deploy complete"}'
```

These credentials are stored in Flint's secret management system (envelope-encrypted, scoped to environments/workspaces).

### ECR / GCR / ACR Login

With pod identity, container registry auth becomes a one-liner rather than a dedicated action:

```yaml
steps:
  - name: push
    runner: production-deployers  # has IRSA for ECR access
    run: |
      aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin $REGISTRY
      docker push $IMAGE
```

Or for daemonless builds (no Docker-in-Docker needed):

```yaml
steps:
  - name: build-and-push
    runner: production-deployers
    image: gcr.io/kaniko-project/executor
    run: |
      /kaniko/executor --dockerfile=Dockerfile --destination=$IMAGE
```

Kaniko and Buildah pick up IRSA credentials automatically for registry auth.

## Pod Specification

### Generated Pod for an Independent Step

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: flint-{runId}-{stepName}
  namespace: flint-runs
  labels:
    flint.dev/run: "{runId}"
    flint.dev/step: "{stepName}"
    flint.dev/project: "{projectId}"
  annotations:
    flint.dev/pipeline: "{pipelineFile}"
spec:
  serviceAccountName: "{runnerPool.serviceAccount}"
  nodeSelector:
    "{runnerPool.nodeSelector}"
  containers:
    - name: step
      image: "{step.image | default runnerPool.defaultImage}"
      command: ["/bin/sh", "-c"]
      args: ["{step.run}"]
      env:
        - name: FLINT_RUN_ID
          value: "{runId}"
        - name: FLINT_STEP_NAME
          value: "{stepName}"
        - name: FLINT_WORKSPACE
          value: "{workspace}"
        # ... user-defined env vars and secrets
      resources:
        requests:
          cpu: "{step.cpu | default runnerPool.cpu}"
          memory: "{step.memory | default runnerPool.memory}"
      volumeMounts:
        - name: workspace
          mountPath: /workspace
    - name: agent
      image: ghcr.io/nerdmenot/flint-agent:latest
      # Agent handles: artifact download, log streaming, artifact upload, step lifecycle
      volumeMounts:
        - name: workspace
          mountPath: /workspace
  volumes:
    - name: workspace
      emptyDir:
        sizeLimit: "{runnerPool.workspaceSize | default 10Gi}"
  restartPolicy: Never
  activeDeadlineSeconds: "{step.timeout | default 3600}"
```

### Generated Pod for a Step Group

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: flint-{runId}-group-{groupName}
spec:
  serviceAccountName: "{runnerPool.serviceAccount}"
  initContainers:
    # Agent downloads inputs before any step runs
    - name: agent-init
      image: ghcr.io/nerdmenot/flint-agent:latest
      command: ["flint-agent", "download-inputs"]
      volumeMounts:
        - name: workspace
          mountPath: /workspace
  containers:
    # Each step in the group is a container
    # Agent orchestrates their execution order based on dependsOn
    - name: build-image
      image: docker:dind
      command: ["/bin/sh", "-c", "docker build -t $IMAGE /workspace/src"]
      volumeMounts:
        - name: workspace
          mountPath: /workspace
        - name: docker-socket
          mountPath: /var/run/docker.sock
    - name: push-image
      image: docker:cli
      command: ["/bin/sh", "-c", "docker push $IMAGE"]
      volumeMounts:
        - name: workspace
          mountPath: /workspace
        - name: docker-socket
          mountPath: /var/run/docker.sock
    # Docker-in-Docker sidecar
    - name: dind
      image: docker:dind
      securityContext:
        privileged: true
      volumeMounts:
        - name: docker-socket
          mountPath: /var/run/docker.sock
    # Agent sidecar for log streaming and lifecycle
    - name: agent
      image: ghcr.io/nerdmenot/flint-agent:latest
      volumeMounts:
        - name: workspace
          mountPath: /workspace
  volumes:
    - name: workspace
      emptyDir:
        sizeLimit: 20Gi
    - name: docker-socket
      emptyDir: {}
  restartPolicy: Never
```

## Lifecycle

### Pipeline Run Lifecycle

```
1. Run triggered (push, PR, manual, schedule)
2. Controller resolves the DAG from the pipeline definition
3. For each wave of steps (respecting dependsOn):
   a. Controller creates step pods (or group pods)
   b. Agent in each pod:
      - Downloads inputs from S3
      - Streams logs to server via gRPC
      - Executes the step command
      - Uploads outputs to S3
      - Reports step result (success/failure + digest)
   c. Controller waits for all steps in the wave to complete
   d. If any required step fails, remaining steps are cancelled
4. Controller marks run as succeeded/failed
5. Artifacts retained for configured retention period
6. Step pods are already terminated (restartPolicy: Never)
```

### Timeout Handling

- Per-step timeout via `activeDeadlineSeconds` on the pod (default: 1 hour)
- Per-pipeline timeout on the run record (default: 6 hours)
- Controller cancels remaining steps if pipeline timeout is reached

### Cleanup

- Step pods are not restarted (`restartPolicy: Never`) — they terminate on completion
- Completed pods are garbage-collected by the controller after recording results (configurable retain period for debugging, default: 1 hour)
- Artifacts in S3 are cleaned up by a periodic sweep based on retention policy
- PVCs are not used — nothing to clean up

## Comparison with Other Systems

| Feature | Flint | GitHub Actions | Tekton | Argo Workflows |
|---------|-------|----------------|--------|----------------|
| Step isolation | Pod-per-step | All steps in one VM | Pod-per-step (in TaskRun) | Pod-per-step |
| Artifact passing | S3 | Artifact actions (S3/Azure) | PVC or S3 | S3 or artifact repo |
| Cloud auth | Pod identity | Explicit OIDC/credentials | Pod identity | Pod identity |
| Shared state | Step groups | Implicit (same VM) | Workspaces (PVC) | Script templates |
| Different runners per step | Yes (runner pools) | Matrix only | Yes (taskRef) | Yes (templates) |
| Docker builds | DinD sidecar in group | Native on VM | DinD sidecar | DinD sidecar |

## Configuration Reference

### Step-level options

```yaml
steps:
  - name: string              # required
    run: string               # shell command to execute
    image: string             # container image (default: runner pool's default)
    runner: string            # runner pool name (default: pipeline's default)
    group: string             # step group name (optional, for shared-pod execution)
    dependsOn: [string]       # step names this step depends on
    timeout: duration         # step timeout (default: 1h)
    env:                      # environment variables
      KEY: value
      SECRET: ${{ secrets.NAME }}
    inputs:                   # artifacts to download before execution
      - from: stepName
        path: /local/path
    outputs:                  # artifacts to upload after execution
      - path: /local/path
    isolate: boolean          # force pod-per-step even in a group (default: false)
```

### Runner pool options

```yaml
apiVersion: flint.dev/v1
kind: RunnerPool
metadata:
  name: string
spec:
  serviceAccount: string     # K8s service account (for pod identity)
  defaultImage: string       # default container image for steps
  nodeSelector: {}           # K8s node selector
  tolerations: []            # K8s tolerations
  resources:
    cpu: string              # default CPU request
    memory: string           # default memory request
  workspaceSize: string      # emptyDir size limit (default: 10Gi)
```
