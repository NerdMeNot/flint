-- name: ListSSOGroupRoleMappings :many
SELECT m.id, m.group_name, m.role_id, r.slug AS role_slug, r.name AS role_name
FROM sso_group_role_mappings m
JOIN roles r ON r.id = m.role_id
WHERE m.org_id = $1
ORDER BY m.group_name, r.name;

-- name: InsertSSOGroupRoleMapping :exec
INSERT INTO sso_group_role_mappings (org_id, group_name, role_id)
VALUES ($1, $2, $3)
ON CONFLICT (org_id, group_name, role_id) DO NOTHING;

-- name: DeleteAllSSOGroupRoleMappings :exec
DELETE FROM sso_group_role_mappings WHERE org_id = $1;
