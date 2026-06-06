-- name: InsertOutboxEvent :exec
INSERT INTO flint_outbox (event_type, payload, idempotency_key, max_attempts)
VALUES ($1, $2, $3, $4);

-- name: ClaimOutboxBatch :many
UPDATE flint_outbox SET status = 'processing', attempts = attempts + 1
WHERE id IN (
    SELECT id FROM flint_outbox
    WHERE status = 'pending' AND process_after <= now()
    ORDER BY created_at
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
RETURNING id, event_type, payload, attempts, max_attempts;

-- name: ResolveOutboxEvent :exec
UPDATE flint_outbox SET status = 'resolved', resolved_at = now()
WHERE id = $1;

-- name: FailOutboxEvent :exec
UPDATE flint_outbox SET
    status = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END,
    last_error = $2,
    process_after = now() + make_interval(secs := power(2, attempts) * 5)
WHERE id = $1;

-- name: CleanResolvedOutbox :exec
DELETE FROM flint_outbox WHERE status = 'resolved' AND resolved_at < now() - interval '7 days';
