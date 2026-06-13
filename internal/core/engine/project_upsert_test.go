package engine_test

import (
	"context"
	"testing"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestUpsertProject_WorkspacePlacement exercises the workspace-placement logic
// in UpsertProject: a declared workspace wins (and is created on the fly), an
// absent one is inferred from the repo owner, and the CRD is authoritative so a
// later change re-homes the project.
func TestUpsertProject_WorkspacePlacement(t *testing.T) {
	pool, q := setupTestDB(t)
	ctx := context.Background()

	// Self-contained seed with a UNIQUE forge display_name — the shared
	// 'test-forge' used elsewhere collides across tests on the unique index,
	// which would make UpsertProject resolve a different org's connection.
	orgID := uuid.NewString()
	forgeRef := "wsplace-" + orgID[:8]
	_, err := pool.Exec(ctx, `INSERT INTO orgs (id, name, slug) VALUES ($1, $2, $3)`,
		orgID, "wsplace-"+orgID[:8], "wsplace-"+orgID[:8])
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO forge_connections (id, org_id, forge_type, display_name, webhook_secret, credentials_enc)
		 VALUES ($1, $2, 'github', $3, 'secret', $4)`,
		uuid.NewString(), orgID, forgeRef, []byte{})
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO workspaces (id, org_id, name, slug, is_default) VALUES ($1, $2, 'Unsorted', 'unsorted-'||$3, true)`,
		uuid.NewString(), orgID, orgID[:8])
	require.NoError(t, err)

	placement := func(repo string) (slug string, inferred bool) {
		err := pool.QueryRow(ctx,
			`SELECT w.slug, p.workspace_inferred
			 FROM projects p
			 JOIN workspaces w ON w.id = p.workspace_id
			 JOIN forge_connections f ON f.id = p.forge_id
			 WHERE p.repo_path = $1 AND f.org_id = $2`, repo, orgID).Scan(&slug, &inferred)
		require.NoError(t, err)
		return
	}

	// Unique repo owners per test run so inferred workspace slugs don't collide
	// with other tests' orgs (workspaces are unique per (org_id, slug), so this
	// is really only for clarity).
	acme := "acme" + orgID[:6]
	globex := "globex" + orgID[:6]

	base := func(repo, workspace string) db.UpsertProjectParams {
		return db.UpsertProjectParams{
			ForgeRef: forgeRef, RepoPath: repo, RepoUrl: "https://example.com/" + repo,
			Colour: "#6366f1", DefaultBranch: "main", PipelineSource: []byte(`{}`),
			Tags: []string{}, Workspace: workspace,
		}
	}

	// 1. Declared workspace — created on the fly, not inferred.
	declaredWs := "payments-" + orgID[:6]
	_, err = q.UpsertProject(ctx, base(acme+"/checkout", declaredWs))
	require.NoError(t, err)
	slug, inferred := placement(acme + "/checkout")
	require.Equal(t, declaredWs, slug)
	require.False(t, inferred)

	// 2. No workspace declared — inferred from the repo owner.
	_, err = q.UpsertProject(ctx, base(globex+"/api", ""))
	require.NoError(t, err)
	slug, inferred = placement(globex + "/api")
	require.Equal(t, globex, slug)
	require.True(t, inferred)

	// 3. Re-declare with an explicit workspace — re-homed, no longer inferred.
	rehomeWs := "platform-" + orgID[:6]
	_, err = q.UpsertProject(ctx, base(globex+"/api", rehomeWs))
	require.NoError(t, err)
	slug, inferred = placement(globex + "/api")
	require.Equal(t, rehomeWs, slug)
	require.False(t, inferred)
}
