-- name: GetOrg :one
SELECT id, name, slug FROM orgs LIMIT 1;

-- name: GetOrCreateDefaultOrg :one
INSERT INTO orgs (name, slug, temporal_namespace)
VALUES ('default', 'default', 'default')
ON CONFLICT (slug) DO UPDATE SET name = orgs.name
RETURNING id;
