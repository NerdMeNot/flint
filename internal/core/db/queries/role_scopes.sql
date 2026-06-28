-- ────────────────────────────────────────────────────────────
-- Role permissions
-- ────────────────────────────────────────────────────────────

-- name: ListRolePermissions :many
SELECT object, action FROM role_permissions WHERE role_id = $1;

-- name: InsertRolePermission :exec
INSERT INTO role_permissions (role_id, object, action)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: DeleteRolePermissions :exec
DELETE FROM role_permissions WHERE role_id = $1;

-- ────────────────────────────────────────────────────────────
-- Role workspace scope
-- ────────────────────────────────────────────────────────────

-- name: ListRoleWorkspaceSlugs :many
SELECT w.slug FROM role_workspace_scope rws
JOIN workspaces w ON w.id = rws.workspace_id
WHERE rws.role_id = $1;

-- name: InsertRoleWorkspaceScope :exec
INSERT INTO role_workspace_scope (role_id, workspace_id)
SELECT $1, id FROM workspaces WHERE slug = $2
ON CONFLICT DO NOTHING;

-- name: DeleteRoleWorkspaceScopes :exec
DELETE FROM role_workspace_scope WHERE role_id = $1;

-- ────────────────────────────────────────────────────────────
-- Role environment scope
-- ────────────────────────────────────────────────────────────

-- name: ListRoleEnvironmentNames :many
SELECT e.name FROM role_environment_scope res
JOIN environments e ON e.id = res.environment_id
WHERE res.role_id = $1;

-- name: InsertRoleEnvironmentScope :exec
INSERT INTO role_environment_scope (role_id, environment_id)
SELECT $1, id FROM environments WHERE slug = $2
ON CONFLICT DO NOTHING;

-- name: DeleteRoleEnvironmentScopes :exec
DELETE FROM role_environment_scope WHERE role_id = $1;

-- ────────────────────────────────────────────────────────────
-- Role assignments
-- ────────────────────────────────────────────────────────────

-- name: ListAllRoleAssignments :many
SELECT subject, role_id FROM role_assignments;

-- name: ListAllRoleAssignmentsWithRole :many
SELECT ra.subject, r.slug AS role
FROM role_assignments ra
JOIN roles r ON r.id = ra.role_id
ORDER BY ra.subject;

-- name: ListRoleAssignmentsBySubject :many
SELECT subject, role_id FROM role_assignments WHERE subject = $1;

-- name: ListRoleAssignmentsByRole :many
SELECT subject, role_id FROM role_assignments WHERE role_id = $1;

-- name: InsertRoleAssignment :exec
-- Manual/authoritative assignment. Marks the row source='internal' so SSO group
-- sync will never remove it, even if an 'idp' row already existed.
INSERT INTO role_assignments (subject, role_id, source, created_at)
VALUES ($1, $2, 'internal', now())
ON CONFLICT (subject, role_id) DO UPDATE SET source = 'internal';

-- name: InsertIdpRoleAssignment :exec
-- IdP-derived assignment from a group→role mapping. Never downgrades an existing
-- (manual) 'internal' row.
INSERT INTO role_assignments (subject, role_id, source, created_at)
VALUES ($1, $2, 'idp', now())
ON CONFLICT (subject, role_id) DO NOTHING;

-- name: DeleteIdpRoleAssignment :exec
DELETE FROM role_assignments WHERE subject = $1 AND role_id = $2 AND source = 'idp';

-- name: ListIdpRoleAssignmentRoleIDs :many
SELECT role_id FROM role_assignments WHERE subject = $1 AND source = 'idp';

-- name: ListRoleIDsForGroups :many
SELECT DISTINCT role_id FROM sso_group_role_mappings
WHERE org_id = @org_id AND group_name = ANY(@groups::text[]);

-- name: CountRoleAssignments :one
SELECT COUNT(*) FROM role_assignments WHERE subject = $1;

-- name: DeleteRoleAssignment :exec
DELETE FROM role_assignments WHERE subject = $1 AND role_id = $2;
