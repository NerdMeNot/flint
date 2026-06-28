-- name: InsertScimToken :exec
INSERT INTO scim_tokens (org_id, token_hash) VALUES ($1, $2);

-- name: DeleteScimTokensForOrg :exec
DELETE FROM scim_tokens WHERE org_id = $1;

-- name: GetScimTokenOrg :one
SELECT org_id FROM scim_tokens WHERE token_hash = $1;

-- name: CountScimTokensForOrg :one
SELECT COUNT(*) FROM scim_tokens WHERE org_id = $1;

-- name: TouchScimToken :exec
UPDATE scim_tokens SET last_used_at = now() WHERE token_hash = $1;

-- name: GetUserByExternalID :one
SELECT id, email, external_id, name, is_active FROM users
WHERE org_id = $1 AND external_id = $2;

-- name: ScimUpsertUser :one
-- Provision/update a user from SCIM. Keyed on (org_id, external_id).
INSERT INTO users (org_id, email, external_id, name, is_active)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (org_id, external_id) DO UPDATE SET
    email = EXCLUDED.email, name = EXCLUDED.name, is_active = EXCLUDED.is_active
RETURNING id, created_at;

-- name: SetUserActive :exec
UPDATE users SET is_active = $2 WHERE id = $1;

-- name: ListScimUsers :many
SELECT id, email, external_id, name, is_active, created_at FROM users
WHERE org_id = $1 ORDER BY email;

-- name: GetTeamBySlug :one
SELECT id, name, slug, COALESCE(source, 'internal')::text AS source FROM teams
WHERE org_id = $1 AND slug = $2;

-- name: CreateScimTeam :one
INSERT INTO teams (org_id, name, slug, source) VALUES ($1, $2, $3, 'scim')
ON CONFLICT (org_id, slug) DO UPDATE SET name = EXCLUDED.name
RETURNING id;

-- name: UpdateTeamName :exec
UPDATE teams SET name = $2 WHERE id = $1;

-- name: ListScimTeams :many
SELECT t.id, t.name, t.slug FROM teams t
WHERE t.org_id = $1 AND t.source = 'scim' ORDER BY t.name;
