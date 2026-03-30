-- name: AddTeamMember :exec
INSERT INTO team_members (team_id, user_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: RemoveTeamMember :execrows
DELETE FROM team_members WHERE team_id = $1 AND user_id = $2;

-- name: RemoveUserFromAllTeams :execrows
DELETE FROM team_members WHERE user_id = $1;

-- name: ListTeamMembers :many
SELECT u.id, u.email, u.name FROM users u
JOIN team_members tm ON tm.user_id = u.id
WHERE tm.team_id = $1;

-- name: ListUserTeams :many
SELECT t.id, t.name, t.slug FROM teams t
JOIN team_members tm ON tm.team_id = t.id
WHERE tm.user_id = $1;

-- name: ListUserTeamIDs :many
SELECT team_id FROM team_members WHERE user_id = $1;

-- name: IsUserInTeamBySlug :one
SELECT EXISTS(
    SELECT 1 FROM team_members tm
    JOIN teams t ON t.id = tm.team_id
    WHERE t.org_id = $1 AND t.slug = $2 AND tm.user_id = $3
) AS is_member;
