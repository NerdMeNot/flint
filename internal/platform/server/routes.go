package server

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	pgtype "github.com/jackc/pgx/v5/pgtype"
)

// ── Response structs ──────────────────────────────────────────

type lastRunResponse struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Branch      string `json:"branch"`
	Duration    string `json:"duration"`
	TriggeredBy string `json:"triggeredBy"`
	StartedAt   string `json:"startedAt"`
}

type projectResponse struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	Repo           string           `json:"repo"`
	Workspace      string           `json:"workspace"`
	Colour         string           `json:"colour"`
	Tags           []string         `json:"tags"`
	PipelineCount  int              `json:"pipelineCount"`
	PipelineErrors int              `json:"pipelineErrors"`
	LastRun        *lastRunResponse `json:"lastRun,omitempty"`
	Health         *projectHealth   `json:"health,omitempty"`
	CreatedAt      string           `json:"createdAt"`
	Inferred       bool             `json:"inferred"`
}

// projectHealth summarizes a project's recent run outcomes for the dashboard
// health bars / "needs attention" (UI ProjectHealth). passRate is a percent.
type projectHealth struct {
	RecentRuns []string `json:"recentRuns"`
	PassRate   int      `json:"passRate"`
	FailingNow bool     `json:"failingNow"`
	TotalRuns  int      `json:"totalRuns"`
}

// buildProjectHealth assembles a projectHealth from recent statuses (newest
// first) and totals. Returns nil when the project has no runs.
func buildProjectHealth(recent []string, total, succeeded int64) *projectHealth {
	if total == 0 {
		return nil
	}
	if recent == nil {
		recent = []string{}
	}
	h := &projectHealth{
		RecentRuns: recent,
		PassRate:   int(math.Round(float64(succeeded) / float64(total) * 100)),
		TotalRuns:  int(total),
	}
	if len(recent) > 0 {
		h.FailingNow = recent[0] == "failed"
	}
	return h
}

type runResponse struct {
	ID            string  `json:"id"`
	ProjectID     string  `json:"projectId"`
	ProjectName   string  `json:"projectName"`
	ProjectColour string  `json:"projectColour"`
	Repo          string  `json:"repo"`
	Status        string  `json:"status"`
	TriggerType   string  `json:"triggerType"`
	Branch        string  `json:"branch"`
	CommitSha     string  `json:"commitSha"`
	CommitMessage string  `json:"commitMessage"`
	TriggeredBy   string  `json:"triggeredBy"`
	WorkflowFile  string  `json:"workflowFile"`
	Duration      string  `json:"duration"`
	StartedAt     string  `json:"startedAt"`
	FinishedAt    *string `json:"finishedAt,omitempty"`
	// Epoch-ms variants the UI uses for time-range filtering and adaptive
	// timestamp rendering (alongside the RFC3339 strings above).
	StartedAtTs  int64            `json:"startedAtTs"`
	FinishedAtTs *int64           `json:"finishedAtTs,omitempty"`
	Environment  *string          `json:"environment,omitempty"`
	ErrorMessage *string          `json:"errorMessage,omitempty"`
	Steps        []runStepSummary `json:"steps,omitempty"`
}

// runStepSummary is the compact per-step shape the run feed renders as stage
// pips (UI RunStepSummary).
type runStepSummary struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type runnerResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Provider    string  `json:"provider"`
	CPU         string  `json:"cpu"`
	Memory      string  `json:"memory"`
	Disk        *string `json:"disk,omitempty"`
	Arch        string  `json:"arch"`
	GPUVendor   *string `json:"gpuVendor,omitempty"`
	GPUModel    *string `json:"gpuModel,omitempty"`
	GPUCount    *int32  `json:"gpuCount,omitempty"`
	// Economics policy — surfaced so the pool editor can round-trip it and the
	// UI can show the speed/cost tradeoff the pool encodes.
	CapacityType   string `json:"capacityType"`
	Objective      string `json:"objective"`
	MinWarm        int32  `json:"minWarm"`
	MaxMachines    int32  `json:"maxMachines"`
	IdleTTLSeconds int32  `json:"idleTtlSeconds"`
	IsDefault      bool   `json:"isDefault"`
	Ready          bool   `json:"ready"`
	CreatedAt      string `json:"createdAt"`
}

func (s *Server) registerAPIRoutes() {
	v1 := s.hertz.Group("/api/v1",
		s.requestIDMiddleware(),
		maxBodyMiddleware(1<<20),
		s.authMiddleware(),
	)

	// Capabilities — which product sections the unified UI should render. Any
	// authenticated user needs this to build the top-level navigation.
	v1.GET("/capabilities", s.handleCapabilities)

	// Meta — backend data mode (live vs mock), drives the demo banner.
	v1.GET("/meta", s.handleMeta)

	// Stats (replaces dashboard).
	v1.GET("/stats", s.requirePermission(auth.ObjWorkspace, auth.ActRead), s.handleStats)
	v1.GET("/queue", s.requirePermission(auth.ObjWorkspace, auth.ActRead), s.handleQueueStats)
	v1.GET("/search", s.requirePermission(auth.ObjProject, auth.ActRead), s.handleSearch)

	// Org.
	v1.GET("/org", s.requirePermission(auth.ObjWorkspace, auth.ActRead), s.getOrg)
	v1.PUT("/org/policy", s.requirePermission(auth.ObjWorkspace, auth.ActManage), s.handleSetOrgPolicy)

	// CI product routes — gated by products.ci.enabled (default on).
	if s.deps.Config.Products.CIEnabled() {
		// Projects.
		v1.GET("/projects", s.requirePermission(auth.ObjProject, auth.ActRead), s.listProjects)
		v1.POST("/projects", s.requirePermission(auth.ObjProject, auth.ActWrite), s.handleCreateProject)
		v1.GET("/projects/:id", s.requirePermission(auth.ObjProject, auth.ActRead), s.getProject)
		v1.PATCH("/projects/:id", s.requirePermission(auth.ObjProject, auth.ActWrite), s.handleUpdateProject)
		v1.DELETE("/projects/:id", s.requirePermission(auth.ObjProject, auth.ActWrite), s.handleArchiveProject)
		v1.POST("/projects/:id/restore", s.requirePermission(auth.ObjProject, auth.ActWrite), s.handleRestoreProject)
		v1.GET("/projects/:id/pipelines", s.requirePermission(auth.ObjProject, auth.ActRead), s.handleListProjectPipelines)
		v1.PUT("/projects/:id/tags", s.requirePermission(auth.ObjProject, auth.ActWrite), s.handleSetProjectTags)
		v1.GET("/projects/:id/webhooks", s.requirePermission(auth.ObjProject, auth.ActWrite), s.handleListWebhooks)
		v1.POST("/projects/:id/webhooks", s.requirePermission(auth.ObjProject, auth.ActWrite), s.handleCreateWebhook)
		v1.DELETE("/projects/:id/webhooks/:webhook", s.requirePermission(auth.ObjProject, auth.ActWrite), s.handleDeleteWebhook)

		// Runs (global list + per-run operations).
		v1.GET("/runs", s.requirePermission(auth.ObjRun, auth.ActRead), s.handleListRuns)
		v1.GET("/runs/:id", s.requirePermission(auth.ObjRun, auth.ActRead), s.getRun)
		v1.POST("/runs", s.requirePermission(auth.ObjRun, auth.ActTrigger), s.triggerRun)
		v1.GET("/runs/:id/steps", s.requirePermission(auth.ObjRun, auth.ActRead), s.handleGetRunSteps)
		v1.GET("/runs/:id/cost", s.requirePermission(auth.ObjRun, auth.ActRead), s.handleRunCost)
		v1.GET("/runs/:id/placement", s.requirePermission(auth.ObjRun, auth.ActRead), s.handleRunPlacement)
		v1.GET("/runs/:id/events", s.requirePermission(auth.ObjRun, auth.ActRead), s.handleGetRunEvents)
		v1.GET("/runs/:id/steps/:step/logs", s.requirePermission(auth.ObjRun, auth.ActRead), s.handleGetStepLogs)
		v1.GET("/runs/:id/steps/:step/logs/stream", s.requirePermission(auth.ObjRun, auth.ActRead), s.handleStreamStepLogs)
		v1.GET("/runs/:id/logs", s.requirePermission(auth.ObjRun, auth.ActRead), s.handleGetRunLogs)
		v1.GET("/runs/:id/stream", s.requirePermission(auth.ObjRun, auth.ActRead), s.handleStreamRunState)
		v1.POST("/runs/:id/cancel", s.requirePermission(auth.ObjRun, auth.ActCancel), s.handleCancelRun)
		v1.POST("/runs/:id/pause", s.requirePermission(auth.ObjRun, auth.ActCancel), s.handlePauseRun)
		v1.POST("/runs/:id/resume", s.requirePermission(auth.ObjRun, auth.ActTrigger), s.handleResumeRun)
		v1.POST("/runs/:id/retry", s.requirePermission(auth.ObjRun, auth.ActTrigger), s.handleRetryRun)
		v1.POST("/runs/:id/rerun-failed", s.requirePermission(auth.ObjRun, auth.ActTrigger), s.handleRerunFailed)
		v1.POST("/runs/:id/retry-from/:step", s.requirePermission(auth.ObjRun, auth.ActTrigger), s.handleRetryFromStep)
		v1.POST("/runs/:id/steps/:step/resolve", s.requirePermission(auth.ObjRun, auth.ActCancel), s.handleResolveStep)

		// Gates.
		v1.GET("/gates", s.requirePermission(auth.ObjRun, auth.ActRead), s.handleListGates)
		v1.POST("/runs/:id/gates/:step/approve", s.requirePermission(auth.ObjGate, auth.ActApprove), s.handleApproveGate)
		v1.POST("/runs/:id/gates/:step/reject", s.requirePermission(auth.ObjGate, auth.ActReject), s.handleRejectGate)

		// External signals (resolves `wait` steps).
		v1.POST("/runs/:id/signals", s.requirePermission(auth.ObjRun, auth.ActTrigger), s.handleSendSignal)
	}

	// Teams.
	v1.GET("/teams", s.requirePermission(auth.ObjTeam, auth.ActRead), s.handleListTeams)
	v1.GET("/teams/:id", s.requirePermission(auth.ObjTeam, auth.ActRead), s.handleGetTeam)
	v1.POST("/teams", s.requirePermission(auth.ObjTeam, auth.ActManage), s.handleCreateTeam)
	v1.DELETE("/teams/:id", s.requirePermission(auth.ObjTeam, auth.ActManage), s.handleDeleteTeam)
	v1.POST("/teams/:id/members", s.requirePermission(auth.ObjTeam, auth.ActManage), s.handleAddTeamMembers)
	v1.DELETE("/teams/:id/members/:userId", s.requirePermission(auth.ObjTeam, auth.ActManage), s.handleRemoveTeamMember)

	// Users.
	v1.GET("/users", s.requirePermission(auth.ObjTeam, auth.ActRead), s.handleListUsers)
	v1.POST("/users", s.requirePermission(auth.ObjTeam, auth.ActManage), s.handleCreateUser)

	// Roles.
	v1.GET("/roles", s.requirePermission(auth.ObjRole, auth.ActRead), s.handleListRoles)
	v1.POST("/roles", s.requirePermission(auth.ObjRole, auth.ActManage), s.handleCreateRole)
	v1.PATCH("/roles/:id", s.requirePermission(auth.ObjRole, auth.ActManage), s.handleUpdateRole)
	v1.DELETE("/roles/:id", s.requirePermission(auth.ObjRole, auth.ActManage), s.handleDeleteRole)

	// Role assignments.
	v1.GET("/roles/assignments", s.requirePermission(auth.ObjRole, auth.ActRead), s.handleListAssignments)
	v1.POST("/roles/assignments", s.requirePermission(auth.ObjRole, auth.ActManage), s.handleCreateAssignment)
	v1.DELETE("/roles/assignments/:id", s.requirePermission(auth.ObjRole, auth.ActManage), s.handleDeleteAssignment)

	// Environments.
	v1.GET("/environments", s.requirePermission(auth.ObjEnvironment, auth.ActRead), s.handleListEnvironments)
	v1.POST("/environments", s.requirePermission(auth.ObjEnvironment, auth.ActManage), s.handleCreateEnvironment)
	v1.DELETE("/environments/:id", s.requirePermission(auth.ObjEnvironment, auth.ActManage), s.handleDeleteEnvironment)

	// Environment variables.
	v1.GET("/env-variables", s.requirePermission(auth.ObjSecret, auth.ActRead), s.handleListEnvVariables)
	v1.GET("/env-variables/values", s.requirePermission(auth.ObjSecret, auth.ActRead), s.handleListEnvVariableValues)
	v1.POST("/env-variables", s.requirePermission(auth.ObjSecret, auth.ActManage), s.handleCreateEnvVariable)
	v1.PUT("/env-variables/:id/values", s.requirePermission(auth.ObjSecret, auth.ActManage), s.handleSetEnvVariableValue)
	v1.DELETE("/env-variables/:id", s.requirePermission(auth.ObjSecret, auth.ActManage), s.handleDeleteEnvVariable)

	// Workspaces.
	v1.GET("/workspaces", s.requirePermission(auth.ObjWorkspace, auth.ActRead), s.handleListWorkspaces)
	v1.POST("/workspaces", s.requirePermission(auth.ObjWorkspace, auth.ActManage), s.handleCreateWorkspace)
	v1.DELETE("/workspaces/:id", s.requirePermission(auth.ObjWorkspace, auth.ActManage), s.handleDeleteWorkspace)

	// Tag registry (curated tag keys).
	v1.GET("/tags", s.requirePermission(auth.ObjTag, auth.ActRead), s.handleListTagKeys)
	v1.POST("/tags", s.requirePermission(auth.ObjTag, auth.ActManage), s.handleCreateTagKey)
	v1.PUT("/tags/:id", s.requirePermission(auth.ObjTag, auth.ActManage), s.handleUpdateTagKey)
	v1.DELETE("/tags/:id", s.requirePermission(auth.ObjTag, auth.ActManage), s.handleDeleteTagKey)

	// Saved views — personal navigation targets (route + filters), scoped to the
	// owning user. Gated at CI read level; handlers isolate by owner.
	v1.GET("/views", s.requirePermission(auth.ObjProject, auth.ActRead), s.handleListSavedViews)
	v1.POST("/views", s.requirePermission(auth.ObjProject, auth.ActRead), s.handleCreateSavedView)
	v1.DELETE("/views/:id", s.requirePermission(auth.ObjProject, auth.ActRead), s.handleDeleteSavedView)

	// API keys.
	v1.GET("/api-keys", s.requirePermission(auth.ObjAPIKey, auth.ActRead), s.handleListAPIKeys)
	v1.POST("/api-keys", s.requirePermission(auth.ObjAPIKey, auth.ActManage), s.handleCreateAPIKey)
	v1.DELETE("/api-keys/:id", s.requirePermission(auth.ObjAPIKey, auth.ActManage), s.handleDeleteAPIKey)

	// Personal tokens (any authenticated user).
	v1.GET("/personal-tokens", s.handleListPersonalTokens)
	v1.POST("/personal-tokens", s.handleCreatePersonalToken)
	v1.DELETE("/personal-tokens/:id", s.handleRevokePersonalToken)

	// Auth provider config (SSO setup).
	v1.GET("/auth/providers", s.requirePermission(auth.ObjRole, auth.ActManage), s.handleListAuthProviders)
	v1.PUT("/auth/provider", s.requirePermission(auth.ObjRole, auth.ActManage), s.handleUpdateAuthProvider)
	v1.POST("/auth/provider/test", s.requirePermission(auth.ObjRole, auth.ActManage), s.handleTestAuthProvider)
	v1.POST("/auth/provider/test-login", s.requirePermission(auth.ObjRole, auth.ActManage), s.handleStartTestLogin)
	v1.GET("/auth/provider/test-login/:id", s.requirePermission(auth.ObjRole, auth.ActManage), s.handleGetTestLogin)
	v1.GET("/auth/sign-in-log", s.requirePermission(auth.ObjRole, auth.ActManage), s.handleSignInLog)
	v1.DELETE("/auth/provider/:type", s.requirePermission(auth.ObjRole, auth.ActManage), s.handleDeleteAuthProvider)
	v1.GET("/auth/group-mappings", s.requirePermission(auth.ObjRole, auth.ActManage), s.handleGetGroupMappings)
	v1.PUT("/auth/group-mappings", s.requirePermission(auth.ObjRole, auth.ActManage), s.handlePutGroupMappings)
	v1.PUT("/auth/require-sso", s.requirePermission(auth.ObjRole, auth.ActManage), s.handleSetRequireSSO)

	// SCIM token management (the SCIM data-plane lives at /scim/v2 with its own auth).
	v1.GET("/auth/scim", s.requirePermission(auth.ObjRole, auth.ActManage), s.handleGetScimStatus)
	v1.POST("/auth/scim/token", s.requirePermission(auth.ObjRole, auth.ActManage), s.handleGenerateScimToken)
	v1.DELETE("/auth/scim/token", s.requirePermission(auth.ObjRole, auth.ActManage), s.handleRevokeScimToken)

	// Forge connections (DB-managed; credentials envelope-encrypted server-side).
	v1.GET("/forge-connections", s.requirePermission(auth.ObjConnection, auth.ActRead), s.handleListForgeConnections)
	v1.POST("/forge-connections", s.requirePermission(auth.ObjConnection, auth.ActManage), s.handleCreateForgeConnection)
	v1.PUT("/forge-connections", s.requirePermission(auth.ObjConnection, auth.ActManage), s.handleUpdateForgeConnection)
	v1.DELETE("/forge-connections/:id", s.requirePermission(auth.ObjConnection, auth.ActManage), s.handleDeleteForgeConnection)

	// Runners (machine pools).
	v1.GET("/runners", s.requirePermission(auth.ObjRunner, auth.ActRead), s.listRunners)
	v1.POST("/runners", s.requirePermission(auth.ObjRunner, auth.ActManage), s.handleCreateRunner)
	v1.PATCH("/runners/:name", s.requirePermission(auth.ObjRunner, auth.ActManage), s.handleUpdateRunner)
	v1.DELETE("/runners/:name", s.requirePermission(auth.ObjRunner, auth.ActManage), s.handleDeleteRunner)
	v1.POST("/runners/:name/default", s.requirePermission(auth.ObjRunner, auth.ActManage), s.handleSetDefaultRunner)
	v1.POST("/runners/:name/token", s.requirePermission(auth.ObjRunner, auth.ActManage), s.handleMintPoolJoinToken)

	// Machines (the live fleet) + the economics decision ledger.
	v1.GET("/machines", s.requirePermission(auth.ObjRunner, auth.ActRead), s.handleListMachines)
	v1.GET("/machines/:id", s.requirePermission(auth.ObjRunner, auth.ActRead), s.handleGetMachine)
	v1.POST("/machines/:id/drain", s.requirePermission(auth.ObjRunner, auth.ActManage), s.handleDrainMachine)
	v1.GET("/decisions", s.requirePermission(auth.ObjRunner, auth.ActRead), s.handleListDecisions)

	// Compute providers (the machine sources pools draw from).
	v1.GET("/providers", s.requirePermission(auth.ObjRunner, auth.ActRead), s.handleListComputeProviders)
	v1.POST("/providers", s.requirePermission(auth.ObjRunner, auth.ActManage), s.handleCreateComputeProvider)
	v1.PATCH("/providers/:name", s.requirePermission(auth.ObjRunner, auth.ActManage), s.handleUpdateComputeProvider)
	v1.DELETE("/providers/:name", s.requirePermission(auth.ObjRunner, auth.ActManage), s.handleDeleteComputeProvider)
	v1.POST("/providers/:name/test", s.requirePermission(auth.ObjRunner, auth.ActManage), s.handleTestComputeProvider)

	// Audit entries (renamed from audit-log).
	v1.GET("/audit-entries", s.requirePermission(auth.ObjAudit, auth.ActRead), s.handleListAuditLog)

	// Auth.
	// Auth routes (login, callback, device flow) registered separately in server.go.

	// Product routes (e.g. Flint Workflows), wired by the composition root so the
	// platform server doesn't import product packages.
	for _, register := range s.deps.APIRoutes {
		register(v1)
	}
}

// ── Stats ─────────────────────────────────────────────────────

func (s *Server) handleStats(ctx context.Context, c *app.RequestContext) {
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to get org")
		return
	}

	stats, err := s.deps.Q.GetRunStats(ctx, org.ID)
	if err != nil {
		apiInternal(ctx, c, "failed to compute run stats")
		return
	}

	activeProjects, _ := s.deps.Q.CountActiveProjects(ctx)
	pendingGates, _ := s.deps.Q.CountPendingGates(ctx)

	// Percent (0-100) to match the UI, which renders the value with a "%" suffix.
	successRate := 0.0
	if stats.TotalRuns > 0 {
		successRate = math.Round(float64(stats.SuccessRuns) / float64(stats.TotalRuns) * 100)
	}

	c.JSON(consts.StatusOK, utils.H{
		"totalRuns":      stats.TotalRuns,
		"successRate":    successRate,
		"pendingGates":   pendingGates,
		"activeProjects": activeProjects,
		"runsToday":      stats.RunsToday,
		"avgDuration":    (time.Duration(stats.AvgDurationMs) * time.Millisecond).String(),
	})
}

func (s *Server) handleSearch(ctx context.Context, c *app.RequestContext) {
	q := string(c.Query("q"))
	if q == "" {
		c.JSON(consts.StatusOK, utils.H{"projects": []any{}, "runs": []any{}})
		return
	}

	claims := claimsFromCtx(ctx)
	pattern := "%" + q + "%"

	projects := []utils.H{}
	if rows, err := s.deps.Q.SearchProjects(ctx, db.SearchProjectsParams{OrgID: claims.OrgID, Pattern: &pattern}); err == nil {
		for _, p := range rows {
			projects = append(projects, utils.H{"id": p.ID, "name": p.Name, "repo": p.RepoPath, "colour": p.Colour})
		}
	}

	runs := []utils.H{}
	if rows, err := s.deps.Q.SearchRuns(ctx, db.SearchRunsParams{OrgID: claims.OrgID, Pattern: &pattern}); err == nil {
		for _, r := range rows {
			h := utils.H{"id": r.ID, "status": r.Status, "projectName": r.ProjectName, "projectColour": r.ProjectColour}
			if r.Branch != nil {
				h["branch"] = *r.Branch
			}
			if r.CommitSha != nil {
				h["commitSha"] = *r.CommitSha
			}
			runs = append(runs, h)
		}
	}

	c.JSON(consts.StatusOK, utils.H{"projects": projects, "runs": runs})
}

// ── Projects ──────────────────────────────────────────────────

func (s *Server) listProjects(ctx context.Context, c *app.RequestContext) {
	// Admin "archived" view (?archived=true) — a flat list for the settings page,
	// kept on this route to avoid a /projects/:id wildcard conflict.
	if string(c.Query("archived")) == "true" {
		s.listArchivedProjects(ctx, c)
		return
	}

	// Optional server-side filters (repeated query params): ?workspace=slug&tags=key:value
	// Empty slice = no filter for that dimension.
	workspaces := queryStrings(c, "workspace")
	tags := queryStrings(c, "tags")
	needsGrouping := string(c.Query("needsGrouping")) == "true"

	rows, err := s.deps.Q.ListProjectsWithLastRun(ctx, db.ListProjectsWithLastRunParams{
		Workspaces:    workspaces,
		Tags:          tags,
		NeedsGrouping: needsGrouping,
	})
	if err != nil {
		apiInternal(ctx, c, "failed to list projects")
		return
	}

	// Per-project health (recent run outcomes), keyed by project id.
	health := map[string]*projectHealth{}
	if org, oerr := s.deps.Q.GetOrg(ctx); oerr == nil {
		if hrows, herr := s.deps.Q.ProjectHealthByOrg(ctx, org.ID); herr == nil {
			for _, h := range hrows {
				health[h.ProjectID] = buildProjectHealth(h.RecentStatuses, h.TotalRuns, h.SucceededRuns)
			}
		}
	}

	result := make([]projectResponse, 0, len(rows))
	for _, row := range rows {
		p := projectResponse{
			ID:        row.ID,
			Name:      row.Name,
			Repo:      row.RepoPath,
			Workspace: row.Workspace,
			Colour:    row.Colour,
			Tags:      row.Tags,
			Health:    health[row.ID],
			CreatedAt: row.CreatedAt.Format(time.RFC3339),
			Inferred:  row.Inferred,
		}
		if p.Tags == nil {
			p.Tags = []string{}
		}

		if row.LastRunID != "" {
			duration := "0s"
			if row.LastRunDurationMs.Valid {
				duration = formatDuration(row.LastRunDurationMs.Int32)
			}
			startedAt := ""
			if !row.LastRunStartedAt.IsZero() {
				startedAt = row.LastRunStartedAt.Format(time.RFC3339)
			}
			p.LastRun = &lastRunResponse{
				ID:          row.LastRunID,
				Status:      row.LastRunStatus,
				Branch:      derefString(row.LastRunBranch),
				Duration:    duration,
				TriggeredBy: derefString(row.LastRunTriggeredBy),
				StartedAt:   startedAt,
			}
		}

		result = append(result, p)
	}

	c.JSON(consts.StatusOK, utils.H{"items": result})
}

// handleSetProjectTags replaces a project's tags. Tags are constrained to the
// curated registry: only declared key:value pairs are accepted; anything else
// (free tags, undeclared values) is dropped, so projects can only carry
// predeclared tags and legacy free tags normalize away on the next edit.
func (s *Server) handleSetProjectTags(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	var req struct {
		Tags []string `json:"tags"`
	}
	if err := c.BindJSON(&req); err != nil {
		apiBadRequest(ctx, c, "invalid request body")
		return
	}

	// Build the set of allowed "key:value" tags from the registry.
	allowed := map[string]bool{}
	if org, err := s.deps.Q.GetOrg(ctx); err == nil {
		if keys, err := s.deps.Q.ListTagKeys(ctx, org.ID); err == nil {
			for _, k := range keys {
				for _, v := range k.AllowedValues {
					allowed[k.Key+":"+v] = true
				}
			}
		}
	}
	tags := make([]string, 0, len(req.Tags))
	seen := map[string]bool{}
	for _, t := range req.Tags {
		if allowed[t] && !seen[t] {
			tags = append(tags, t)
			seen[t] = true
		}
	}

	if err := s.deps.Q.UpdateProjectTags(ctx, db.UpdateProjectTagsParams{ID: id, Tags: tags}); err != nil {
		apiInternal(ctx, c, "failed to update tags")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true, "tags": tags})
}

func (s *Server) getProject(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")

	row, err := s.deps.Q.GetProjectBasic(ctx, id)
	if err != nil {
		apiNotFound(ctx, c, "project not found")
		return
	}

	p := projectResponse{
		ID:        row.ID,
		Name:      row.Name,
		Repo:      row.RepoPath,
		Workspace: row.Workspace,
		Colour:    row.Colour,
		Tags:      row.Tags,
		CreatedAt: row.CreatedAt.Format(time.RFC3339),
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}

	// Most recent run for the project (separate query keeps nullability clean).
	if runs, _ := s.deps.Q.ListRunsByProject(ctx, db.ListRunsByProjectParams{ProjectID: &id, Limit: 1}); len(runs) > 0 {
		lr := runs[0]
		p.LastRun = &lastRunResponse{
			ID:          lr.ID,
			Status:      lr.Status,
			Branch:      derefString(lr.TriggerRef),
			Duration:    durationFromInt4(lr.DurationMs),
			TriggeredBy: derefString(lr.TriggeredBy),
			StartedAt:   lr.StartedAt.Format(time.RFC3339),
		}
	}

	if h, herr := s.deps.Q.ProjectHealthByID(ctx, &id); herr == nil {
		p.Health = buildProjectHealth(h.RecentStatuses, h.TotalRuns, h.SucceededRuns)
	}

	c.JSON(consts.StatusOK, p)
}

func (s *Server) handleListProjectPipelines(ctx context.Context, c *app.RequestContext) {
	projectID := c.Param("id")

	info, err := s.deps.Q.GetProjectRepoInfo(ctx, projectID)
	if err != nil {
		apiNotFound(ctx, c, "project not found")
		return
	}

	// Fetch pipeline files from the forge.
	files, err := s.deps.Forge.GetDirectory(ctx, info.RepoPath, "HEAD", info.PipelinePath)
	if err != nil {
		// Forge unavailable or no pipeline dir — return empty.
		c.JSON(consts.StatusOK, utils.H{"items": []any{}})
		return
	}

	type pipelineItem struct {
		Filename string   `json:"filename"`
		Yaml     string   `json:"yaml"`
		Status   string   `json:"status"`
		Errors   []string `json:"errors,omitempty"`
		Steps    []any    `json:"steps"`
	}

	var items []pipelineItem
	for name, content := range files {
		if !isYAMLFile(name) {
			continue
		}

		item := pipelineItem{
			Filename: name,
			Yaml:     string(content), // raw source for the YAML tab
			Status:   "valid",
			Steps:    []any{},
		}

		p, parseErr := pipeline.Parse(content)
		if parseErr != nil {
			item.Status = "invalid"
			item.Errors = []string{parseErr.Error()}
		} else {
			for _, step := range p.Steps {
				item.Steps = append(item.Steps, map[string]any{
					"name":     step.Name,
					"execType": step.ExecType(),
					"wave":     0,
				})
			}
		}

		items = append(items, item)
	}

	if items == nil {
		items = []pipelineItem{}
	}
	c.JSON(consts.StatusOK, utils.H{"items": items})
}

func isYAMLFile(name string) bool {
	return len(name) > 5 && (name[len(name)-5:] == ".yaml" || name[len(name)-4:] == ".yml")
}

// ── Runs ──────────────────────────────────────────────────────

func (s *Server) handleListRuns(ctx context.Context, c *app.RequestContext) {
	projectID := string(c.Query("projectId"))
	status := string(c.Query("status"))
	p := parsePagination(c)

	// Keyset cursor: (started_at, id) tuple from the previous page's last row.
	var cursorTs, cursorID string
	if p.Cursor != "" {
		if id, ts, err := decodeCursor(p.Cursor); err == nil {
			cursorID, cursorTs = id, ts
		}
	}

	rows, err := s.deps.Q.ListRunsFiltered(ctx, db.ListRunsFilteredParams{
		ProjectID: projectID,
		Status:    status,
		CursorTs:  cursorTs,
		CursorID:  cursorID,
		Lim:       int32(p.Limit + 1),
	})
	if err != nil {
		apiInternal(ctx, c, "failed to list runs")
		return
	}

	result := make([]runResponse, 0, len(rows))
	for _, row := range rows {
		r := runResponse{
			ID:            row.ID,
			ProjectID:     row.ProjectID,
			ProjectName:   derefString(row.ProjectName),
			ProjectColour: row.ProjectColour,
			Repo:          row.RepoPath,
			Status:        row.Status,
			TriggerType:   row.TriggerType,
			Branch:        derefString(row.Branch),
			CommitSha:     derefString(row.CommitSha),
			CommitMessage: derefString(row.CommitMessage),
			TriggeredBy:   derefString(row.TriggeredBy),
			WorkflowFile:  derefString(row.WorkflowFile),
			Duration:      durationFromInt4(row.DurationMs),
			StartedAt:     row.StartedAt.Format(time.RFC3339),
			StartedAtTs:   row.StartedAt.UnixMilli(),
			FinishedAt:    formatTimePtrOpt(row.FinishedAt),
			Environment:   row.Environment,
			ErrorMessage:  row.ErrorMessage,
			Steps:         decodeStepSummaries(row.Steps),
		}
		if row.FinishedAt != nil {
			ts := row.FinishedAt.UnixMilli()
			r.FinishedAtTs = &ts
		}
		result = append(result, r)
	}

	if result == nil {
		result = []runResponse{}
	}

	// Handle cursor pagination. The cursor timestamp must be full precision: the
	// display StartedAt is RFC3339 (second-granularity), which would skip rows
	// sharing a second on the next page. Encode from the original row time.
	var nextCursor string
	if len(result) > p.Limit {
		result = result[:p.Limit]
		if len(result) > 0 {
			last := result[len(result)-1]
			nextCursor = encodeCursor(last.ID, rows[p.Limit-1].StartedAt.Format(time.RFC3339Nano))
		}
	}

	resp := utils.H{"items": result}
	if nextCursor != "" {
		resp["nextCursor"] = nextCursor
	}
	c.JSON(consts.StatusOK, resp)
}

func (s *Server) getRun(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")

	row, err := s.deps.Q.GetRunDetail(ctx, id)
	if err != nil {
		apiNotFound(ctx, c, "run not found")
		return
	}

	r := runResponse{
		ID:            row.ID,
		ProjectID:     row.ProjectID,
		ProjectName:   derefString(row.ProjectName),
		ProjectColour: row.ProjectColour,
		Repo:          row.RepoPath,
		Status:        row.Status,
		TriggerType:   row.TriggerType,
		Branch:        derefString(row.Branch),
		CommitSha:     derefString(row.CommitSha),
		CommitMessage: derefString(row.CommitMessage),
		TriggeredBy:   derefString(row.TriggeredBy),
		WorkflowFile:  derefString(row.WorkflowFile),
		Duration:      durationFromInt4(row.DurationMs),
		StartedAt:     row.StartedAt.Format(time.RFC3339),
		StartedAtTs:   row.StartedAt.UnixMilli(),
		FinishedAt:    formatTimePtrOpt(row.FinishedAt),
		Environment:   row.Environment,
		ErrorMessage:  row.ErrorMessage,
		Steps:         decodeStepSummaries(row.Steps),
	}
	if row.FinishedAt != nil {
		ts := row.FinishedAt.UnixMilli()
		r.FinishedAtTs = &ts
	}

	c.JSON(consts.StatusOK, r)
}

// decodeStepSummaries parses the json_agg step array from the run queries.
func decodeStepSummaries(b []byte) []runStepSummary {
	if len(b) == 0 {
		return nil
	}
	var out []runStepSummary
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

// durationFromInt4 formats a nullable duration_ms column as a human string.
func durationFromInt4(d pgtype.Int4) string {
	if !d.Valid {
		return "0s"
	}
	return formatDuration(d.Int32)
}

func (s *Server) triggerRun(ctx context.Context, c *app.RequestContext) {
	var req struct {
		ProjectID    string `json:"projectId"`
		Branch       string `json:"branch"`
		WorkflowFile string `json:"workflowFile"`
		Environment  string `json:"environment"`
	}
	if err := c.BindJSON(&req); err != nil {
		apiBadRequest(ctx, c, "invalid request body")
		return
	}
	if req.ProjectID == "" {
		apiBadRequest(ctx, c, "projectId is required")
		return
	}
	if s.deps.Runs == nil {
		apiInternal(ctx, c, "run creation unavailable")
		return
	}
	runID, workflowID, err := s.deps.Runs.TriggerManual(ctx, req.ProjectID, req.Branch, req.WorkflowFile, req.Environment)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			apiNotFound(ctx, c, err.Error())
		} else {
			apiInternal(ctx, c, err.Error())
		}
		return
	}
	c.JSON(consts.StatusAccepted, utils.H{"id": runID, "workflowId": workflowID, "status": "pending"})
}

// ── Runners ──────────────────────────────────────────────────

func (s *Server) listRunners(ctx context.Context, c *app.RequestContext) {
	lim := parsePagination(c).Limit
	off := listOffset(c)
	rows, err := s.deps.Q.ListMachinePoolsPaged(ctx, db.ListMachinePoolsPagedParams{
		Limit:  int32(lim),
		Offset: int32(off),
	})
	if err != nil {
		apiInternal(ctx, c, "failed to list runners")
		return
	}

	result := make([]runnerResponse, 0, len(rows))
	for _, r := range rows {
		rr := runnerResponse{
			ID:             r.ID,
			Name:           r.Name,
			Description:    r.Description,
			Provider:       r.Provider,
			CPU:            r.Cpu,
			Memory:         r.Memory,
			Disk:           r.Disk,
			Arch:           r.Arch,
			GPUVendor:      r.GpuVendor,
			GPUModel:       r.GpuModel,
			CapacityType:   r.CapacityType,
			Objective:      r.Objective,
			MinWarm:        r.MinWarm,
			MaxMachines:    r.MaxMachines,
			IdleTTLSeconds: r.IdleTtlSeconds,
			IsDefault:      r.IsDefault,
			Ready:          r.Ready,
			CreatedAt:      r.CreatedAt.Format(time.RFC3339),
		}
		if r.GpuCount.Valid {
			v := r.GpuCount.Int32
			rr.GPUCount = &v
		}
		result = append(result, rr)
	}

	paginatedResponse(c, result, PaginationResponse{NextCursor: nextOffsetCursor(off, lim, len(result))})
}

// ── Org (sqlc) ──────────────────────────────────────────────

func (s *Server) getOrg(ctx context.Context, c *app.RequestContext) {
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiNotFound(ctx, c, "no org configured")
		return
	}
	c.JSON(consts.StatusOK, utils.H{
		"id":                      org.ID,
		"name":                    org.Name,
		"slug":                    org.Slug,
		"concurrencyLimit":        org.ConcurrencyLimit,
		"requireProjectWorkspace": org.RequireProjectWorkspace,
	})
}

// handleSetOrgPolicy updates org-level governance policies. Currently just the
// "require a workspace on every project" switch.
func (s *Server) handleSetOrgPolicy(ctx context.Context, c *app.RequestContext) {
	var req struct {
		RequireProjectWorkspace bool `json:"requireProjectWorkspace"`
	}
	if err := c.BindJSON(&req); err != nil {
		apiBadRequest(ctx, c, "invalid request body")
		return
	}
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiNotFound(ctx, c, "no org configured")
		return
	}
	if err := s.deps.Q.SetOrgRequireProjectWorkspace(ctx, db.SetOrgRequireProjectWorkspaceParams{
		ID: org.ID, Require: req.RequireProjectWorkspace,
	}); err != nil {
		apiInternal(ctx, c, "failed to update org policy")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true})
}

// ── Teams -- get by ID ───────────────────────────────────────

type teamMemberResponse struct {
	ID    string  `json:"id"`
	Email string  `json:"email"`
	Name  *string `json:"name,omitempty"`
}

type teamWithMembersResponse struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Slug        string               `json:"slug"`
	Source      string               `json:"source"`
	IDPGroup    *string              `json:"idpGroup,omitempty"`
	MemberCount int                  `json:"memberCount"`
	Members     []teamMemberResponse `json:"members"`
}

func (s *Server) handleGetTeam(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")

	t, err := s.deps.Q.GetTeam(ctx, id)
	if err != nil {
		apiNotFound(ctx, c, "team not found")
		return
	}
	team := teamWithMembersResponse{
		ID:       t.ID,
		Name:     t.Name,
		Slug:     t.Slug,
		Source:   t.Source,
		IDPGroup: t.IdpGroup,
	}
	if team.Source == "" {
		team.Source = "internal"
	}

	// Get members.
	members, err := s.deps.Q.ListTeamMembers(ctx, id)
	if err != nil {
		team.Members = []teamMemberResponse{}
	} else {
		team.Members = make([]teamMemberResponse, 0, len(members))
		for _, m := range members {
			team.Members = append(team.Members, teamMemberResponse{
				ID:    m.ID,
				Email: m.Email,
				Name:  m.Name,
			})
		}
	}
	team.MemberCount = len(team.Members)

	c.JSON(consts.StatusOK, team)
}

// ── Roles -- update ──────────────────────────────────────────

func (s *Server) handleUpdateRole(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	var req struct {
		Name         *string           `json:"name,omitempty"`
		Description  *string           `json:"description,omitempty"`
		Permissions  []auth.Permission `json:"permissions,omitempty"`
		Workspaces   []string          `json:"workspaces,omitempty"`
		Environments []string          `json:"environments,omitempty"`
	}
	if err := c.BindJSON(&req); err != nil {
		apiBadRequest(ctx, c, "invalid request body")
		return
	}

	role, err := s.deps.Q.GetRoleByID(ctx, id)
	if err != nil {
		apiNotFound(ctx, c, "role not found")
		return
	}
	if role.IsSystem {
		apiBadRequest(ctx, c, "system roles cannot be modified")
		return
	}

	// Name / description.
	if req.Name != nil || req.Description != nil {
		if _, err := s.deps.Q.UpdateRole(ctx, db.UpdateRoleParams{
			ID:          id,
			Name:        req.Name,
			Description: req.Description,
		}); err != nil {
			apiInternal(ctx, c, "failed to update role")
			return
		}
	}

	// Replace permissions / scope when provided. role_permissions and the scope
	// tables are the source of truth; RegenerateForRole rebuilds Casbin from them.
	if req.Permissions != nil {
		if err := s.deps.Q.DeleteRolePermissions(ctx, id); err != nil {
			apiInternal(ctx, c, "failed to update permissions")
			return
		}
		for _, perm := range req.Permissions {
			if err := s.deps.Q.InsertRolePermission(ctx, db.InsertRolePermissionParams{
				RoleID: id, Object: perm.Object, Action: perm.Action,
			}); err != nil {
				apiInternal(ctx, c, "failed to update permissions")
				return
			}
		}
	}
	if req.Workspaces != nil {
		if err := s.deps.Q.DeleteRoleWorkspaceScopes(ctx, id); err != nil {
			apiInternal(ctx, c, "failed to update workspace scope")
			return
		}
		for _, slug := range req.Workspaces {
			_ = s.deps.Q.InsertRoleWorkspaceScope(ctx, db.InsertRoleWorkspaceScopeParams{RoleID: id, Slug: slug})
		}
	}
	if req.Environments != nil {
		if err := s.deps.Q.DeleteRoleEnvironmentScopes(ctx, id); err != nil {
			apiInternal(ctx, c, "failed to update environment scope")
			return
		}
		for _, slug := range req.Environments {
			_ = s.deps.Q.InsertRoleEnvironmentScope(ctx, db.InsertRoleEnvironmentScopeParams{RoleID: id, Slug: slug})
		}
	}

	// Apply immediately for every subject holding this role.
	if s.deps.Enforcer != nil {
		if err := auth.RegenerateForRole(ctx, s.deps.Q, s.deps.DB, s.deps.Enforcer, id); err != nil {
			apiInternal(ctx, c, "failed to apply role update")
			return
		}
	}

	c.JSON(consts.StatusOK, utils.H{"success": true})
}

// ── Helpers ──────────────────────────────────────────────────

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func formatDuration(ms int32) string {
	d := time.Duration(ms) * time.Millisecond
	if d < time.Second {
		return "0s"
	}
	if d < time.Minute {
		return d.Truncate(time.Second).String()
	}
	return d.Truncate(time.Second).String()
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

func formatTimePtrOpt(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

// ensure imports are used
var (
	_ = auth.RoleViewer
	_ = observe.RequestID
)
