-- name: InsertOutboxEvent :exec
INSERT INTO flint_outbox (event_type, payload, idempotency_key, max_attempts)
VALUES ($1, $2, $3, $4)
ON CONFLICT (idempotency_key) DO NOTHING;

-- name: ClaimOutboxBatch :many
-- claimed_at stamps when the event entered 'processing' so RecoverStaleOutboxEvents
-- can detect events stranded by a worker crash mid-delivery.
UPDATE flint_outbox SET status = 'processing', attempts = attempts + 1, claimed_at = now()
WHERE id IN (
    SELECT id FROM flint_outbox
    WHERE status = 'pending' AND process_after <= now()
    ORDER BY created_at
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
RETURNING id, event_type, payload, attempts, max_attempts;

-- name: RecoverStaleOutboxEvents :execrows
-- Returns events stranded in 'processing' (worker crashed between claim and
-- resolve/fail) back to 'pending' for redelivery.
--
-- The attempt budget is enforced HERE as well as in FailOutboxEvent. attempts is
-- incremented at claim, but only FailOutboxEvent converts an exhausted budget
-- into 'failed' — and an event whose delivery reliably strands the worker never
-- reaches it. Returning such an event to 'pending' unconditionally retried it
-- every five minutes forever: the one delivery that can hang a worker, retried
-- indefinitely, which is precisely the case the budget exists to stop.
UPDATE flint_outbox SET
    status = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END,
    last_error = COALESCE(last_error, 'delivery stranded in processing')
WHERE status = 'processing' AND claimed_at < now() - interval '5 minutes';

-- name: CleanFailedOutbox :exec
-- Terminally-failed events are kept for a window so an operator can see what
-- stopped delivering, then pruned. Without this they are the one outbox status
-- that accumulates forever — CleanResolvedOutbox only prunes successes.
DELETE FROM flint_outbox WHERE status = 'failed' AND created_at < now() - interval '30 days';

-- name: ResolveOutboxEvent :exec
UPDATE flint_outbox SET status = 'resolved', resolved_at = now()
WHERE id = $1;

-- name: FailOutboxEvent :exec
-- Exponential backoff with ±20% jitter (0.8–1.2×) to avoid a thundering herd of
-- webhook retries all firing in lockstep when an endpoint recovers. Mirrors the
-- step-retry jitter in backoffDuration.
UPDATE flint_outbox SET
    status = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END,
    last_error = $2,
    process_after = now() + make_interval(secs := power(2, attempts) * 5 * (0.8 + random() * 0.4))
WHERE id = $1;

-- name: CleanResolvedOutbox :exec
DELETE FROM flint_outbox WHERE status = 'resolved' AND resolved_at < now() - interval '7 days';
