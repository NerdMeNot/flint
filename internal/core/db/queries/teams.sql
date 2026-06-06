-- name: ListTeams :many
SELECT id, name, slug FROM teams ORDER BY name;

-- name: CreateTeam :one
INSERT INTO teams (org_id, name, slug) VALUES ($1, $2, $3) RETURNING id;

-- name: DeleteTeam :execrows
DELETE FROM teams WHERE id = $1;

-- name: GetOrCreateTeamBySlug :one
INSERT INTO teams (org_id, name, slug) VALUES ($1, $2, $3)
ON CONFLICT (org_id, slug) DO UPDATE SET name = teams.name
RETURNING id;
