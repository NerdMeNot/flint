-- name: ListTeams :many
SELECT id, name, slug FROM teams ORDER BY name
LIMIT $1 OFFSET $2;

-- name: CreateTeam :one
INSERT INTO teams (org_id, name, slug) VALUES ($1, $2, $3) RETURNING id;

-- name: DeleteTeam :execrows
DELETE FROM teams WHERE id = $1;

-- name: GetOrCreateTeamBySlug :one
INSERT INTO teams (org_id, name, slug) VALUES ($1, $2, $3)
ON CONFLICT (org_id, slug) DO UPDATE SET name = teams.name
RETURNING id;

-- name: GetTeam :one
SELECT id, name, slug, COALESCE(source, 'internal')::text AS source, idp_group
FROM teams WHERE id = $1;

-- name: ListTeamsPaged :many
SELECT t.id, t.name, t.slug, COALESCE(t.source, 'internal')::text AS source,
       t.idp_group, COUNT(tm.user_id) AS member_count
FROM teams t LEFT JOIN team_members tm ON tm.team_id = t.id
WHERE t.org_id = $1 GROUP BY t.id ORDER BY t.name LIMIT $2 OFFSET $3;
