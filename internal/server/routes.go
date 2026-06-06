package server

import (
	"context"
	"time"

	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/NerdMeNot/flint/pkg/secret"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
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
	CreatedAt      string           `json:"createdAt"`
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
	Environment   *string `json:"environment,omitempty"`
}

type runnerResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	CPU         string  `json:"cpu"`
	Memory      string  `json:"memory"`
	Arch        string  `json:"arch"`
	GPUVendor   *string `json:"gpuVendor,omitempty"`
	GPUModel    *string `json:"gpuModel,omitempty"`
	GPUCount    *int32  `json:"gpuCount,omitempty"`
	Ready       bool    `json:"ready"`
	CreatedAt   string  `json:"createdAt"`
}

func (s *Server) registerAPIRoutes() {
	v1 := s.hertz.Group("/api/v1",
		s.requestIDMiddleware(),
		maxBodyMiddleware(1<<20),
		s.authMiddleware(),
	)

	// Stats (replaces dashboard).
	v1.GET("/stats", s.requirePermission(auth.ObjWorkspace, auth.ActRead), s.handleStats)
	v1.GET("/search", s.requirePermission(auth.ObjProject, auth.ActRead), s.handleSearch)

	// Org.
	v1.GET("/org", s.requirePermission(auth.ObjWorkspace, auth.ActRead), s.getOrg)

	// Projects.
	v1.GET("/projects", s.requirePermission(auth.ObjProject, auth.ActRead), s.listProjects)
	v1.GET("/projects/:id", s.requirePermission(auth.ObjProject, auth.ActRead), s.getProject)
	v1.GET("/projects/:id/pipelines", s.requirePermission(auth.ObjProject, auth.ActRead), s.handleListProjectPipelines)
	v1.GET("/projects/:id/webhooks", s.requirePermission(auth.ObjProject, auth.ActWrite), s.handleListWebhooks)
	v1.POST("/projects/:id/webhooks", s.requirePermission(auth.ObjProject, auth.ActWrite), s.handleCreateWebhook)
	v1.DELETE("/projects/:id/webhooks/:webhookId", s.requirePermission(auth.ObjProject, auth.ActWrite), s.handleDeleteWebhook)

	// Runs (global list + per-run operations).
	v1.GET("/runs", s.requirePermission(auth.ObjRun, auth.ActRead), s.handleListRuns)
	v1.GET("/runs/:id", s.requirePermission(auth.ObjRun, auth.ActRead), s.getRun)
	v1.POST("/runs", s.requirePermission(auth.ObjRun, auth.ActTrigger), s.triggerRun)
	v1.GET("/runs/:id/steps", s.requirePermission(auth.ObjRun, auth.ActRead), s.handleGetRunSteps)
	v1.GET("/runs/:id/steps/:step/logs", s.requirePermission(auth.ObjRun, auth.ActRead), s.handleGetStepLogs)
	v1.GET("/runs/:id/steps/:step/logs/stream", s.requirePermission(auth.ObjRun, auth.ActRead), s.handleStreamStepLogs)
	v1.POST("/runs/:id/cancel", s.requirePermission(auth.ObjRun, auth.ActCancel), s.handleCancelRun)
	v1.POST("/runs/:id/retry", s.requirePermission(auth.ObjRun, auth.ActTrigger), s.handleRetryRun)

	// Gates.
	v1.GET("/gates", s.requirePermission(auth.ObjRun, auth.ActRead), s.handleListGates)
	v1.POST("/runs/:id/gates/:step/approve", s.requirePermission(auth.ObjGate, auth.ActApprove), s.handleApproveGate)
	v1.POST("/runs/:id/gates/:step/reject", s.requirePermission(auth.ObjGate, auth.ActReject), s.handleRejectGate)

	// Teams.
	v1.GET("/teams", s.requirePermission(auth.ObjTeam, auth.ActRead), s.handleListTeams)
	v1.GET("/teams/:id", s.requirePermission(auth.ObjTeam, auth.ActRead), s.handleGetTeam)
	v1.POST("/teams", s.requirePermission(auth.ObjTeam, auth.ActManage), s.handleCreateTeam)
	v1.DELETE("/teams/:id", s.requirePermission(auth.ObjTeam, auth.ActManage), s.handleDeleteTeam)

	// Users.
	v1.GET("/users", s.requirePermission(auth.ObjTeam, auth.ActRead), s.handleListUsers)

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
	v1.DELETE("/auth/provider/:type", s.requirePermission(auth.ObjRole, auth.ActManage), s.handleDeleteAuthProvider)

	// Forge connections (DB-managed; credentials envelope-encrypted server-side).
	v1.GET("/forge-connections", s.requirePermission(auth.ObjConnection, auth.ActRead), s.handleListForgeConnections)
	v1.POST("/forge-connections", s.requirePermission(auth.ObjConnection, auth.ActWrite), s.handleCreateForgeConnection)
	v1.PUT("/forge-connections", s.requirePermission(auth.ObjConnection, auth.ActWrite), s.handleUpdateForgeConnection)
	v1.DELETE("/forge-connections/:id", s.requirePermission(auth.ObjConnection, auth.ActWrite), s.handleDeleteForgeConnection)

	// Runners.
	v1.GET("/runners", s.requirePermission(auth.ObjRunner, auth.ActRead), s.listRunners)

	// Audit entries (renamed from audit-log).
	v1.GET("/audit-entries", s.requirePermission(auth.ObjAudit, auth.ActRead), s.handleListAuditLog)

	// Auth.
	// Auth routes (login, callback, device flow) registered separately in server.go.
}

// ── Stats ─────────────────────────────────────────────────────

func (s *Server) handleStats(ctx context.Context, c *app.RequestContext) {
	// TODO: implement real stats queries
	c.JSON(consts.StatusOK, utils.H{
		"totalRuns":      0,
		"successRate":    0.0,
		"pendingGates":   0,
		"activeProjects": 0,
		"runsToday":      0,
		"avgDuration":    "0s",
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

	// Search projects by name/repo.
	projectRows, _ := s.deps.DB.Query(ctx,
		`SELECT id, COALESCE(display_name, repo_path) AS name, repo_path, colour
		 FROM projects WHERE org_id = $1 AND is_archived = false
		 AND (display_name ILIKE $2 OR repo_path ILIKE $2)
		 ORDER BY display_name LIMIT 10`, claims.OrgID, pattern)

	var projects []utils.H
	if projectRows != nil {
		defer projectRows.Close()
		for projectRows.Next() {
			var id, name, repo, colour string
			if projectRows.Scan(&id, &name, &repo, &colour) == nil {
				projects = append(projects, utils.H{
					"id": id, "name": name, "repo": repo, "colour": colour,
				})
			}
		}
	}
	if projects == nil {
		projects = []utils.H{}
	}

	// Search runs by commit SHA or branch.
	runRows, _ := s.deps.DB.Query(ctx,
		`SELECT pr.id, pr.status, pr.trigger_ref, pr.commit_sha,
		        COALESCE(p.display_name, p.repo_path) AS project_name, p.colour
		 FROM pipeline_runs pr
		 JOIN projects p ON p.id = pr.project_id
		 WHERE pr.org_id = $1
		 AND (pr.trigger_ref ILIKE $2 OR pr.commit_sha ILIKE $2)
		 ORDER BY pr.started_at DESC LIMIT 10`, claims.OrgID, pattern)

	var runs []utils.H
	if runRows != nil {
		defer runRows.Close()
		for runRows.Next() {
			var id, status, projectName, colour string
			var branch, sha *string
			if runRows.Scan(&id, &status, &branch, &sha, &projectName, &colour) == nil {
				r := utils.H{"id": id, "status": status, "projectName": projectName, "projectColour": colour}
				if branch != nil {
					r["branch"] = *branch
				}
				if sha != nil {
					r["commitSha"] = *sha
				}
				runs = append(runs, r)
			}
		}
	}
	if runs == nil {
		runs = []utils.H{}
	}

	c.JSON(consts.StatusOK, utils.H{"projects": projects, "runs": runs})
}

// ── Projects ──────────────────────────────────────────────────

func (s *Server) listProjects(ctx context.Context, c *app.RequestContext) {
	// Use a raw query that joins workspace + computes last run info.
	rows, err := s.deps.DB.Query(ctx, `
		SELECT p.id, COALESCE(p.display_name, p.repo_path) AS name, p.repo_path,
		       COALESCE(w.slug, '') AS workspace, p.colour, p.tags, p.created_at,
		       lr.id AS last_run_id, lr.status AS last_run_status,
		       lr.trigger_ref AS last_run_branch, lr.triggered_by AS last_run_triggered_by,
		       lr.started_at AS last_run_started_at, lr.duration_ms AS last_run_duration_ms
		FROM projects p
		LEFT JOIN workspaces w ON w.id = p.workspace_id
		LEFT JOIN LATERAL (
		    SELECT id, status, trigger_ref, triggered_by, started_at, duration_ms
		    FROM pipeline_runs
		    WHERE project_id = p.id
		    ORDER BY started_at DESC
		    LIMIT 1
		) lr ON true
		WHERE p.is_archived = false
		ORDER BY COALESCE(p.display_name, p.repo_path)
	`)
	if err != nil {
		apiInternal(ctx, c, "failed to list projects")
		return
	}
	defer rows.Close()

	var result []projectResponse
	for rows.Next() {
		var p projectResponse
		var createdAt time.Time
		var lastRunID, lastRunStatus, lastRunBranch, lastRunTriggeredBy *string
		var lastRunStartedAt *time.Time
		var lastRunDurationMs *int32

		if err := rows.Scan(
			&p.ID, &p.Name, &p.Repo,
			&p.Workspace, &p.Colour, &p.Tags, &createdAt,
			&lastRunID, &lastRunStatus,
			&lastRunBranch, &lastRunTriggeredBy,
			&lastRunStartedAt, &lastRunDurationMs,
		); err != nil {
			apiInternal(ctx, c, "failed to scan project")
			return
		}

		if p.Tags == nil {
			p.Tags = []string{}
		}
		p.CreatedAt = createdAt.Format(time.RFC3339)

		if lastRunID != nil && *lastRunID != "" {
			duration := "0s"
			if lastRunDurationMs != nil {
				duration = formatDuration(*lastRunDurationMs)
			}
			branch := ""
			if lastRunBranch != nil {
				branch = *lastRunBranch
			}
			triggeredBy := ""
			if lastRunTriggeredBy != nil {
				triggeredBy = *lastRunTriggeredBy
			}
			startedAt := ""
			if lastRunStartedAt != nil {
				startedAt = lastRunStartedAt.Format(time.RFC3339)
			}
			p.LastRun = &lastRunResponse{
				ID:          *lastRunID,
				Status:      derefString(lastRunStatus),
				Branch:      branch,
				Duration:    duration,
				TriggeredBy: triggeredBy,
				StartedAt:   startedAt,
			}
		}

		result = append(result, p)
	}

	if result == nil {
		result = []projectResponse{}
	}
	c.JSON(consts.StatusOK, utils.H{"items": result})
}

func (s *Server) getProject(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")

	var p projectResponse
	var createdAt time.Time
	var lastRunID, lastRunStatus, lastRunBranch, lastRunTriggeredBy *string
	var lastRunStartedAt *time.Time
	var lastRunDurationMs *int32

	err := s.deps.DB.QueryRow(ctx, `
		SELECT p.id, COALESCE(p.display_name, p.repo_path) AS name, p.repo_path,
		       COALESCE(w.slug, '') AS workspace, p.colour, p.tags, p.created_at,
		       lr.id AS last_run_id, lr.status AS last_run_status,
		       lr.trigger_ref AS last_run_branch, lr.triggered_by AS last_run_triggered_by,
		       lr.started_at AS last_run_started_at, lr.duration_ms AS last_run_duration_ms
		FROM projects p
		LEFT JOIN workspaces w ON w.id = p.workspace_id
		LEFT JOIN LATERAL (
		    SELECT id, status, trigger_ref, triggered_by, started_at, duration_ms
		    FROM pipeline_runs
		    WHERE project_id = p.id
		    ORDER BY started_at DESC
		    LIMIT 1
		) lr ON true
		WHERE p.id = $1
	`, id).Scan(
		&p.ID, &p.Name, &p.Repo,
		&p.Workspace, &p.Colour, &p.Tags, &createdAt,
		&lastRunID, &lastRunStatus,
		&lastRunBranch, &lastRunTriggeredBy,
		&lastRunStartedAt, &lastRunDurationMs,
	)
	if err != nil {
		apiNotFound(ctx, c, "project not found")
		return
	}

	if p.Tags == nil {
		p.Tags = []string{}
	}
	p.CreatedAt = createdAt.Format(time.RFC3339)

	if lastRunID != nil && *lastRunID != "" {
		duration := "0s"
		if lastRunDurationMs != nil {
			duration = formatDuration(*lastRunDurationMs)
		}
		p.LastRun = &lastRunResponse{
			ID:          *lastRunID,
			Status:      derefString(lastRunStatus),
			Branch:      derefString(lastRunBranch),
			Duration:    duration,
			TriggeredBy: derefString(lastRunTriggeredBy),
			StartedAt:   formatTimePtr(lastRunStartedAt),
		}
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

	// Use a raw query for global run list with optional filters.
	rows, err := s.deps.DB.Query(ctx, `
		SELECT pr.id, pr.status, pr.started_at, pr.workflow_file,
		       pr.trigger_ref AS branch, pr.trigger_type, pr.commit_sha,
		       pr.commit_message, pr.triggered_by, pr.duration_ms, pr.finished_at,
		       p.display_name AS project_name, p.id AS project_id, p.colour AS project_colour,
		       p.repo_path, pr.environment
		FROM pipeline_runs pr
		JOIN projects p ON p.id = pr.project_id
		WHERE ($1::text = '' OR pr.project_id = $1)
		  AND ($2::text = '' OR pr.status = $2)
		ORDER BY pr.started_at DESC
		LIMIT $3
	`, projectID, status, int32(p.Limit+1))
	if err != nil {
		apiInternal(ctx, c, "failed to list runs")
		return
	}
	defer rows.Close()

	var result []runResponse
	for rows.Next() {
		var r runResponse
		var startedAt time.Time
		var finishedAt *time.Time
		var durationMs *int32
		var branch, commitSha, commitMessage, triggeredBy, projectName *string

		if err := rows.Scan(
			&r.ID, &r.Status, &startedAt, &r.WorkflowFile,
			&branch, &r.TriggerType, &commitSha,
			&commitMessage, &triggeredBy, &durationMs, &finishedAt,
			&projectName, &r.ProjectID, &r.ProjectColour,
			&r.Repo, &r.Environment,
		); err != nil {
			apiInternal(ctx, c, "failed to scan run")
			return
		}

		r.Branch = derefString(branch)
		r.CommitSha = derefString(commitSha)
		r.CommitMessage = derefString(commitMessage)
		r.TriggeredBy = derefString(triggeredBy)
		r.ProjectName = derefString(projectName)
		r.StartedAt = startedAt.Format(time.RFC3339)
		r.FinishedAt = formatTimePtrOpt(finishedAt)
		r.Duration = "0s"
		if durationMs != nil {
			r.Duration = formatDuration(*durationMs)
		}

		result = append(result, r)
	}

	if result == nil {
		result = []runResponse{}
	}

	// Handle cursor pagination.
	var nextCursor string
	if len(result) > p.Limit {
		result = result[:p.Limit]
		if len(result) > 0 {
			last := result[len(result)-1]
			nextCursor = encodeCursor(last.ID, last.StartedAt)
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

	var r runResponse
	var startedAt time.Time
	var finishedAt *time.Time
	var durationMs *int32
	var branch, commitSha, commitMessage, triggeredBy, projectName *string

	err := s.deps.DB.QueryRow(ctx, `
		SELECT pr.id, pr.status, pr.started_at, pr.workflow_file,
		       pr.trigger_ref AS branch, pr.trigger_type, pr.commit_sha,
		       pr.commit_message, pr.triggered_by, pr.duration_ms, pr.finished_at,
		       p.display_name AS project_name, p.id AS project_id, p.colour AS project_colour,
		       p.repo_path, pr.environment
		FROM pipeline_runs pr
		JOIN projects p ON p.id = pr.project_id
		WHERE pr.id = $1
	`, id).Scan(
		&r.ID, &r.Status, &startedAt, &r.WorkflowFile,
		&branch, &r.TriggerType, &commitSha,
		&commitMessage, &triggeredBy, &durationMs, &finishedAt,
		&projectName, &r.ProjectID, &r.ProjectColour,
		&r.Repo, &r.Environment,
	)
	if err != nil {
		apiNotFound(ctx, c, "run not found")
		return
	}

	r.Branch = derefString(branch)
	r.CommitSha = derefString(commitSha)
	r.CommitMessage = derefString(commitMessage)
	r.TriggeredBy = derefString(triggeredBy)
	r.ProjectName = derefString(projectName)
	r.StartedAt = startedAt.Format(time.RFC3339)
	r.FinishedAt = formatTimePtrOpt(finishedAt)
	r.Duration = "0s"
	if durationMs != nil {
		r.Duration = formatDuration(*durationMs)
	}

	c.JSON(consts.StatusOK, r)
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
	if req.WorkflowFile == "" {
		req.WorkflowFile = "ci.yaml"
	}
	if req.Branch == "" {
		req.Branch = "main"
	}

	info, err := s.deps.Q.GetProjectRepoInfo(ctx, req.ProjectID)
	if err != nil {
		apiNotFound(ctx, c, "project not found")
		return
	}

	pipelinePath := info.PipelinePath
	if pipelinePath == "" {
		pipelinePath = ".flint/"
	}

	runID := observe.RequestID(ctx)

	var env *string
	if req.Environment != "" {
		env = &req.Environment
	}

	err = s.deps.Q.InsertManualRun(ctx, db.InsertManualRunParams{
		ID: runID, ProjectID: req.ProjectID, OrgID: info.OrgID,
		WorkflowFile: req.WorkflowFile, TriggerRef: &req.Branch,
		Environment: env,
	})
	if err != nil {
		apiInternal(ctx, c, "failed to create run")
		return
	}

	var workflowID string
	if s.deps.Engine != nil {
		var startErr error
		workflowID, startErr = s.deps.Engine.StartWorkflow(ctx, engine.StartWorkflowInput{
			RunID: runID, OrgID: info.OrgID, ProjectID: req.ProjectID,
			Repo: info.RepoPath, Ref: req.Branch,
			TriggerType: "manual", TriggeredBy: "api",
			WorkflowFile: req.WorkflowFile, PipelinePath: pipelinePath,
			Environment: req.Environment,
		})
		if startErr != nil {
			errMsg := startErr.Error()
			_ = s.deps.Q.FailRunWithError(ctx, db.FailRunWithErrorParams{
				ID:           runID,
				ErrorMessage: &errMsg,
			})
		}
	}

	c.JSON(consts.StatusAccepted, utils.H{
		"id": runID, "workflowId": workflowID, "status": "pending",
	})
}

// ── Secrets (legacy -- to be replaced by env variables) ──────

func (s *Server) listSecrets(ctx context.Context, c *app.RequestContext) {
	projectID := c.Param("id")
	secrets, err := s.deps.Q.ListProjectSecrets(ctx, &projectID)
	if err != nil {
		apiInternal(ctx, c, "failed to list secrets")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"items": secrets})
}

func (s *Server) createSecret(ctx context.Context, c *app.RequestContext) {
	projectID := c.Param("id")
	var req struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if c.BindJSON(&req) != nil || req.Name == "" || req.Value == "" {
		apiBadRequest(ctx, c, "name and value are required")
		return
	}
	if s.deps.Secrets == nil {
		apiInternal(ctx, c, "secret store not configured")
		return
	}

	orgID, err := s.deps.Q.GetProjectOrgID(ctx, projectID)
	if err != nil {
		apiNotFound(ctx, c, "project not found")
		return
	}

	if err := s.deps.Secrets.Set(ctx, secret.Ref{OrgID: orgID, ProjectID: projectID, Name: req.Name}, req.Value); err != nil {
		apiInternal(ctx, c, "failed to store secret")
		return
	}
	c.JSON(consts.StatusCreated, utils.H{"name": req.Name, "status": "created"})
}

func (s *Server) deleteSecret(ctx context.Context, c *app.RequestContext) {
	projectID := c.Param("id")
	name := c.Param("name")

	orgID, err := s.deps.Q.GetProjectOrgID(ctx, projectID)
	if err != nil {
		apiNotFound(ctx, c, "project not found")
		return
	}

	if err := s.deps.Secrets.Delete(ctx, secret.Ref{OrgID: orgID, ProjectID: projectID, Name: name}); err != nil {
		apiNotFound(ctx, c, "secret not found")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"status": "ok"})
}

// ── Runners ──────────────────────────────────────────────────

func (s *Server) listRunners(ctx context.Context, c *app.RequestContext) {
	rows, err := s.deps.DB.Query(ctx, `
		SELECT id, name, description, cpu, memory, arch,
		       gpu_vendor, gpu_model, gpu_count, ready, created_at
		FROM runner_pools
		ORDER BY name
	`)
	if err != nil {
		apiInternal(ctx, c, "failed to list runners")
		return
	}
	defer rows.Close()

	var result []runnerResponse
	for rows.Next() {
		var r runnerResponse
		var createdAt time.Time
		var gpuCount *int32

		if err := rows.Scan(
			&r.ID, &r.Name, &r.Description, &r.CPU, &r.Memory, &r.Arch,
			&r.GPUVendor, &r.GPUModel, &gpuCount, &r.Ready, &createdAt,
		); err != nil {
			apiInternal(ctx, c, "failed to scan runner")
			return
		}

		r.GPUCount = gpuCount
		r.CreatedAt = createdAt.Format(time.RFC3339)
		result = append(result, r)
	}

	if result == nil {
		result = []runnerResponse{}
	}
	c.JSON(consts.StatusOK, utils.H{"items": result})
}

// ── Org (sqlc) ──────────────────────────────────────────────

func (s *Server) getOrg(ctx context.Context, c *app.RequestContext) {
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiNotFound(ctx, c, "no org configured")
		return
	}
	c.JSON(consts.StatusOK, org)
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

	// Get team info.
	var team teamWithMembersResponse
	var source, idpGroup *string
	err := s.deps.DB.QueryRow(ctx,
		`SELECT id, name, slug,
		        COALESCE(source, 'internal') AS source,
		        idp_group
		 FROM teams WHERE id = $1`, id,
	).Scan(&team.ID, &team.Name, &team.Slug, &source, &idpGroup)
	if err != nil {
		apiNotFound(ctx, c, "team not found")
		return
	}

	team.Source = derefString(source)
	if team.Source == "" {
		team.Source = "internal"
	}
	team.IDPGroup = idpGroup

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

	// TODO: update role in DB + regenerate Casbin policies
	_ = id
	_ = req

	c.JSON(consts.StatusOK, utils.H{"status": "ok"})
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
