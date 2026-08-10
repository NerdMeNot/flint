-- name: InsertStep :exec
INSERT INTO steps (workflow_id, name, exec_type, status, wave, max_attempts,
    step_def, timeout_seconds, retry_backoff, retry_interval_seconds, on_failure)
VALUES ($1, $2, $3, 'pending', $4, $5, $6, $7, $8, $9, $10);

-- name: InsertSeededStep :exec
-- Inserts a step already in terminal 'succeeded' state, carrying a result copied
-- from a prior run. Used by re-run-failed / retry-from-step to skip work that
-- already succeeded while still satisfying downstream dependencies and expression
-- context (steps.<name>.<output>).
INSERT INTO steps (workflow_id, name, exec_type, status, wave, max_attempts,
    step_def, timeout_seconds, retry_backoff, retry_interval_seconds, on_failure,
    result, queued_at, started_at, finished_at)
VALUES ($1, $2, $3, 'succeeded', $4, $5, $6, $7, $8, $9, $10, $11, now(), now(), now());

-- name: LockStep :one
SELECT id, status, max_attempts, retry_backoff, retry_interval_seconds
FROM steps
WHERE workflow_id = $1 AND name = $2 AND attempt = $3
FOR UPDATE;

-- name: UpdateStepResult :exec
UPDATE steps SET status = $2, result = $3, finished_at = now() WHERE id = $1;

-- name: SetStepStatus :exec
UPDATE steps SET status = $2 WHERE id = $1;

-- name: SetStepQueued :exec
-- Clears execution timestamps so a re-queued step (throttled, undispatched, or
-- first queue) starts with a clean slate. Critical: a stale deadline_at left over
-- from an earlier claim would let the sweep time the step out prematurely.
UPDATE steps SET status = 'queued', queued_at = now(),
    deadline_at = NULL, started_at = NULL, dispatched_at = NULL
WHERE id = $1;

-- name: MarkStepDispatched :exec
-- Records that a claimed step was successfully handed to an executor. dispatched_at
-- distinguishes "running, dispatched" from "claimed but the worker died before
-- dispatch" — the latter is recovered by RequeueUndispatchedSteps. dispatch_handle
-- is the executor's opaque correlation id (machine executor: step_assignments.id).
UPDATE steps SET dispatched_at = now(), dispatch_handle = $2 WHERE id = $1;

-- name: RequeueUndispatchedSteps :execrows
-- Recovers steps that were claimed (status='running') but never dispatched — e.g.
-- the worker crashed between claiming and creating the Job. Re-queue (not fail):
-- the step never executed, so it deserves a fresh dispatch rather than a failure.
UPDATE steps SET status = 'queued', queued_at = now(),
    deadline_at = NULL, started_at = NULL, dispatched_at = NULL
WHERE status = 'running' AND dispatched_at IS NULL
    AND started_at < now() - make_interval(secs := sqlc.arg(grace_secs)::double precision);

-- name: SetStepSkipped :exec
UPDATE steps SET status = 'skipped', finished_at = now() WHERE id = $1;

-- name: CancelPendingSteps :many
-- Cancels every non-terminal step of a workflow and returns each affected step's
-- name, attempt, and prior status so the caller can record a per-step 'cancelled'
-- history event. The CTE captures old_status before the UPDATE overwrites it.
WITH affected AS (
    SELECT steps.id, steps.name, steps.attempt, steps.status AS old_status
    FROM steps
    WHERE steps.workflow_id = $1 AND steps.status NOT IN ('succeeded', 'failed', 'skipped', 'cancelled')
    FOR UPDATE
)
UPDATE steps SET status = 'cancelled', finished_at = now()
FROM affected
WHERE steps.id = affected.id
RETURNING affected.name, affected.attempt, affected.old_status;

-- name: LatestStepsByWorkflow :many
SELECT DISTINCT ON (name) id, name, status, attempt, on_failure,
    max_attempts, retry_backoff, retry_interval_seconds,
    step_def->>'if' AS if_condition,
    step_def->>'when' AS when_condition,
    step_def->'dependsOn' AS depends_on
FROM steps WHERE workflow_id = $1
ORDER BY name, attempt DESC;

-- name: ListStepsByWorkflow :many
SELECT DISTINCT ON (name) name, status, wave, attempt, max_attempts, result,
    exec_type, queued_at, started_at, finished_at,
    step_def->'dependsOn' AS depends_on
FROM steps WHERE workflow_id = $1
ORDER BY name, attempt DESC;

-- name: ClaimQueuedSteps :many
-- Claims queued steps whose workflow is 'running' (a single predicate that also
-- enforces pause: a paused workflow's steps are simply not claimable). FOR UPDATE
-- OF steps locks only the step rows — never the workflow row — so claiming cannot
-- contend with advanceWorkflow's FOR UPDATE on the workflow. The workflow status
-- read is best-effort: a pause committing after this read may let one already-
-- queued step dispatch, which is acceptable (pause halts new work, not in-flight).
UPDATE steps SET
    status = CASE WHEN exec_type IN ('gate', 'wait') THEN 'waiting' ELSE 'running' END,
    started_at = now(),
    deadline_at = now() + make_interval(secs := timeout_seconds)
WHERE id IN (
    SELECT steps.id FROM steps
    JOIN workflows w ON w.id = steps.workflow_id AND w.status = 'running'
    WHERE steps.status = 'queued'
    -- Ordered by when the step became ELIGIBLE, not by wave. Wave is a
    -- per-workflow depth with no meaning across workflows, so leading with it
    -- meant every newly-started run's wave-0 steps outranked an hour-old run's
    -- wave-5 step however long it had waited. Under sustained load that starves
    -- deep pipelines outright; below saturation it still hands the worst queue
    -- times to the longest runs. wave stays as a tiebreak so steps that became
    -- eligible in the same instant still go shallowest-first.
    ORDER BY steps.queued_at, steps.wave
    LIMIT $1
    FOR UPDATE OF steps SKIP LOCKED
)
RETURNING id, workflow_id, name, exec_type, attempt, step_def, status;

-- name: SetStepTaskToken :exec
UPDATE steps SET task_token = $2 WHERE id = $1;

-- name: SetStepTaskTokens :exec
-- Batch variant: stamp every claimed step's task token in ONE round-trip
-- instead of one UPDATE per step (the claim path's hottest write).
UPDATE steps SET task_token = t.token
FROM (SELECT unnest(sqlc.arg(ids)::uuid[]) AS id, unnest(sqlc.arg(tokens)::text[]) AS token) t
WHERE steps.id = t.id;

-- name: SetStepDispatchHandle :exec
UPDATE steps SET dispatch_handle = $2 WHERE id = $1;

-- name: CreateRetryStep :exec
-- The retry attempt is parked in 'retry_wait', NOT 'pending'. A 'pending' row
-- would be queued immediately by advanceWorkflow, bypassing the retry_backoff
-- timer. Only the fired retry_backoff timer (RequeueRetryStep) promotes it to
-- 'queued', so the backoff delay is actually honoured.
INSERT INTO steps (workflow_id, name, exec_type, status, wave, attempt,
    max_attempts, step_def, timeout_seconds, retry_backoff, retry_interval_seconds, on_failure)
SELECT s.workflow_id, s.name, s.exec_type, 'retry_wait', s.wave, sqlc.arg(new_attempt),
    s.max_attempts, s.step_def, s.timeout_seconds, s.retry_backoff, s.retry_interval_seconds, s.on_failure
FROM steps s WHERE s.id = sqlc.arg(source_step_id);

-- name: LockRunningStepByName :one
-- The live attempt of a running step, with its retry policy, locked for update.
-- Timers carry (workflow, step) but no attempt, so the attempt is resolved here.
-- Returning the policy is what lets a timeout honour `retry:` the same way an
-- agent-reported failure does — previously the timeout path updated the row
-- directly and the policy was never consulted.
SELECT id, status, attempt, max_attempts, retry_backoff, retry_interval_seconds
FROM steps
WHERE workflow_id = $1 AND name = $2 AND status = 'running'
ORDER BY attempt DESC
LIMIT 1
FOR UPDATE;

-- name: ClaimStaleRunningSteps :many
-- Sweep: running steps past their execution deadline, claimed in batches so the
-- caller can fail each one through the transition chokepoint AND schedule its
-- retry. This replaces a set-based UPDATE that failed every stale step in one
-- statement — correct, but with no seam to apply the step's retry policy, so a
-- swept step never retried however many attempts it had left.
SELECT id, workflow_id, name, attempt, max_attempts, retry_backoff, retry_interval_seconds
FROM steps
WHERE status = 'running' AND deadline_at < now()
ORDER BY deadline_at
LIMIT $1
FOR UPDATE SKIP LOCKED;

-- name: FailGateByTimeout :exec
UPDATE steps SET status = 'failed', result = sqlc.arg(result)::jsonb, finished_at = now()
WHERE workflow_id = sqlc.arg(workflow_id) AND name = sqlc.arg(step_name) AND status = 'waiting';

-- name: RequeueRetryStep :exec
-- Promotes the parked retry attempt (retry_wait) to queued once its backoff timer
-- fires. This is the ONLY path out of retry_wait.
WITH latest AS (
    SELECT s.id FROM steps s
    WHERE s.workflow_id = $1 AND s.name = $2 AND s.status = 'retry_wait'
    ORDER BY s.attempt DESC LIMIT 1
)
UPDATE steps SET status = 'queued', queued_at = now()
FROM latest WHERE steps.id = latest.id;

-- name: CompleteParentInvokeStep :exec
UPDATE steps SET status = sqlc.arg(new_status), result = sqlc.arg(result), finished_at = now()
WHERE workflow_id = sqlc.arg(workflow_id) AND name = sqlc.arg(step_name) AND status = 'running';

-- name: RecentlyFailedWorkflowIDs :many
SELECT DISTINCT workflow_id FROM steps
WHERE status = 'failed' AND finished_at >= now() - interval '10 seconds';

-- name: GetStepStatus :one
SELECT s.status FROM steps s
JOIN workflows w ON s.workflow_id = w.id
JOIN pipeline_runs pr ON w.run_id = pr.id
WHERE pr.id = $1 AND s.name = $2
ORDER BY s.attempt DESC LIMIT 1;

-- name: ListPendingGates :many
SELECT s.name AS step_name,
       s.step_def->'gate'->>'message' AS message,
       w.id AS workflow_id,
       pr.id AS run_id, pr.trigger_ref AS branch, pr.triggered_by,
       p.id AS project_id, p.display_name AS project_name, p.colour AS project_colour,
       s.created_at
FROM steps s
JOIN workflows w ON s.workflow_id = w.id
JOIN pipeline_runs pr ON w.run_id = pr.id
JOIN projects p ON pr.project_id = p.id
WHERE s.exec_type = 'gate' AND s.status = 'waiting'
ORDER BY s.created_at ASC
LIMIT 50;

-- name: GetStepByWorkflowAndName :one
SELECT id, workflow_id, name, exec_type, step_def FROM steps
WHERE workflow_id = $1 AND name = $2
ORDER BY attempt DESC LIMIT 1;

-- name: LockLatestStep :one
-- Row-locks the latest attempt of a step for an operator override (manual resolve).
SELECT id, status, attempt FROM steps
WHERE workflow_id = $1 AND name = $2
ORDER BY attempt DESC LIMIT 1
FOR UPDATE;

-- name: LockNextApprovedGate :one
-- Claims one waiting gate that has an unconsumed approval signal, locking the step
-- row (FOR UPDATE OF s SKIP LOCKED) so concurrent workers never process the same
-- gate. The caller consumes the signal, transitions the step, and advances — all
-- in the same transaction, so the effect is atomic and exactly-once. The
-- status='waiting' predicate is first-writer-wins: once approved or rejected, the
-- gate is no longer claimable here.
SELECT s.id AS step_id, s.workflow_id, s.name AS step_name, s.attempt, sig.id AS signal_id
FROM steps s
JOIN signals sig ON sig.workflow_id = s.workflow_id
    AND sig.signal_name = 'gate-' || s.name
    AND sig.consumed = false
WHERE s.status = 'waiting' AND s.exec_type = 'gate'
ORDER BY s.created_at
LIMIT 1
FOR UPDATE OF s SKIP LOCKED;

-- name: LockNextRejectedGate :one
-- Same as LockNextApprovedGate but for rejection signals. Returns the payload so
-- the caller can surface the rejection reason.
SELECT s.id AS step_id, s.workflow_id, s.name AS step_name, s.attempt, sig.id AS signal_id, sig.payload
FROM steps s
JOIN signals sig ON sig.workflow_id = s.workflow_id
    AND sig.signal_name = 'gate-reject-' || s.name
    AND sig.consumed = false
WHERE s.status = 'waiting' AND s.exec_type = 'gate'
ORDER BY s.created_at
LIMIT 1
FOR UPDATE OF s SKIP LOCKED;

-- name: LockNextSignaledWaitStep :one
-- Claims one waiting 'wait' step whose configured external signal has arrived
-- (step_def->'wait'->>'signal'). Same atomic lock-and-advance pattern as gates.
-- Returns the signal payload, which the caller captures into the step outputs so
-- downstream steps can reference steps.<name>.<key>.
SELECT s.id AS step_id, s.workflow_id, s.name AS step_name, s.attempt, sig.id AS signal_id, sig.payload
FROM steps s
JOIN signals sig ON sig.workflow_id = s.workflow_id
    AND sig.signal_name = COALESCE(NULLIF(s.step_def->'wait'->>'signal', ''), s.name)
    AND sig.consumed = false
WHERE s.status = 'waiting' AND s.exec_type = 'wait'
ORDER BY s.created_at
LIMIT 1
FOR UPDATE OF s SKIP LOCKED;

-- name: FailOrphanedWaitingSteps :execrows
-- Defense-in-depth backstop: a gate/wait step still 'waiting' although its timeout
-- timer already fired (handler regression or crash) is forced to failed so the
-- workflow can run onFailure steps / finish instead of wedging forever. With atomic
-- timer handling (LockNextDueTimer) this should never fire; it exists so a future
-- regression degrades to "recovered late" rather than "stuck".
UPDATE steps SET status = 'failed',
    result = jsonb_build_object('stepName', name, 'success', false,
        'error', 'wait timed out (backstop)')::jsonb,
    finished_at = now()
WHERE status = 'waiting' AND exec_type IN ('gate', 'wait')
AND EXISTS (
    SELECT 1 FROM timers t
    WHERE t.workflow_id = steps.workflow_id AND t.step_name = steps.name
    AND t.timer_type IN ('gate_timeout', 'wait_timeout') AND t.fired = true
);

-- name: ListGatesByStatus :many
-- Gate steps enriched with run/project/workspace context, filtered by step status
-- (the handler maps UI status names: pending→waiting, approved→succeeded,
-- rejected→failed). Powers GET /api/v1/gates.
SELECT s.name AS step_name,
       COALESCE(s.step_def->'gate'->>'message', '')::text AS message,
       pr.id AS run_id, pr.trigger_ref AS branch, pr.triggered_by,
       p.display_name AS project_name, p.colour AS project_colour,
       COALESCE(w.slug, '')::text AS workspace,
       COALESCE(s.step_def->'gate'->>'environment', '')::text AS environment,
       s.status, s.created_at,
       s.result->>'approvedBy' AS reviewed_by,
       s.finished_at AS reviewed_at
FROM steps s
JOIN workflows wf ON s.workflow_id = wf.id
JOIN pipeline_runs pr ON wf.run_id = pr.id
JOIN projects p ON pr.project_id = p.id
LEFT JOIN workspaces w ON w.id = p.workspace_id
WHERE s.exec_type = 'gate' AND s.status = sqlc.arg('status')
ORDER BY s.created_at ASC LIMIT 50;

-- name: QueueStats :one
-- One-shot queue visibility: how much work is waiting, how long the oldest
-- queued step has waited, and what's running/gated right now.
SELECT
    count(*) FILTER (WHERE status = 'queued')  AS queued,
    count(*) FILTER (WHERE status = 'running') AS running,
    count(*) FILTER (WHERE status = 'waiting') AS waiting_gates,
    coalesce(EXTRACT(EPOCH FROM (now() - min(queued_at) FILTER (WHERE status = 'queued')))::bigint, 0) AS oldest_queued_secs
FROM steps
WHERE status IN ('queued', 'running', 'waiting');

-- name: RunStepCosts :many
-- Per-step timing + compute requests for the run cost breakdown.
SELECT DISTINCT ON (s.name) s.name, s.exec_type, s.started_at, s.finished_at,
    s.step_def->'resources' AS resources
FROM steps s
JOIN workflows w ON w.id = s.workflow_id
WHERE w.run_id = $1
ORDER BY s.name, s.attempt DESC;
