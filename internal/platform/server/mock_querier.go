package server

import (
	"context"
	"errors"
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
	return db.GetRunStatsRow{TotalRuns: 160, SuccessRuns: 140, RunsToday: 24, AvgDurationMs: 195000}, nil
}

func (q *mockQuerier) CountActiveProjects(context.Context) (int64, error) {
	return int64(len(q.data.projects)), nil
}

func (q *mockQuerier) CountPendingGates(context.Context) (int64, error) { return 1, nil }

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
	out := []db.ListRunsFilteredRow{}
	for i := range q.data.runs {
		r := &q.data.runs[i]
		if arg.ProjectID != "" && r.projectID != arg.ProjectID {
			continue
		}
		st := q.data.effectiveStatus(r)
		if arg.Status != "" && st != arg.Status {
			continue
		}
		out = append(out, q.runRow(r, st))
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

func (q *mockQuerier) GetWorkflowDAGWaves(context.Context, string) ([]byte, error) {
	return q.data.dagWaves(), nil
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

// ── settings tail: empty lists so pages load without a DB ───────────────────

func (q *mockQuerier) ListTeamsPaged(context.Context, db.ListTeamsPagedParams) ([]db.ListTeamsPagedRow, error) {
	return []db.ListTeamsPagedRow{}, nil
}
func (q *mockQuerier) GetTeam(context.Context, string) (db.GetTeamRow, error) {
	return db.GetTeamRow{}, errNotFound
}
func (q *mockQuerier) ListTeamMembers(context.Context, string) ([]db.ListTeamMembersRow, error) {
	return []db.ListTeamMembersRow{}, nil
}
func (q *mockQuerier) ListUsers(context.Context, db.ListUsersParams) ([]db.ListUsersRow, error) {
	return []db.ListUsersRow{}, nil
}
func (q *mockQuerier) ListRoles(context.Context, db.ListRolesParams) ([]db.ListRolesRow, error) {
	return []db.ListRolesRow{}, nil
}
func (q *mockQuerier) ListAllRoleAssignmentsWithRole(context.Context) ([]db.ListAllRoleAssignmentsWithRoleRow, error) {
	return []db.ListAllRoleAssignmentsWithRoleRow{}, nil
}
func (q *mockQuerier) ListEnvironments(context.Context, db.ListEnvironmentsParams) ([]db.ListEnvironmentsRow, error) {
	return []db.ListEnvironmentsRow{}, nil
}
func (q *mockQuerier) ListEnvVariables(context.Context, db.ListEnvVariablesParams) ([]db.ListEnvVariablesRow, error) {
	return []db.ListEnvVariablesRow{}, nil
}
func (q *mockQuerier) ListEnvVariableValues(context.Context, string) ([]db.ListEnvVariableValuesRow, error) {
	return []db.ListEnvVariableValuesRow{}, nil
}
func (q *mockQuerier) ListRunnerPoolsPaged(context.Context, db.ListRunnerPoolsPagedParams) ([]db.ListRunnerPoolsPagedRow, error) {
	return []db.ListRunnerPoolsPagedRow{}, nil
}
func (q *mockQuerier) ListForgeConnections(context.Context, db.ListForgeConnectionsParams) ([]db.ListForgeConnectionsRow, error) {
	return []db.ListForgeConnectionsRow{}, nil
}
func (q *mockQuerier) ListAuditLog(context.Context, db.ListAuditLogParams) ([]db.ListAuditLogRow, error) {
	return []db.ListAuditLogRow{}, nil
}
func (q *mockQuerier) ListSavedViews(context.Context, db.ListSavedViewsParams) ([]db.ListSavedViewsRow, error) {
	return []db.ListSavedViewsRow{}, nil
}
func (q *mockQuerier) ListTagKeys(context.Context, string) ([]db.ListTagKeysRow, error) {
	return []db.ListTagKeysRow{}, nil
}
func (q *mockQuerier) ListAPIKeysDetailed(context.Context, db.ListAPIKeysDetailedParams) ([]db.ListAPIKeysDetailedRow, error) {
	return []db.ListAPIKeysDetailedRow{}, nil
}
func (q *mockQuerier) ListAPIKeyWorkspaceSlugs(context.Context, string) ([]string, error) {
	return nil, nil
}
func (q *mockQuerier) ListAPIKeyEnvironmentSlugs(context.Context, string) ([]string, error) {
	return nil, nil
}
func (q *mockQuerier) ListPersonalTokensByUser(context.Context, string) ([]db.ListPersonalTokensByUserRow, error) {
	return []db.ListPersonalTokensByUserRow{}, nil
}
func (q *mockQuerier) ListProjectWebhooks(context.Context, string) ([]db.Webhook, error) {
	return []db.Webhook{}, nil
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
