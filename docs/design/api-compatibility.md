# API compatibility audit — web UI ↔ Go server

Status: **living document** (Phase 1 of the server↔UI compatibility effort).
Last reconciled against `main` @ the `feat/api-compat-audit` branch.

## How the contract actually works

```
browser ──oRPC(/api/rpc)──▶ TanStack Start appRouter ──REST(/api/v1/*)──▶ Go server (Hertz)
                            web/src/lib/api/router.ts    web/src/lib/api/backend.ts
```

- The **Go server speaks plain REST** at `/api/v1/*`. There is **no oRPC on the
  server** — oRPC is a client/SSR construct. **The REST API is the canonical
  contract.**
- `web/src/lib/api/backend.ts` is a **raw pass-through**: it does `fetch(...).json()`
  with **zero shape translation**. Therefore a server response must **match the
  UI's zod/oRPC type exactly**, or the handler in `router.ts` must explicitly map
  it. Today most handlers do neither — they declare a `backendGet<T>()` type that
  is *assumed* and never validated, so drift fails silently (and is masked by the
  client mock in `auto`/`mock` mode).

This is why the UI "works" today (mock mode) but has latent breakage in `live`
mode. This doc enumerates the drift; Phase 2 fixes it.

## Verdict summary

| Resource | Endpoint(s) | Verdict |
| --- | --- | --- |
| PipelineRun | `runs.list`, `runs.get` | ⚠️ missing `steps[]`, `startedAtTs`, `finishedAtTs`, `errorMessage` |
| PipelineStep | `runs.steps` | ⚠️ missing `scheduledAt` |
| Step logs | `runs.stepLogs` | ❌ shape mismatch (`{lines:string}` vs `{lines:LogLine[],…}`) |
| Combined logs | `runs.logs` | ❌ server endpoint missing |
| Project | `projects.list/get` | ⚠️ missing `health`, `pipelineCount`, `pipelineErrors` |
| DashboardSummary | `stats.get` | ⚠️ `successRate` fraction vs percent (semantic) |
| WorkflowRun | `workflows.list/get` | ⚠️ missing `name`, `stepCount`, `duration` |
| Workflows trigger | `workflows.trigger` | ⚠️ body encoding (`{definition}` vs raw YAML) |
| Workspace | `workspaces.list` | ⚠️ missing `projectCount` |
| Session | `auth.sessions.list` | ⚠️ missing `current` |
| Gate | `gates.*` | ✅ match (incl. status mapping) |
| Paginated&lt;T&gt; | all lists | ✅ `{items,nextCursor}` consistent |
| Enums | run/step/gate/trigger status | ✅ values align (gate maps DB→UI server-side) |
| Spelling/timestamps | colour, RFC3339 strings | ✅ consistent (epoch-ms variants missing — see below) |
| ~16 other CRUD resources | env vars, teams, users, roles, api-keys, tokens, runners, forge, audit, org, auth.me, views, tags, search | ✅ match (spot-checked) |

Field-level tables follow. References are `file:line` on both sides.

---

## P0 — breaks the UI in live mode

### 1. `runs.list` / `runs.get` — `PipelineRun.steps[]` missing
The run feed renders stage "pips" from `run.steps` (`RunRow.tsx`). Server
`runResponse` (`internal/platform/server/routes.go:42`) does not include steps.
- UI: `PipelineRunSchema.steps` = optional `RunStepSummary[]` (`types.ts:46`).
- Server: no `steps` field; steps are a separate `runs.steps` call.
- **Fix:** populate `steps: [{name,status}]` on the run list/detail responses
  (join the steps table). Medium effort.

### 2. `runs.steps` — `PipelineStep.scheduledAt` missing
The new timeline/spine shows runner-queue wait = `scheduledAt → startedAt`.
- UI: `PipelineStepSchema.scheduledAt` optional ISO (`types.ts:115`).
- Server: `engine.StepState` (`internal/core/engine/engine.go:169`) has no
  `scheduledAt`.
- **Fix:** track the scheduled/ready timestamp in the engine step row and emit it.
  Higher effort (engine surface). If the column already exists (claim time), just
  expose it; otherwise the spine's queue segment stays empty in live mode.

### 3. `runs.stepLogs` — log shape mismatch
- UI handler types `{ lines: string }` (`router.ts:288`) and the log view does
  `logs.split('\n')`.
- Server `handleGetStepLogs` returns `{ lines: []logsink.LogLine, hasMore, complete }`
  (`internal/platform/server/log_handlers.go:60`).
- **Fix (chosen):** map in the `router.ts` handler — join `lines[].content` into a
  string (keep the server structured). Optionally later: have the UI consume
  structured lines (timestamps/levels). Low effort.

### 4. `runs.logs` (All output) — server endpoint missing
- UI calls `GET /runs/:id/logs` → `{ logs: Record<string,string> }` (`router.ts:301`).
- Server has only per-step logs; no combined route (`routes.go`).
- **Fix:** add `GET /api/v1/runs/:id/logs` aggregating each started step's log text
  from the `LogSink` into a `{ logs: {stepName: text} }` map. Low effort.

---

## P1 — feature gaps / wrong values

### 5. `PipelineRun.startedAtTs` / `finishedAtTs` missing (epoch ms)
The runs time-range filter and adaptive timestamps use epoch-ms fields.
- UI: optional `number` (`types.ts:63-64`); used for `from`/`to` filtering
  (`router.ts:255`) and `formatTimeline`.
- Server: only RFC3339 strings; no `*Ts`.
- **Fix:** add `startedAtTs`/`finishedAtTs` via `.UnixMilli()` alongside the
  strings. Trivial. (Until then, time-range filtering only works in mock mode.)

### 6. `PipelineRun.errorMessage` missing
- UI: optional string (`types.ts`). Server omits it.
- **Fix:** aggregate the failing step's error onto the run response.

### 7. `Project.health` / `pipelineCount` / `pipelineErrors` missing
Dashboard "needs attention" + project triage cards read `project.health`
(`ProjectHealth { recentRuns[], passRate, failingNow, totalRuns }`) and pipeline
counts.
- UI: `types.ts:82` (`ProjectHealthSchema`), `types.ts:91` (`ProjectSchema`).
- Server `projectResponse` (`routes.go:28`) has none of these.
- **Fix:** compute per-project health (recent run statuses + pass rate) and
  pipeline counts. Higher effort (aggregation). Until then the dashboard
  attention strip / health bars are empty in live mode.

### 8. `stats.successRate` — fraction vs percent (semantic)
- Server returns a **fraction 0..1** (`routes.go:229`); the dashboard renders
  `{stats.successRate}%` → would show `0.95%`.
- **Fix:** decide one unit. Recommend server returns **percent (0-100)** to match
  the UI's `%` rendering, or the UI multiplies by 100. Pick server-canonical.

### 9. Workflows — `name`/`stepCount`/`duration` + trigger encoding
- `workflows.list`/`get`: UI `WorkflowRun` wants `name`, `stepCount`, `duration`;
  server `/workflows/runs` omits them (`internal/products/workflows/api.go:205`).
- `workflows.trigger`: UI posts `{ definition }`; server wants a **raw YAML body**.
- **Fix:** add the fields server-side; align trigger to send raw YAML (or add a
  server envelope) — server-canonical.

---

## P2 — minor

- **Workspace.projectCount** missing (`rbac_handlers.go:32`) — add a COUNT.
- **Session.current** missing (`auth_handlers.go` / `sessions.sql.go:162`) — flag
  the request's own session.

---

## Real-time

| Concern | Today | Target |
| --- | --- | --- |
| Step logs (live) | server SSE exists at `GET /runs/:id/steps/:step/logs/stream` (`log_handlers.go`), **UI doesn't use it** — one-shot fetch only | UI tails via SSE |
| Run/step state | **UI polls** `runs.get` + `runs.steps` every 4s (`ci.runs.$id.tsx`); no server push | new `GET /runs/:id/stream` SSE + drop polling |

**Hard constraint:** browser `EventSource` **cannot send `Authorization` headers**.
Both SSE routes sit behind `requirePermission` (Bearer/API-key) today, so the
browser cannot authenticate them as-is. The auth middleware must accept a **JWT
session cookie** and/or a **short-lived token query param** for SSE routes
(`internal/platform/server/middleware.go`). Tracked in Phase 4.

---

## Contract test

`internal/platform/server/contract_test.go` validates that each `/api/v1`
response contains the fields the UI requires. It is **skipped unless
`FLINT_CONTRACT_URL` points at a running server** (a demo-mode server, Phase 3),
so it becomes fully runnable once demo mode lands and then guards against drift in
CI. The required-field expectations mirror `web/src/lib/api/types.ts`; update both
together when the contract changes.

---

## Fix backlog (feeds Phase 2)

1. Add `GET /runs/:id/logs` (combined). *(P0-4)*
2. Map `runs.stepLogs` to `{lines:string}` in `router.ts`. *(P0-3)*
3. Add `startedAtTs`/`finishedAtTs` to run responses. *(P1-5)*
4. Add `steps[]` to run list/detail. *(P0-1)*
5. Emit `scheduledAt` on steps. *(P0-2)*
6. Add `errorMessage` to run. *(P1-6)*
7. Add `health`/`pipelineCount`/`pipelineErrors` to project. *(P1-7)*
8. Fix `successRate` unit. *(P1-8)*
9. Workflows `name`/`stepCount`/`duration` + trigger encoding. *(P1-9)*
10. Workspace `projectCount`, Session `current`. *(P2)*
11. Remove `as any` casts in `router.ts` once shapes align.
