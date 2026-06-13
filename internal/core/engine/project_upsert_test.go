package engine_test

import (
	"context"
	"testing"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/stretchr/testify/require"
)

// TestUpsertProject_WorkspacePlacement exercises the workspace-placement logic
// in UpsertProject: a declared workspace wins (and is created on the fly), an
// absent one is inferred from the repo owner, and the CRD is authoritative so a
// later change re-homes the project.
func TestUpsertProject_WorkspacePlacement(t *testing.T) {
	pool, q := setupTestDB(t)
	ctx := context.Background()
	orgID, _ := seedOrgAndProject(t, pool) // sets up org + 'test-forge' + a default ws

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

	base := func(repo, workspace string) db.UpsertProjectParams {
		return db.UpsertProjectParams{
			ForgeRef: "test-forge", RepoPath: repo, RepoUrl: "https://example.com/" + repo,
			Colour: "#6366f1", DefaultBranch: "main", PipelineSource: []byte(`{}`),
			Tags: []string{}, Workspace: workspace,
		}
	}

	// 1. Declared workspace — created on the fly, not inferred.
	_, err := q.UpsertProject(ctx, base("acme/checkout", "payments"))
	require.NoError(t, err)
	slug, inferred := placement("acme/checkout")
	require.Equal(t, "payments", slug)
	require.False(t, inferred)

	// 2. No workspace declared — inferred from the repo owner.
	_, err = q.UpsertProject(ctx, base("globex/api", ""))
	require.NoError(t, err)
	slug, inferred = placement("globex/api")
	require.Equal(t, "globex", slug)
	require.True(t, inferred)

	// 3. Re-declare with an explicit workspace — re-homed, no longer inferred.
	_, err = q.UpsertProject(ctx, base("globex/api", "platform"))
	require.NoError(t, err)
	slug, inferred = placement("globex/api")
	require.Equal(t, "platform", slug)
	require.False(t, inferred)
}
