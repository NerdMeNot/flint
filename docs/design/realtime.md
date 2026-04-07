# Real-Time Updates in Flint

## Overview

The run detail page needs real-time updates — step status transitions, log streaming, and run completion. Flint uses **SSE (Server-Sent Events)** as the primary mechanism with **polling as fallback**.

## Architecture

Flint uses an embedded Postgres-backed DAG executor (no Temporal, no external services). All workflow state lives in Postgres. The agent executes steps and reports back to the server.

```
Agent (executes step in container)
  → writes logs to LogSink (filesystem / S3)
  → calls POST /runs/:id/steps/:name/complete on Flint Server

Flint Server
  → updates step/run status in Postgres
  → engine advances DAG (next wave of steps)
  → publishes event to in-memory event bus
  → SSE handler pushes to connected browsers/TUI

Browser (EventSource)
  → receives SSE events
  → updates TanStack Query cache in-place
  → UI re-renders automatically
```

No pg_notify needed. The server that receives the agent's completion callback is the same process that holds the SSE connections. A simple in-memory pub/sub (Go channels) fans events out to subscribers.

## SSE Endpoints

### `GET /api/v1/runs/:id/events`

Streams run and step status changes for a specific run.

**Event types:**

```
event: step-status
data: {"step":"build","status":"running","startedAt":"2024-01-15T10:01:20Z"}

event: step-status
data: {"step":"build","status":"succeeded","finishedAt":"2024-01-15T10:01:53Z","duration":"33s"}

event: run-status
data: {"status":"succeeded","duration":"2m 34s","finishedAt":"2024-01-15T10:02:34Z"}

event: heartbeat
data: {}
```

- `step-status`: emitted when a step changes state (pending→queued→running→succeeded/failed)
- `run-status`: emitted when the overall run status changes
- `heartbeat`: sent every 15s to keep the connection alive through proxies

**Connection lifecycle:**
1. Client opens `EventSource('/api/v1/runs/:id/events')`
2. Server registers this connection in the in-memory subscriber map for this run ID
3. When the engine advances the DAG (step completes, new steps queued), it publishes to the event bus
4. SSE handler picks up events for this run and pushes to the client
5. Client disconnects when leaving the page → server cleans up subscriber
6. If connection drops, `EventSource` auto-reconnects with `Last-Event-ID` header

### `GET /api/v1/runs/:id/steps/:name/logs/stream`

Streams log lines for a specific step in real-time.

```
event: log
data: {"line":"[10:01:28] Compiling internal/handler...","seq":42}

event: log
data: {"line":"[10:01:35] Build succeeded","seq":43}

event: done
data: {"totalLines":43}
```

- `log`: a new log line with sequence number
- `done`: step has finished, no more logs coming
- Client sends `Last-Event-ID: 42` on reconnect → server replays from seq 43

## Log Storage

Logs are NOT stored in Postgres. They are high-volume, append-only, and read-sequentially — a terrible fit for a relational DB.

The `pkg/logsink` package provides the `LogSink` interface. The implementation is `S3Sink` — logs stored as JSONL objects in S3 (`s3://<bucket>/logs/<org>/<run>/<step>.jsonl`).

**Write path** (during step execution):
```
Agent runs step in container
  → captures stdout/stderr
  → streams lines to LogSink via agent API
  → S3Sink appends lines to the step's JSONL object
```

**Read path** (completed step — full content):
```
GET /runs/:id/steps/:name/logs
  → Server reads from LogSink
  → Returns { lines: "full content" }
```

**Stream path** (running step — real-time tail):
```
GET /runs/:id/steps/:name/logs/stream (SSE)
  → Server tails S3 object using Range GET (byte offset tracking)
  → Polls every 1s for new bytes appended to the JSONL object
  → Pushes new lines as SSE events
```

## In-Memory Event Bus

The server maintains a simple pub/sub for run events:

```go
type EventBus struct {
    mu          sync.RWMutex
    subscribers map[string][]chan RunEvent  // runID → channels
}

func (b *EventBus) Subscribe(runID string) <-chan RunEvent
func (b *EventBus) Unsubscribe(runID string, ch <-chan RunEvent)
func (b *EventBus) Publish(runID string, event RunEvent)
```

When the engine advances a run (step completes, new steps dispatched), it calls `eventBus.Publish(runID, event)`. All SSE handlers subscribed to that run ID receive the event and push it to their clients.

This is purely in-memory — no external message broker needed. If the server restarts, SSE connections drop and clients auto-reconnect. The initial SSE connection response includes current state, so no events are missed.

## Frontend Integration

### useRunEvents hook

```typescript
function useRunEvents(runId: string, enabled: boolean) {
  const queryClient = useQueryClient()

  useEffect(() => {
    if (!enabled) return

    const source = new EventSource(`/api/v1/runs/${runId}/events`)

    source.addEventListener('step-status', (e) => {
      const data = JSON.parse(e.data)
      queryClient.setQueryData(
        orpc.runs.steps.queryKey({ input: { runId } }),
        (old) => updateStepInCache(old, data)
      )
    })

    source.addEventListener('run-status', (e) => {
      const data = JSON.parse(e.data)
      queryClient.setQueryData(
        orpc.runs.get.queryKey({ input: { id: runId } }),
        (old) => ({ ...old, ...data })
      )
    })

    source.onerror = () => {
      // SSE failed — fall back to polling.
      source.close()
    }

    return () => source.close()
  }, [runId, enabled, queryClient])
}
```

### useLogStream hook

```typescript
function useLogStream(runId: string, stepName: string | null) {
  const [lines, setLines] = useState<string[]>([])
  const [done, setDone] = useState(false)

  useEffect(() => {
    if (!stepName) return

    const source = new EventSource(
      `/api/v1/runs/${runId}/steps/${stepName}/logs/stream`
    )

    source.addEventListener('log', (e) => {
      const data = JSON.parse(e.data)
      setLines((prev) => [...prev, data.line])
    })

    source.addEventListener('done', () => {
      setDone(true)
      source.close()
    })

    return () => source.close()
  }, [runId, stepName])

  return { lines, done }
}
```

### Fallback: Polling

If SSE is unavailable (detected by `onerror` firing on first connect), fall back to TanStack Query's `refetchInterval`:

```typescript
const { data: steps } = useQuery({
  ...orpc.runs.steps.queryOptions({ input: { runId } }),
  refetchInterval: sseAvailable ? false : 3000,
})
```

## Multi-Instance Scaling

For a single server instance: in-memory event bus is sufficient.

For multiple server instances behind a load balancer: the agent's step-completion request may hit a different instance than the one holding the user's SSE connection. Two options:

1. **Sticky sessions** — route the SSE connection to the same instance that handles the run. Simple but limits scaling.
2. **Redis pub/sub** — instances publish/subscribe to a shared Redis channel. Each instance's event bus subscribes to Redis and fans out to local SSE connections. This is the production scaling path but not needed on day one.

Start with in-memory. Add Redis when running multiple instances.

## TUI Integration

The TUI consumes the same SSE endpoints via Go's standard library:

```go
resp, _ := http.Get(serverURL + "/api/v1/runs/" + runID + "/events")
scanner := bufio.NewScanner(resp.Body)
for scanner.Scan() {
    line := scanner.Text()
    if strings.HasPrefix(line, "data: ") {
        // Parse and display
    }
}
```

Same real-time experience as the web UI.
