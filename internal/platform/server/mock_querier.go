package server

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/NerdMeNot/flint/internal/platform/config"
	"github.com/NerdMeNot/flint/pkg/forge"
	"github.com/jackc/pgx/v5/pgtype"
)

// mockQuerier serves canned data with no database. It embeds db.Querier (a nil
// interface) so it satisfies the type at compile time; the ~50 methods the
// display + mutation handlers use are implemented below, and the auth-flow tail
// (device codes, MFA, sessions, login) is never reached because mock mode
// bypasses authentication.
type mockQuerier struct {
	db.Querier
	data *mockData
}

var errNotFound = errors.New("not found")

func sp(s string) *string { return &s }

func i4(v int32, valid bool) pgtype.Int4 { return pgtype.Int4{Int32: v, Valid: valid} }

// ── org / stats ─────────────────────────────────────────────────────────────

func (q *mockQuerier) GetOrCreateDefaultOrg(context.Context) (string, error) { return mockOrgID, nil }

func (q *mockQuerier) GetOrg(context.Context) (db.GetOrgRow, error) {
	return db.GetOrgRow{ID: mockOrgID, Name: "Acme", Slug: "acme", ConcurrencyLimit: 10}, nil
}

func (q *mockQuerier) GetRunStats(context.Context, string) (db.GetRunStatsRow, error) {
	return db.GetRunStatsRow{TotalRuns: 1284, SuccessRuns: 1147, RunsToday: 37, AvgDurationMs: 168000}, nil
}

func (q *mockQuerier) CountActiveProjects(context.Context) (int64, error) {
	return int64(len(q.data.projects)), nil
}

func (q *mockQuerier) CountPendingGates(context.Context) (int64, error) {
	var n int64
	for i := range q.data.runs {
		if q.data.runs[i].gate {
			n++
		}
	}
	return n, nil
}

// ── projects ────────────────────────────────────────────────────────────────

func (q *mockQuerier) ListProjectsWithLastRun(_ context.Context, arg db.ListProjectsWithLastRunParams) ([]db.ListProjectsWithLastRunRow, error) {
	out := []db.ListProjectsWithLastRunRow{}
	for _, p := range q.data.projects {
		if len(arg.Workspaces) > 0 && !contains(arg.Workspaces, p.workspace) {
			continue
		}
		if len(arg.Tags) > 0 && !overlaps(arg.Tags, p.tags) {
			continue
		}
		row := db.ListProjectsWithLastRunRow{
			ID: p.id, Name: p.name, RepoPath: p.repo, Workspace: p.workspace,
			Colour: p.colour, Tags: p.tags, CreatedAt: q.data.boot.Add(-720 * time.Hour),
		}
		if lr := q.data.latestRun(p.id); lr != nil {
			row.LastRunID = lr.id
			row.LastRunStatus = q.data.effectiveStatus(lr)
			row.LastRunBranch = sp(lr.branch)
			row.LastRunTriggeredBy = sp(lr.by)
			row.LastRunStartedAt = time.Now().Add(-lr.startedAgo)
			row.LastRunDurationMs = i4(lr.durationMs, lr.durationMs > 0)
		}
		out = append(out, row)
	}
	return out, nil
}

func (q *mockQuerier) GetProjectBasic(_ context.Context, id string) (db.GetProjectBasicRow, error) {
	p := q.data.project(id)
	if p == nil {
		return db.GetProjectBasicRow{}, errNotFound
	}
	return db.GetProjectBasicRow{
		ID: p.id, Name: p.name, RepoPath: p.repo, Workspace: p.workspace,
		Colour: p.colour, Tags: p.tags, CreatedAt: q.data.boot.Add(-720 * time.Hour),
	}, nil
}

func (q *mockQuerier) GetProjectWorkspaceSlug(_ context.Context, id string) (string, error) {
	if p := q.data.project(id); p != nil {
		return p.workspace, nil
	}
	return "", nil
}

func (q *mockQuerier) GetProjectRepoInfo(_ context.Context, id string) (db.GetProjectRepoInfoRow, error) {
	p := q.data.project(id)
	if p == nil {
		return db.GetProjectRepoInfoRow{}, errNotFound
	}
	return db.GetProjectRepoInfoRow{RepoPath: p.repo, OrgID: mockOrgID, PipelinePath: ".flint/"}, nil
}

func (q *mockQuerier) ProjectHealthByOrg(context.Context, string) ([]db.ProjectHealthByOrgRow, error) {
	out := make([]db.ProjectHealthByOrgRow, 0, len(q.data.projects))
	for _, p := range q.data.projects {
		out = append(out, db.ProjectHealthByOrgRow{
			ProjectID: p.id, RecentStatuses: p.recent,
			TotalRuns: int64(p.total), SucceededRuns: int64(p.succeeded),
		})
	}
	return out, nil
}

func (q *mockQuerier) ProjectHealthByID(_ context.Context, id *string) (db.ProjectHealthByIDRow, error) {
	if id != nil {
		if p := q.data.project(*id); p != nil {
			return db.ProjectHealthByIDRow{RecentStatuses: p.recent, TotalRuns: int64(p.total), SucceededRuns: int64(p.succeeded)}, nil
		}
	}
	return db.ProjectHealthByIDRow{}, nil
}

func (q *mockQuerier) SearchProjects(_ context.Context, arg db.SearchProjectsParams) ([]db.SearchProjectsRow, error) {
	out := []db.SearchProjectsRow{}
	pat := patternText(arg.Pattern)
	for _, p := range q.data.projects {
		if pat == "" || strings.Contains(strings.ToLower(p.name+p.repo), pat) {
			out = append(out, db.SearchProjectsRow{ID: p.id, Name: p.name, RepoPath: p.repo, Colour: p.colour})
		}
	}
	return out, nil
}

func (q *mockQuerier) UpdateProjectTags(context.Context, db.UpdateProjectTagsParams) error { return nil }

// ── runs ────────────────────────────────────────────────────────────────────

func (q *mockQuerier) ListRunsFiltered(_ context.Context, arg db.ListRunsFilteredParams) ([]db.ListRunsFilteredRow, error) {
	// Gather matches, then sort most-recent-first (smallest startedAgo) with an
	// id tiebreak — mirroring the SQL's `ORDER BY started_at DESC, id DESC`.
	matches := []*mockRun{}
	for i := range q.data.runs {
		r := &q.data.runs[i]
		if arg.ProjectID != "" && r.projectID != arg.ProjectID {
			continue
		}
		if arg.Status != "" && q.data.effectiveStatus(r) != arg.Status {
			continue
		}
		matches = append(matches, r)
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].startedAgo != matches[j].startedAgo {
			return matches[i].startedAgo < matches[j].startedAgo
		}
		return matches[i].id > matches[j].id
	})

	// Keyset cursor: resume after the row whose id matches the cursor.
	start := 0
	if arg.CursorID != "" {
		for k, r := range matches {
			if r.id == arg.CursorID {
				start = k + 1
				break
			}
		}
	}

	out := []db.ListRunsFilteredRow{}
	for k := start; k < len(matches); k++ {
		out = append(out, q.runRow(matches[k], q.data.effectiveStatus(matches[k])))
		if arg.Lim > 0 && int32(len(out)) >= arg.Lim {
			break
		}
	}
	return out, nil
}

func (q *mockQuerier) GetRunDetail(_ context.Context, id string) (db.GetRunDetailRow, error) {
	r := q.data.run(id)
	if r == nil {
		return db.GetRunDetailRow{}, errNotFound
	}
	x := q.runRow(r, q.data.effectiveStatus(r))
	return db.GetRunDetailRow(x), nil // identical fields
}

func (q *mockQuerier) runRow(r *mockRun, status string) db.ListRunsFilteredRow {
	p := q.data.project(r.projectID)
	started := time.Now().Add(-r.startedAgo)
	row := db.ListRunsFilteredRow{
		ID: r.id, Status: status, StartedAt: started,
		DurationMs: i4(r.durationMs, r.durationMs > 0),
		WorkflowFile: sp("ci.yaml"), Branch: sp(r.branch), TriggerType: r.trigger,
		CommitSha: sp(r.sha), CommitMessage: sp(r.msg), TriggeredBy: sp(r.by),
		ProjectID: r.projectID, RepoPath: "", Steps: q.data.stepSummaryJSON(r),
	}
	if r.env != "" {
		row.Environment = sp(r.env)
	}
	if r.durationMs > 0 {
		fin := started.Add(time.Duration(r.durationMs) * time.Millisecond)
		row.FinishedAt = &fin
	}
	if status == "failed" {
		row.ErrorMessage = sp("build failed: command exited 1")
	}
	if p != nil {
		row.ProjectName = sp(p.name)
		row.ProjectColour = p.colour
		row.RepoPath = p.repo
	}
	return row
}

func (q *mockQuerier) ListRunsByProject(_ context.Context, arg db.ListRunsByProjectParams) ([]db.ListRunsByProjectRow, error) {
	out := []db.ListRunsByProjectRow{}
	for i := range q.data.runs {
		r := &q.data.runs[i]
		if arg.ProjectID != nil && r.projectID != *arg.ProjectID {
			continue
		}
		started := time.Now().Add(-r.startedAgo)
		row := db.ListRunsByProjectRow{
			ID: r.id, WorkflowFile: sp("ci.yaml"), TriggerType: r.trigger,
			TriggerRef: sp(r.branch), CommitSha: sp(r.sha), CommitMessage: sp(r.msg),
			TriggeredBy: sp(r.by), Status: q.data.effectiveStatus(r), StartedAt: started,
			DurationMs: i4(r.durationMs, r.durationMs > 0),
		}
		out = append(out, row)
		if arg.Limit > 0 && int32(len(out)) >= arg.Limit {
			break
		}
	}
	return out, nil
}

func (q *mockQuerier) SearchRuns(_ context.Context, arg db.SearchRunsParams) ([]db.SearchRunsRow, error) {
	out := []db.SearchRunsRow{}
	pat := patternText(arg.Pattern)
	for i := range q.data.runs {
		r := &q.data.runs[i]
		if pat == "" || strings.Contains(strings.ToLower(r.branch+r.sha), pat) {
			p := q.data.project(r.projectID)
			row := db.SearchRunsRow{ID: r.id, Status: q.data.effectiveStatus(r), Branch: sp(r.branch), CommitSha: sp(r.sha)}
			if p != nil {
				row.ProjectName = p.name
				row.ProjectColour = p.colour
			}
			out = append(out, row)
		}
	}
	return out, nil
}

func (q *mockQuerier) GetRunOrgID(context.Context, string) (string, error)     { return mockOrgID, nil }
func (q *mockQuerier) GetRunWorkflowID(_ context.Context, id string) (*string, error) {
	if q.data.run(id) == nil {
		return nil, errNotFound
	}
	return sp(id), nil // mock uses runID as workflowID
}

func (q *mockQuerier) GetRunScope(_ context.Context, id string) (db.GetRunScopeRow, error) {
	if r := q.data.run(id); r != nil {
		if p := q.data.project(r.projectID); p != nil {
			return db.GetRunScopeRow{WorkspaceSlug: p.workspace, Environment: r.env}, nil
		}
	}
	return db.GetRunScopeRow{}, nil
}

func (q *mockQuerier) GetWorkflowDAGWaves(_ context.Context, workflowID string) ([]byte, error) {
	if r := q.data.run(workflowID); r != nil {
		return q.data.dagWaves(r), nil
	}
	return []byte("[]"), nil
}

func (q *mockQuerier) GetStepStatus(_ context.Context, arg db.GetStepStatusParams) (string, error) {
	if r := q.data.run(arg.ID); r != nil {
		for _, s := range q.data.steps(r) {
			if s.Name == arg.Name {
				return s.Status, nil
			}
		}
	}
	return "pending", nil
}

// ── gates ───────────────────────────────────────────────────────────────────

func (q *mockQuerier) ListGatesByStatus(_ context.Context, status string) ([]db.ListGatesByStatusRow, error) {
	out := []db.ListGatesByStatusRow{}
	if status != "waiting" {
		return out, nil
	}
	for i := range q.data.runs {
		r := &q.data.runs[i]
		if !r.gate {
			continue
		}
		p := q.data.project(r.projectID)
		row := db.ListGatesByStatusRow{
			StepName: "approve-production", Message: "Approve production deploy?",
			RunID: r.id, Branch: sp(r.branch), TriggeredBy: sp(r.by),
			Workspace: "", Environment: r.env, Status: "waiting",
			CreatedAt: time.Now().Add(-r.startedAgo),
		}
		if p != nil {
			row.ProjectName = sp(p.name)
			row.ProjectColour = p.colour
			row.Workspace = p.workspace
		}
		out = append(out, row)
	}
	return out, nil
}

// ── workspaces ──────────────────────────────────────────────────────────────

func (q *mockQuerier) ListWorkspacesWithCounts(context.Context, db.ListWorkspacesWithCountsParams) ([]db.ListWorkspacesWithCountsRow, error) {
	counts := map[string]int64{}
	for _, p := range q.data.projects {
		counts[p.workspace]++
	}
	base := q.data.boot.Add(-1000 * time.Hour)
	return []db.ListWorkspacesWithCountsRow{
		{ID: "ws-platform", Name: "Platform", Slug: "platform", CreatedAt: base, ProjectCount: counts["platform"]},
		{ID: "ws-frontend", Name: "Frontend", Slug: "frontend", CreatedAt: base, ProjectCount: counts["frontend"]},
		{ID: "ws-data", Name: "Data", Slug: "data", CreatedAt: base, ProjectCount: counts["data"]},
		{ID: "ws-ml", Name: "Machine Learning", Slug: "ml", CreatedAt: base, ProjectCount: counts["ml"]},
		{ID: "ws-infra", Name: "Infrastructure", Slug: "infra", CreatedAt: base, ProjectCount: counts["infra"]},
		{ID: "ws-mobile", Name: "Mobile", Slug: "mobile", CreatedAt: base, ProjectCount: counts["mobile"]},
		{ID: "ws-unsorted", Name: "Unsorted", Slug: "unsorted", CreatedAt: base, IsDefault: true, ProjectCount: 0},
	}, nil
}

func (q *mockQuerier) CreateWorkspace(context.Context, db.CreateWorkspaceParams) (string, error) {
	return "ws-new", nil
}
func (q *mockQuerier) DeleteWorkspace(context.Context, string) (int64, error) { return 1, nil }

// ── user / sessions / org (read-mostly; mock auth) ──────────────────────────

func (q *mockQuerier) GetUserByID(context.Context, string) (db.GetUserByIDRow, error) {
	return db.GetUserByIDRow{ID: "user-mock", Email: "admin@flint.dev", Name: sp("Mock Admin"), IsActive: true}, nil
}

func (q *mockQuerier) GetUserByEmail(context.Context, db.GetUserByEmailParams) (db.GetUserByEmailRow, error) {
	return db.GetUserByEmailRow{ID: "user-mock", Email: "admin@flint.dev", Name: sp("Mock Admin")}, nil
}

func (q *mockQuerier) ListUserSessions(_ context.Context, _ string) ([]db.ListUserSessionsRow, error) {
	now := time.Now()
	return []db.ListUserSessionsRow{
		{ID: "session-mock", UserAgent: sp("Mock Browser"), CreatedAt: now.Add(-2 * time.Hour), LastActivity: now, ExpiresAt: now.Add(22 * time.Hour)},
	}, nil
}

func (q *mockQuerier) SetOrgRequireProjectWorkspace(context.Context, db.SetOrgRequireProjectWorkspaceParams) error {
	return nil
}

// No SSO providers in mock mode — local login only.
func (q *mockQuerier) ListAuthProviderConfigNames(context.Context) ([]db.ListAuthProviderConfigNamesRow, error) {
	return []db.ListAuthProviderConfigNamesRow{}, nil
}

// ── teams / users / roles ───────────────────────────────────────────────────

// mockUser is one seeded person; their email is the Casbin subject for role
// assignments and team membership.
type mockUser struct {
	id, email, name, role string
}

func mockUsers() []mockUser {
	return []mockUser{
		{"user-mock", "admin@flint.dev", "Mock Admin", "admin"},
		{"u-carol", "carol@acme.dev", "Carol Mendoza", "developer"},
		{"u-dave", "dave@acme.dev", "Dave Whitfield", "developer"},
		{"u-erin", "erin@acme.dev", "Erin Kobayashi", "developer"},
		{"u-frank", "frank@acme.dev", "Frank Osei", "developer"},
		{"u-grace", "grace@acme.dev", "Grace Liang", "security-auditor"},
		{"u-heidi", "heidi@acme.dev", "Heidi Brandt", "developer"},
		{"u-ivan", "ivan@acme.dev", "Ivan Petrov", "staging-operator"},
		{"u-judy", "judy@acme.dev", "Judy Alvarez", "developer"},
		{"u-mallory", "mallory@acme.dev", "Mallory Singh", "viewer"},
		{"u-oscar", "oscar@acme.dev", "Oscar Nilsson", "developer"},
	}
}

// teams and their member emails.
var mockTeams = []struct {
	id, name, slug, source, idpGroup string
	members                          []string
}{
	{"team-platform", "Platform", "platform", "manual", "", []string{"admin@flint.dev", "carol@acme.dev", "dave@acme.dev", "oscar@acme.dev"}},
	{"team-frontend", "Frontend", "frontend", "manual", "", []string{"erin@acme.dev", "judy@acme.dev"}},
	{"team-data", "Data & ML", "data-ml", "manual", "", []string{"frank@acme.dev", "heidi@acme.dev"}},
	{"team-security", "Security", "security", "idp", "okta:security", []string{"grace@acme.dev"}},
	{"team-sre", "SRE", "sre", "idp", "okta:sre", []string{"oscar@acme.dev", "ivan@acme.dev"}},
}

func (q *mockQuerier) ListTeamsPaged(context.Context, db.ListTeamsPagedParams) ([]db.ListTeamsPagedRow, error) {
	out := make([]db.ListTeamsPagedRow, 0, len(mockTeams))
	for _, t := range mockTeams {
		row := db.ListTeamsPagedRow{ID: t.id, Name: t.name, Slug: t.slug, Source: t.source, MemberCount: int64(len(t.members))}
		if t.idpGroup != "" {
			row.IdpGroup = sp(t.idpGroup)
		}
		out = append(out, row)
	}
	return out, nil
}

func (q *mockQuerier) GetTeam(_ context.Context, id string) (db.GetTeamRow, error) {
	for _, t := range mockTeams {
		if t.id == id {
			row := db.GetTeamRow{ID: t.id, Name: t.name, Slug: t.slug, Source: t.source}
			if t.idpGroup != "" {
				row.IdpGroup = sp(t.idpGroup)
			}
			return row, nil
		}
	}
	return db.GetTeamRow{}, errNotFound
}

func (q *mockQuerier) ListTeamMembers(_ context.Context, teamID string) ([]db.ListTeamMembersRow, error) {
	out := []db.ListTeamMembersRow{}
	users := mockUsers()
	for _, t := range mockTeams {
		if t.id != teamID {
			continue
		}
		for _, email := range t.members {
			for _, u := range users {
				if u.email == email {
					out = append(out, db.ListTeamMembersRow{ID: u.id, Email: u.email, Name: sp(u.name)})
				}
			}
		}
	}
	return out, nil
}

func (q *mockQuerier) ListUsers(context.Context, db.ListUsersParams) ([]db.ListUsersRow, error) {
	base := q.data.boot.Add(-9000 * time.Hour)
	out := make([]db.ListUsersRow, 0, 11)
	for i, u := range mockUsers() {
		out = append(out, db.ListUsersRow{ID: u.id, Email: u.email, Name: sp(u.name), CreatedAt: base.Add(time.Duration(i) * 220 * time.Hour)})
	}
	return out, nil
}

func (q *mockQuerier) ListRoles(context.Context, db.ListRolesParams) ([]db.ListRolesRow, error) {
	base := q.data.boot.Add(-9000 * time.Hour)
	return []db.ListRolesRow{
		{ID: "role-admin", Name: "Admin", Slug: "admin", Description: sp("Full access to everything"), IsSystem: true, CreatedAt: base},
		{ID: "role-developer", Name: "Developer", Slug: "developer", Description: sp("Trigger runs, manage projects"), IsSystem: true, CreatedAt: base},
		{ID: "role-viewer", Name: "Viewer", Slug: "viewer", Description: sp("Read-only access"), IsSystem: true, CreatedAt: base},
		{ID: "role-sec", Name: "Security Auditor", Slug: "security-auditor", Description: sp("Read all + manage secrets and audit"), IsSystem: false, CreatedAt: base.Add(400 * time.Hour)},
		{ID: "role-stg", Name: "Staging Operator", Slug: "staging-operator", Description: sp("Deploy to staging only"), IsSystem: false, CreatedAt: base.Add(800 * time.Hour)},
	}, nil
}

func (q *mockQuerier) ListAllRoleAssignmentsWithRole(context.Context) ([]db.ListAllRoleAssignmentsWithRoleRow, error) {
	out := []db.ListAllRoleAssignmentsWithRoleRow{}
	for _, u := range mockUsers() {
		out = append(out, db.ListAllRoleAssignmentsWithRoleRow{Subject: u.email, Role: u.role})
	}
	return out, nil
}

func (q *mockQuerier) ListRolePermissions(_ context.Context, roleID string) ([]db.ListRolePermissionsRow, error) {
	perm := func(o, a string) db.ListRolePermissionsRow { return db.ListRolePermissionsRow{Object: o, Action: a} }
	switch roleID {
	case "role-admin":
		return []db.ListRolePermissionsRow{perm("*", "*")}, nil
	case "role-developer":
		return []db.ListRolePermissionsRow{perm("project", "read"), perm("project", "write"), perm("run", "read"), perm("run", "write"), perm("gate", "write")}, nil
	case "role-viewer":
		return []db.ListRolePermissionsRow{perm("project", "read"), perm("run", "read")}, nil
	case "role-sec":
		return []db.ListRolePermissionsRow{perm("project", "read"), perm("secret", "read"), perm("secret", "write"), perm("audit", "read")}, nil
	case "role-stg":
		return []db.ListRolePermissionsRow{perm("run", "read"), perm("run", "write")}, nil
	}
	return []db.ListRolePermissionsRow{}, nil
}

func (q *mockQuerier) ListRoleWorkspaceSlugs(_ context.Context, roleID string) ([]string, error) {
	if roleID == "role-stg" {
		return []string{"platform"}, nil
	}
	return []string{}, nil
}

func (q *mockQuerier) ListRoleEnvironmentNames(_ context.Context, roleID string) ([]string, error) {
	if roleID == "role-stg" {
		return []string{"staging"}, nil
	}
	return []string{}, nil
}

// ── environments / variables ────────────────────────────────────────────────

func (q *mockQuerier) ListEnvironments(context.Context, db.ListEnvironmentsParams) ([]db.ListEnvironmentsRow, error) {
	base := q.data.boot.Add(-8000 * time.Hour)
	return []db.ListEnvironmentsRow{
		{ID: "env-dev", Name: "Development", Slug: "development", CreatedAt: base},
		{ID: "env-staging", Name: "Staging", Slug: "staging", CreatedAt: base},
		{ID: "env-prod", Name: "Production", Slug: "production", CreatedAt: base},
	}, nil
}

func (q *mockQuerier) ListEnvVariables(context.Context, db.ListEnvVariablesParams) ([]db.ListEnvVariablesRow, error) {
	base := q.data.boot.Add(-7000 * time.Hour)
	mk := func(id, name, desc, scope string, secret bool, off int) db.ListEnvVariablesRow {
		return db.ListEnvVariablesRow{ID: id, Name: name, Description: sp(desc), Scope: scope, IsSecret: secret, CreatedAt: base.Add(time.Duration(off) * 100 * time.Hour)}
	}
	return []db.ListEnvVariablesRow{
		mk("var-cluster", "CLUSTER_URL", "Base URL for the deployment cluster", "environment", false, 0),
		mk("var-replicas", "REPLICAS", "Number of pod replicas", "environment", false, 1),
		mk("var-loglevel", "LOG_LEVEL", "Application log level", "organization", false, 2),
		mk("var-dbhost", "DB_HOST", "Primary database hostname", "environment", false, 3),
		mk("var-stripe", "STRIPE_SECRET_KEY", "Stripe API secret key", "environment", true, 4),
		mk("var-sentry", "SENTRY_DSN", "Sentry error-tracking DSN", "organization", true, 5),
		mk("var-awskey", "AWS_ACCESS_KEY_ID", "AWS access key id", "environment", true, 6),
		mk("var-awssecret", "AWS_SECRET_ACCESS_KEY", "AWS secret access key", "environment", true, 7),
		mk("var-ghtoken", "GITHUB_TOKEN", "GitHub app token for releases", "organization", true, 8),
		mk("var-feature", "FEATURE_FLAGS", "Comma-separated feature flags", "environment", false, 9),
	}, nil
}

func (q *mockQuerier) ListEnvVariableValues(context.Context, string) ([]db.ListEnvVariableValuesRow, error) {
	now := q.data.boot
	v := func(varID, envID, val string, secret bool) db.ListEnvVariableValuesRow {
		return db.ListEnvVariableValuesRow{VariableID: varID, EnvironmentID: envID, Value: val, UpdatedAt: now.Add(-300 * time.Hour), IsSecret: secret}
	}
	return []db.ListEnvVariableValuesRow{
		v("var-cluster", "env-staging", "https://staging.acme.internal", false),
		v("var-cluster", "env-prod", "https://prod.acme.internal", false),
		v("var-replicas", "env-staging", "2", false),
		v("var-replicas", "env-prod", "6", false),
		v("var-dbhost", "env-prod", "db-prod.acme.internal", false),
		v("var-stripe", "env-prod", "sk_live_••••••••••••", true),
		v("var-awskey", "env-prod", "AKIA••••••••", true),
	}, nil
}

// ── runners / forge ─────────────────────────────────────────────────────────

func (q *mockQuerier) ListRunnerPoolsPaged(context.Context, db.ListRunnerPoolsPagedParams) ([]db.ListRunnerPoolsPagedRow, error) {
	base := q.data.boot.Add(-5000 * time.Hour)
	return []db.ListRunnerPoolsPagedRow{
		{ID: "rp-default", Name: "default", Description: sp("General-purpose x86 runners"), Cpu: "2", Memory: "4Gi", Arch: "amd64", Ready: true, CreatedAt: base},
		{ID: "rp-large", Name: "large", Description: sp("High-CPU build runners"), Cpu: "8", Memory: "16Gi", Arch: "amd64", Ready: true, CreatedAt: base.Add(200 * time.Hour)},
		{ID: "rp-arm", Name: "arm64", Description: sp("Graviton ARM runners"), Cpu: "4", Memory: "8Gi", Arch: "arm64", Ready: true, CreatedAt: base.Add(400 * time.Hour)},
		{ID: "rp-gpu", Name: "gpu", Description: sp("NVIDIA A10G for model training"), Cpu: "16", Memory: "64Gi", Arch: "amd64", GpuVendor: sp("nvidia"), GpuModel: sp("A10G"), GpuCount: i4(1, true), Ready: true, CreatedAt: base.Add(600 * time.Hour)},
		{ID: "rp-macos", Name: "macos", Description: sp("macOS runners for iOS builds"), Cpu: "8", Memory: "16Gi", Arch: "arm64", Ready: false, CreatedAt: base.Add(800 * time.Hour)},
	}, nil
}

func (q *mockQuerier) ListForgeConnections(context.Context, db.ListForgeConnectionsParams) ([]db.ListForgeConnectionsRow, error) {
	base := q.data.boot.Add(-9000 * time.Hour)
	return []db.ListForgeConnectionsRow{
		{ID: "forge-gh", ForgeType: "github", DisplayName: "acme (GitHub App)", AppID: sp("428193"), InstallationID: sp("51203847"), CreatedAt: base},
	}, nil
}

// ── audit / tags / saved views ──────────────────────────────────────────────

func (q *mockQuerier) ListAuditLog(context.Context, db.ListAuditLogParams) ([]db.ListAuditLogRow, error) {
	now := q.data.boot
	type e struct {
		action, resType, resID, email string
		agoMin                        int
	}
	entries := []e{
		{"gate.approve", "run", "mock-run-old-1", "carol@acme.dev", 35},
		{"run.trigger", "project", "proj-payments", "grace@acme.dev", 88},
		{"env_variable.update", "env_variable", "var-stripe", "grace@acme.dev", 140},
		{"role.assign", "user", "u-ivan", "admin@flint.dev", 220},
		{"project.create", "project", "proj-ml-serve", "frank@acme.dev", 360},
		{"api_key.create", "api_key", "key-ci-deploy", "admin@flint.dev", 500},
		{"team.update", "team", "team-security", "admin@flint.dev", 720},
		{"run.cancel", "run", "mock-run-old-2", "dave@acme.dev", 900},
		{"workspace.create", "workspace", "ws-ml", "admin@flint.dev", 1200},
		{"environment.create", "environment", "env-staging", "oscar@acme.dev", 1500},
		{"role.create", "role", "role-sec", "admin@flint.dev", 1800},
		{"login", "session", "session-mock", "carol@acme.dev", 2100},
	}
	out := make([]db.ListAuditLogRow, 0, len(entries))
	for i, x := range entries {
		out = append(out, db.ListAuditLogRow{
			ID: fmt.Sprintf("audit-%d", i+1), UserID: sp("user-" + x.email), UserEmail: sp(x.email),
			Action: x.action, ResourceType: x.resType, ResourceID: sp(x.resID),
			Metadata: []byte(`{}`), IpAddress: sp("10.0.1.42"), CreatedAt: now.Add(-time.Duration(x.agoMin) * time.Minute),
		})
	}
	return out, nil
}

func (q *mockQuerier) ListSavedViews(context.Context, db.ListSavedViewsParams) ([]db.ListSavedViewsRow, error) {
	base := q.data.boot.Add(-1200 * time.Hour)
	return []db.ListSavedViewsRow{
		{ID: "view-fail-pay", Name: "Payments — failures", Route: "/ci/runs", Selector: []byte(`{"projectId":"proj-payments","status":"failed"}`), CreatedAt: base},
		{ID: "view-pci", Name: "PCI projects", Route: "/ci/projects", Selector: []byte(`{"tags":["compliance:pci"]}`), CreatedAt: base.Add(100 * time.Hour)},
		{ID: "view-running", Name: "Running now", Route: "/ci/runs", Selector: []byte(`{"status":"running"}`), CreatedAt: base.Add(300 * time.Hour)},
	}, nil
}

func (q *mockQuerier) ListTagKeys(context.Context, string) ([]db.ListTagKeysRow, error) {
	base := q.data.boot.Add(-9000 * time.Hour)
	return []db.ListTagKeysRow{
		{ID: "tk-domain", Key: "domain", Label: "Domain", AllowedValues: []string{"search", "payments", "identity", "analytics", "messaging", "platform", "ml"}, Color: "#22d3ee", CreatedAt: base},
		{ID: "tk-tier", Key: "tier", Label: "Tier", AllowedValues: []string{"1", "2", "3"}, Color: "#f59e0b", CreatedAt: base},
		{ID: "tk-lang", Key: "lang", Label: "Language", AllowedValues: []string{"go", "ts", "python", "rust", "swift", "kotlin"}, Color: "#a78bfa", CreatedAt: base},
		{ID: "tk-compliance", Key: "compliance", Label: "Compliance", AllowedValues: []string{"pci", "soc2", "hipaa"}, Color: "#fb7185", CreatedAt: base},
	}, nil
}

// ── API keys / personal tokens ──────────────────────────────────────────────

func (q *mockQuerier) ListAPIKeysDetailed(context.Context, db.ListAPIKeysDetailedParams) ([]db.ListAPIKeysDetailedRow, error) {
	now := q.data.boot
	used1 := now.Add(-3 * time.Hour)
	used2 := now.Add(-48 * time.Hour)
	exp := now.Add(60 * 24 * time.Hour)
	return []db.ListAPIKeysDetailedRow{
		{ID: "key-ci-deploy", Name: "CI Deploy Bot", Role: "developer", CreatedBy: "admin@flint.dev", LastUsedAt: &used1, CreatedAt: now.Add(-2000 * time.Hour)},
		{ID: "key-readonly", Name: "Grafana (read-only)", Role: "viewer", CreatedBy: "oscar@acme.dev", LastUsedAt: &used2, CreatedAt: now.Add(-1500 * time.Hour)},
		{ID: "key-release", Name: "Release Pipeline", Role: "developer", CreatedBy: "admin@flint.dev", ExpiresAt: &exp, CreatedAt: now.Add(-800 * time.Hour)},
		{ID: "key-terraform", Name: "Terraform Cloud", Role: "staging-operator", CreatedBy: "ivan@acme.dev", CreatedAt: now.Add(-400 * time.Hour)},
	}, nil
}

func (q *mockQuerier) ListAPIKeyWorkspaceSlugs(_ context.Context, keyID string) ([]string, error) {
	if keyID == "key-terraform" {
		return []string{"infra"}, nil
	}
	return nil, nil
}
func (q *mockQuerier) ListAPIKeyEnvironmentSlugs(_ context.Context, keyID string) ([]string, error) {
	if keyID == "key-terraform" {
		return []string{"staging"}, nil
	}
	return nil, nil
}

func (q *mockQuerier) ListPersonalTokensByUser(context.Context, string) ([]db.ListPersonalTokensByUserRow, error) {
	now := q.data.boot
	used := now.Add(-90 * time.Minute)
	exp := now.Add(30 * 24 * time.Hour)
	return []db.ListPersonalTokensByUserRow{
		{ID: "pat-cli", Name: "laptop-cli", LastUsedAt: &used, ExpiresAt: &exp, CreatedAt: now.Add(-720 * time.Hour)},
		{ID: "pat-ci", Name: "homelab", CreatedAt: now.Add(-240 * time.Hour)},
	}, nil
}

func (q *mockQuerier) ListProjectWebhooks(_ context.Context, projectID string) ([]db.Webhook, error) {
	return []db.Webhook{
		{ID: "wh-" + projectID, ProjectID: projectID, Url: "https://hooks.acme.dev/flint/" + projectID, CreatedAt: q.data.boot.Add(-1000 * time.Hour)},
	}, nil
}

// ── mutations: accept and no-op (mock has no store) ─────────────────────────

func (q *mockQuerier) InsertAuditEntry(context.Context, db.InsertAuditEntryParams) error { return nil }
func (q *mockQuerier) CreateTagKey(context.Context, db.CreateTagKeyParams) (string, error) {
	return "tag-new", nil
}
func (q *mockQuerier) UpdateTagKey(context.Context, db.UpdateTagKeyParams) error      { return nil }
func (q *mockQuerier) DeleteTagKey(context.Context, string) (int64, error)            { return 1, nil }
func (q *mockQuerier) CreateEnvironment(context.Context, db.CreateEnvironmentParams) (string, error) {
	return "env-new", nil
}
func (q *mockQuerier) DeleteEnvironment(context.Context, string) error { return nil }
func (q *mockQuerier) CreateEnvVariable(context.Context, db.CreateEnvVariableParams) (string, error) {
	return "var-new", nil
}
func (q *mockQuerier) UpsertEnvVariableValue(context.Context, db.UpsertEnvVariableValueParams) error {
	return nil
}
func (q *mockQuerier) DeleteEnvVariable(context.Context, string) error { return nil }
func (q *mockQuerier) IsEnvVariableSecret(context.Context, string) (bool, error) { return false, nil }
func (q *mockQuerier) CreateSavedView(context.Context, db.CreateSavedViewParams) (string, error) {
	return "view-new", nil
}
func (q *mockQuerier) DeleteSavedView(context.Context, db.DeleteSavedViewParams) (int64, error) {
	return 1, nil
}

// NewMockDeps assembles a fully in-memory Deps for `--mock` mode: no DB pool, no
// engine loop, RBAC disabled (nil Enforcer → requirePermission allows all), and
// a mock-auth shortcut (MockMode → authMiddleware injects an admin identity).
func NewMockDeps(cfg *config.Config) Deps {
	data := newMockData()
	return Deps{
		Config:         cfg,
		DB:             nil,
		Q:              &mockQuerier{data: data},
		Engine:         &mockEngine{data: data},
		Forge:          forge.NewGitHub("", nil),
		Logs:           mockLogSink{},
		LogBroadcast:   NewLogStream(),
		StateBroadcast: NewStateStream(),
		Mode:           "all",
		MockMode:       true,
		Runs:           mockRunCreator{},
		Sessions: auth.NewSessionManager(auth.SessionConfig{
			SigningKey: []byte(cfg.Auth.JWT.Secret),
			Issuer:     cfg.Server.BaseURL,
		}),
		Enforcer: nil, // disables RBAC enforcement in requirePermission
	}
}

// ── small helpers ───────────────────────────────────────────────────────────

func (m *mockData) latestRun(projectID string) *mockRun {
	var best *mockRun
	for i := range m.runs {
		r := &m.runs[i]
		if r.projectID != projectID {
			continue
		}
		if best == nil || r.startedAgo < best.startedAgo {
			best = r
		}
	}
	return best
}

func patternText(p *string) string {
	if p == nil {
		return ""
	}
	return strings.ToLower(strings.Trim(*p, "%"))
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func overlaps(a, b []string) bool {
	for _, x := range a {
		if contains(b, x) {
			return true
		}
	}
	return false
}
