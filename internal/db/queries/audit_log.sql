-- name: ListAuditLog :many
SELECT al.id, al.user_id, u.email AS user_email, al.action, al.resource_type,
       al.resource_id, al.metadata, al.ip_address, al.created_at
FROM audit_log al
LEFT JOIN users u ON al.user_id = u.id
WHERE al.org_id = $1
  AND (sqlc.narg('action')::text IS NULL OR al.action = sqlc.narg('action'))
ORDER BY al.created_at DESC
LIMIT $2;
