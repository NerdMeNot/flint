# Log Architecture in Flint

## Problem Statement

CI build logs have unique characteristics that make them challenging:

1. **High volume** — a single build step can produce thousands of lines per second (compiler output, test results, docker layer pulls)
2. **Append-only** — logs are never modified, only appended during execution and read afterward
3. **Ephemeral producers** — the agent pod that produces logs may crash or be evicted mid-run
4. **Real-time viewers** — users watching a running build expect sub-second latency for new lines
5. **Long-term storage** — completed logs must be retrievable for debugging days/weeks later
6. **No data loss** — losing log lines during a failed build is the worst possible outcome (you need the logs most when things break)

### Why not Postgres?

Logs are sequential, high-volume, and read as a blob — the worst possible workload for a relational database. Storing logs in Postgres would:
- Bloat the database (logs are 10-100x larger than all other data combined)
- Degrade query performance for everything else
- Require expensive vacuum/maintenance cycles
- Make backups slow and expensive

### Why not just S3 directly from the agent?

The agent could write directly to S3, but:
- **No real-time streaming** — S3 is not a message bus, you'd poll with GET requests ($0.0004 per request, adds up fast)
- **No crash safety** — if the agent crashes mid-write, the partial upload is lost
- **High PUT cost at line granularity** — writing each line as a PUT would cost $0.005 per 1,000 requests. A build with 10,000 lines = $0.05 per build just for log writes.

### Why not the Flint server as the write path?

The server could buffer logs in memory and flush to S3, but:
- **Server crash = data loss** — in-memory buffers are gone
- **Server becomes a bottleneck** — every log line passes through the server
- **Coupling** — the server's availability becomes critical for log durability

## Architecture

Flint uses a **three-path architecture** that separates durability, real-time streaming, and long-term storage:

```
┌─────────────────────────────────────────────────────────────┐
│ Agent Pod                                                    │
│                                                              │
│  ┌──────────┐     stdout/stderr     ┌──────────────────┐   │
│  │ Step     │ ──────────────────── │ Fluent Bit        │   │
│  │ Container│                       │ (sidecar)         │   │
│  └──────────┘                       │                    │   │
│       │                             │ • captures stdout  │   │
│       │ step completion             │ • disk buffer      │   │
│       │ + last N lines              │ • ships to S3      │   │
│       ▼                             └────────┬───────────┘   │
│  ┌──────────┐                                │               │
│  │ Agent    │ ── POST /steps/:name/complete  │               │
│  │ Process  │    (includes last N lines      │               │
│  └──────────┘     for real-time fan-out)     │               │
│                                              │               │
└──────────────────────────────────────────────┼───────────────┘
                                               │
                    ┌──────────────────────────┐│
                    │                          ││
                    ▼                          ▼│
              ┌──────────┐              ┌──────────┐
              │ Flint    │              │ S3       │
              │ Server   │              │ Bucket   │
              │          │              │          │
              │ • SSE    │              │ durable  │
              │   fan-out│              │ log      │
              │ • in-mem │              │ storage  │
              │   buffer │              │          │
              └────┬─────┘              └────┬─────┘
                   │                         │
                   │ SSE events              │ GET (completed)
                   ▼                         │
              ┌──────────┐                   │
              │ Browser  │◄──────────────────┘
              │ / TUI    │
              └──────────┘
```

### Three paths explained:

**Path 1: Durability (Agent → Fluent Bit → S3)**
- Fluent Bit runs as a sidecar container in the agent pod
- Captures stdout/stderr directly from the step container
- Buffers to local disk (survives container restarts)
- Ships to S3 in batches (every 5s or 1MB, whichever comes first)
- **Zero log loss** — even if the agent, server, or network fails, logs are safe in Fluent Bit's disk buffer and eventually reach S3

**Path 2: Real-time streaming (Agent → Server → SSE → Browser)**
- When the agent reports step progress or completion, it includes the last N log lines in the request body
- The server pushes these lines to SSE subscribers immediately
- This is best-effort — if the server is down, lines aren't lost (Path 1 handles durability)
- Latency: sub-second (agent callback → server → SSE → browser)

**Path 3: Historical reads (Browser → Server → S3)**
- For completed steps, the browser requests the full log
- Server reads the JSONL object from S3 and returns it
- One GET request per step, cacheable by the browser
- No real-time polling needed

## Fluent Bit Configuration

Fluent Bit runs as a sidecar in the agent pod, configured to:

```yaml
# fluent-bit.conf
[INPUT]
    Name              tail
    Path              /var/log/flint/steps/*.log
    Tag               flint.logs.*
    Refresh_Interval  1
    Read_from_Head    True
    DB                /var/log/flint/fb.db  # tracks file position for crash recovery

[FILTER]
    Name    modify
    Match   flint.logs.*
    Add     org_id    ${ORG_ID}
    Add     run_id    ${RUN_ID}
    Add     step_name ${STEP_NAME}

[OUTPUT]
    Name                         s3
    Match                        flint.logs.*
    bucket                       ${FLINT_LOG_BUCKET}
    region                       ${AWS_REGION}
    s3_key_format                /logs/$org_id/$run_id/$step_name.jsonl
    total_file_size              1M
    upload_timeout               5s
    store_dir                    /var/log/flint/s3-buffer
    use_put_object               On
    compression                  gzip
```

Key settings:
- `DB` — file-position tracking database. If Fluent Bit restarts, it resumes from where it left off.
- `store_dir` — local disk buffer for S3 uploads. If S3 is unreachable, data accumulates here.
- `upload_timeout: 5s` — flush to S3 every 5 seconds at most.
- `total_file_size: 1M` — flush when buffer reaches 1MB.
- `compression: gzip` — reduces S3 storage cost and transfer time.

## S3 Object Layout

```
s3://flint-logs/
  logs/
    <org-id>/
      <run-id>/
        checkout.jsonl.gz
        install-deps.jsonl.gz
        lint.jsonl.gz
        unit-tests.jsonl.gz
        build.jsonl.gz
```

Each file is a gzipped JSONL (newline-delimited JSON) with one object per line:

```json
{"timestamp":"2024-01-15T10:00:32Z","stream":"stdout","content":"[10:00:32] Running: go test -race ./..."}
{"timestamp":"2024-01-15T10:00:45Z","stream":"stdout","content":"ok  acme/api-gateway/internal/handler 0.8s"}
{"timestamp":"2024-01-15T10:00:52Z","stream":"stderr","content":"WARNING: deprecated API usage detected"}
```

The last line of every completed step's log is a sentinel:

```json
{"timestamp":"2024-01-15T10:01:53Z","stream":"_flint","content":"__EOF__","exitCode":0,"lineCount":4523,"sha256":"a3f8c21..."}
```

This enables verification — see "Log Verification and Tail Repair" below.

## S3Sink (Go implementation)

The `S3Sink` in `pkg/logsink/s3.go` implements the `LogSink` interface:

- **Write**: used by the server when receiving tail-repair lines from the agent's step-completion callback
- **Read**: used to serve completed log requests. Single `GetObject` call, decompress gzip, parse JSONL.

The `Tail` method is NOT used for production streaming — real-time streaming goes through the in-memory SSE path. `Tail` exists only as a recovery mechanism if the server needs to catch up from S3 after a restart.

## Log Verification and Tail Repair

### The Problem

Fluent Bit flushes to S3 every 5 seconds. If the agent pod terminates (step completes, OOM, eviction) between flushes, the last few seconds of log output can be lost. These are often the most important lines — build results, test summaries, error messages.

### The Solution: Completion Digest + Tail Lines

When a step finishes, the agent does three things:

**1. Appends an EOF sentinel to the log file**

The agent writes a final line to the log file that Fluent Bit is tailing:

```json
{"stream":"_flint","content":"__EOF__","exitCode":0,"lineCount":4523,"sha256":"a3f8c21..."}
```

This line flows through Fluent Bit to S3 like any other line. Its presence in S3 confirms the log is complete.

**2. Signals Fluent Bit to flush immediately**

The agent sends SIGUSR1 to the Fluent Bit process, triggering an immediate S3 upload. It then waits briefly (2-3 seconds) for the flush to complete before proceeding.

```go
// In the agent, after writing EOF sentinel:
syscall.Kill(fluentBitPID, syscall.SIGUSR1)
time.Sleep(3 * time.Second)
```

**3. Sends a completion callback with digest + tail lines**

The agent reports step completion to the Flint server, including:

```json
POST /api/v1/runs/:id/steps/build/complete
{
  "status": "succeeded",
  "exitCode": 0,
  "duration": "33.1s",
  "log": {
    "lineCount": 4523,
    "sha256": "a3f8c21e9b4f...",
    "tailLines": [
      {"timestamp":"...","stream":"stdout","content":"Build succeeded: bin/api-gateway (18.4MB)"},
      {"timestamp":"...","stream":"stdout","content":"Build completed in 33.1s"},
      {"timestamp":"...","stream":"_flint","content":"__EOF__","exitCode":0,"lineCount":4523,"sha256":"a3f8c21..."}
    ]
  }
}
```

The `tailLines` field contains the last ~50 lines. This is small (a few KB) and covers the most common failure case.

### Server-Side Verification (async)

After receiving the completion callback, the server queues an async verification job:

```
1. Read the S3 object for this step
2. Count lines in S3
3. Check: does the last line contain "__EOF__"?
   → YES: log is complete. Verify lineCount and sha256 match.
   → NO: log is truncated. Proceed to tail repair.
4. If truncated:
   a. Compare S3 line count vs agent's lineCount
   b. Append the missing tailLines from the completion payload to S3
   c. Re-upload the repaired object
   d. Verify again
5. Store verification result on the step record:
   - log_status: 'complete' | 'repaired' | 'incomplete'
   - log_line_count: 4523
```

### Verification States

| S3 has EOF? | Line count matches? | SHA matches? | Result |
|---|---|---|---|
| Yes | Yes | Yes | `complete` — perfect |
| Yes | Yes | No | `complete` — hash mismatch likely due to compression, OK |
| No | — | — | `repaired` if tail lines cover the gap, `incomplete` otherwise |

### Graceful Shutdown

The agent pod uses a `preStop` hook to maximize flush time:

```yaml
lifecycle:
  preStop:
    exec:
      command:
        - sh
        - -c
        - |
          # Signal Fluent Bit to flush immediately
          kill -SIGUSR1 $(pidof fluent-bit) 2>/dev/null
          # Wait for flush to complete
          sleep 5
```

Combined with a `terminationGracePeriodSeconds: 30` on the pod, this gives Fluent Bit up to 30 seconds to flush remaining buffers before SIGKILL.

### Disk Buffer Durability

Fluent Bit's disk buffer uses `emptyDir` in the agent pod:

```yaml
volumes:
  - name: fluentbit-buffer
    emptyDir:
      sizeLimit: 100Mi
```

| Scenario | emptyDir survives? | Log loss? |
|---|---|---|
| Container restart (OOM, crash) | Yes | No — FB resumes from disk |
| Pod restart (same node) | No | No — tail repair covers the gap |
| Pod eviction/reschedule | No | No — tail repair covers the gap |
| Node failure | No | Minimal — tail repair covers last ~50 lines |

The key insight: `emptyDir` doesn't need to survive pod deletion because **tail repair** fills any gap. The agent always holds the last ~50 lines in memory and sends them in the completion callback. Even if Fluent Bit loses its entire buffer, the most important lines (the ending) are preserved.

## Cost Analysis

For a typical build with 5,000 log lines across 8 steps:

| Operation | Count | Cost |
|---|---|---|
| Fluent Bit → S3 PUT (batched per step) | ~16 (2 per step avg) | $0.00008 |
| Browser → S3 GET (completed step) | 8 | $0.0000032 |
| Verification GET (async, one per step) | 8 | $0.0000032 |
| Tail repair PUT (only if needed) | 0-2 | $0.00001 |
| S3 storage (gzipped, ~50KB per step) | 400KB | ~$0.00001/month |
| **Total per build** | | **< $0.0002** |

## Failure Scenarios

| Scenario | What happens | Log loss? |
|---|---|---|
| Agent crashes mid-step | Step marked failed. FB disk buffer survives container restart. On re-dispatch, FB resumes. | No |
| Agent pod evicted | Step marked failed. FB buffer lost, but agent sends tail lines in failure callback. Server tail-repairs. | Last ~50 lines preserved, earlier lines may be lost if FB hadn't flushed |
| Server crashes | SSE drops, browsers reconnect. FB continues shipping to S3 independently. Completion callback retries. | No |
| S3 unreachable | FB buffers to disk, retries with backoff. Completion callback succeeds (doesn't need S3). Verification retries later. | No |
| Network partition (agent ↔ server) | Real-time SSE stops. FB ships to S3 directly. When partition heals, agent sends completion. | No |
| Fluent Bit crashes | Container restarts. DB tracks position, resumes. Worst case: loses up to 5s of lines. Tail repair fills the gap. | No (tail repair) |
| Hard SIGKILL (no graceful shutdown) | FB buffer lost. Agent doesn't send completion. Step times out, marked failed. S3 has everything FB flushed before kill. | Up to 5s of lines lost, no tail repair possible |

## Log Durability Tiers

Different organizations have different tolerance for log loss. Flint offers two durability tiers, configurable per-environment or globally.

### Standard (default)

**Guarantee**: best-effort, up to 5s of lines may be lost on hard pod kill.
**When to use**: development, staging, cost-sensitive teams, most CI workloads.

```
Agent → log file → Fluent Bit sidecar → S3
                    (ephemeral volume buffer)
Agent → completion callback with tail lines → Server → tail repair on S3
```

| Component | Configuration |
|---|---|
| Fluent Bit buffer | Ephemeral volume (lost on pod delete) |
| Write path | Single: Fluent Bit → S3 |
| Tail repair | Last ~50 lines via completion callback |
| Verification | Async: EOF marker + line count check |

**Risk**: Hard SIGKILL before Fluent Bit flushes → up to 5s of tail lines lost. Tail repair covers most cases, but if the agent is also killed before sending the completion callback, those lines are gone.

**Cost**: Minimal. No persistent volumes, no extra infrastructure.

#### Buffer storage options

The Fluent Bit buffer volume is configured based on the deployment environment:

**Cloud (EKS, GKE, AKS)** — use a generic ephemeral volume backed by the cloud's block storage (e.g., EBS gp3). This gives dedicated IOPS, doesn't compete with the node's root disk, and is auto-cleaned on pod termination:

```yaml
volumes:
  - name: fluentbit-buffer
    ephemeral:
      volumeClaimTemplate:
        spec:
          accessModes: ["ReadWriteOnce"]
          storageClassName: gp3   # or pd-balanced (GKE), managed-csi (AKS)
          resources:
            requests:
              storage: 500Mi
```

Benefits over `emptyDir`:
- **Dedicated IOPS** — gp3 provides 3,000 baseline IOPS independent of volume size. Not shared with node workloads.
- **No node disk pressure** — the volume is a separate block device, doesn't count against the node's root volume. No risk of kubelet evicting the pod due to disk pressure from logs.
- **Right-sized** — 500Mi is plenty for buffering; you're not over-provisioning a full PV.
- **Auto-cleanup** — deleted with the pod, no PVC garbage collection needed.

**Local dev / kind / bare metal** — use `emptyDir` for simplicity:

```yaml
volumes:
  - name: fluentbit-buffer
    emptyDir:
      sizeLimit: 500Mi
```

The agent's Helm chart / pod template selects the appropriate volume type based on a `logBufferType` setting (`ephemeral` or `emptyDir`).

---

### Guaranteed

**Guarantee**: cryptographically verifiable completeness. Every line is provably stored.
**When to use**: production, compliance, regulated industries, any team that cannot tolerate log loss.

```
Agent → WAL (PersistentVolume) → log file → Fluent Bit → S3     (path A)
Agent → gRPC stream → Server → S3                                (path B)
Agent → completion callback with WAL digest → Server → verify
```

| Component | Configuration |
|---|---|
| Fluent Bit buffer | **PersistentVolume** (survives pod reschedule) |
| Write paths | **Dual**: Fluent Bit → S3 AND Agent → Server → S3 |
| **Write-ahead log (WAL)** | **Agent writes every line to a PV-backed WAL before stdout** |
| Verification | **WAL checksum is the source of truth** |
| Tamper detection | SHA256 chain: each line's hash includes the previous line's hash |

**How dual-write works:**

During step execution, the agent sends log lines to the server over a persistent gRPC stream (or chunked HTTP POST) in addition to writing to the local log file that Fluent Bit tails. The server buffers these lines in memory (for SSE fan-out) and flushes to S3 every 5 seconds as a second copy:

```
s3://flint-logs/logs/<org>/<run>/<step>.jsonl.gz          ← Fluent Bit's copy
s3://flint-logs/logs/<org>/<run>/<step>.server.jsonl.gz    ← Server's copy
```

On step completion, the server reconciles: reads both S3 objects, takes the longer one (more lines), merges if they differ, and writes the canonical version. The `.server.jsonl.gz` copy is then deleted.

**How the WAL works:**

Before the agent writes a log line to stdout (where the step container and Fluent Bit can see it), it first writes the line to a write-ahead log on the PersistentVolume:

```go
// Agent log capture loop:
for line := range stdout {
    // 1. Write to WAL first (durable)
    wal.Write(line)  // fsync'd to PV

    // 2. Then write to log file (Fluent Bit tails this)
    logFile.Write(line)

    // 3. Send to server stream (real-time)
    grpcStream.Send(line)
}
```

The WAL is the **single source of truth**. If the container crashes between step 1 and step 2, the WAL has the line but Fluent Bit doesn't — the verification step detects and repairs this.

**SHA256 chain for tamper detection:**

Each WAL entry includes a chained hash:

```json
{"seq":1,"hash":"a3f8...","prevHash":"0000...","content":"..."}
{"seq":2,"hash":"b7e4...","prevHash":"a3f8...","content":"..."}
{"seq":3,"hash":"c9d1...","prevHash":"b7e4...","content":"..."}
```

On completion, the agent sends the final hash + sequence number. The server verifies the entire chain by reading the S3 object and recomputing hashes. Any tampered or missing line breaks the chain.

**PersistentVolume for Fluent Bit:**

If the agent pod is evicted and rescheduled on a different node, Fluent Bit's disk buffer on the PV still has unshipped data. When the new pod mounts the same PV, Fluent Bit resumes from where it left off.

```yaml
volumes:
  - name: fluentbit-buffer
    persistentVolumeClaim:
      claimName: fb-buffer-{{ .RunID }}
```

PVCs are created per-run and cleaned up after the run completes + verification passes.

**Cost**: PV provisioning (~100Mi per run), higher PV IOPS (fsync per WAL write), dual write bandwidth, gRPC streaming. Roughly 3x the cost of Standard.

---

### Comparison

| | Standard | Guaranteed |
|---|---|---|
| Write paths | 1 (FB → S3) | 2 (FB + Server → S3) + WAL |
| Buffer | emptyDir | PersistentVolume |
| Worst-case loss | ~5s of lines | Zero (provable) |
| Verification | Line count + EOF | SHA256 chain |
| Tamper detection | No | Yes |
| Cost multiplier | 1x | ~3x |
| Use case | Dev, staging, most CI | Production, compliance, regulated |

### Configuration

The durability tier is set per-environment in the Flint admin:

```yaml
environments:
  - name: dev
    logDurability: standard
  - name: staging
    logDurability: standard
  - name: production
    logDurability: guaranteed
```

The agent reads the target environment's durability setting and configures its log pipeline accordingly (sidecar resources, PV allocation, WAL, gRPC stream).

## Why Fluent Bit specifically?

- **Tiny footprint** — ~2.5MB binary, ~30-50MB RAM under load. Runs as a sidecar without impacting build performance.
- **Disk buffering** — crash-safe with `mmap(2)`, survives container restarts.
- **S3 output plugin** — built-in, production-tested, handles retries/batching/gzip.
- **Kubernetes-native** — designed for sidecar deployment, understands pod lifecycle.
- **Battle-tested** — used by AWS (FireLens), GCP, and most Kubernetes log pipelines.
- **No external dependencies** — no Kafka, no Elasticsearch, no log aggregator service to manage.

Alternatives considered:
- **Vector** — similar capability but larger binary (~50MB). Good but heavier.
- **Filebeat** — Elastic-ecosystem specific. Heavier, Java-adjacent.
- **Custom Go agent** — would need to reimplement disk buffering, S3 batching, crash recovery. Not worth it when Fluent Bit does it perfectly.
