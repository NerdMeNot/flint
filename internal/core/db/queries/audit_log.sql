-- name: InsertAuditEntry :exec
INSERT INTO audit_log (org_id, user_id, action, resource_type, resource_id, ip_address)
VALUES ($1, $2, $3, $4, $5, $6::inet);

-- name: InsertAuditEntryWithMeta :exec
INSERT INTO audit_log (org_id, user_id, action, resource_type, resource_id, ip_address, metadata)
VALUES ($1, $2, $3, $4, $5, $6::inet, $7);

-- name: ListRecentSignIns :many
-- Recent SSO sign-in attempts (success + failure) for the sign-in diagnostics log.
SELECT al.user_id, u.email AS user_email, al.action, al.resource_id, al.metadata, CAST(COALESCE(host(al.ip_address), '') AS text) AS ip_address, al.created_at
FROM audit_log al
LEFT JOIN users u ON al.user_id = u.id
WHERE al.org_id = $1 AND al.action IN ('auth.login', 'auth.login.failed')
ORDER BY al.created_at DESC
LIMIT $2;

-- name: ListAuditLog :many
SELECT al.id, al.user_id, u.email AS user_email, al.action, al.resource_type,
       al.resource_id, al.metadata, al.ip_address, al.created_at
FROM audit_log al
LEFT JOIN users u ON al.user_id = u.id
WHERE al.org_id = $1
  AND (sqlc.narg('action')::text IS NULL OR al.action = sqlc.narg('action'))
ORDER BY al.created_at DESC
LIMIT $2;
