package server

import (
	"context"

	"github.com/NerdMeNot/flint/internal/auth"
	"github.com/NerdMeNot/flint/internal/db"
	"github.com/NerdMeNot/flint/internal/engine"
	"github.com/NerdMeNot/flint/internal/observe"
	"github.com/NerdMeNot/flint/pkg/secret"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func (s *Server) registerAPIRoutes() {
	v1 := s.hertz.Group("/api/v1",
		s.requestIDMiddleware(),
		maxBodyMiddleware(1<<20),
		s.authMiddleware(),
	)

	// Dashboard.
	v1.GET("/dashboard/summary", s.requirePermission("org", "read"), s.handleDashboardSummary)
	v1.GET("/dashboard/activity", s.requirePermission("org", "read"), s.handleDashboardActivity)

	// Org.
	v1.GET("/org", s.requirePermission("org", "read"), s.getOrg)

	// Projects.
	v1.GET("/projects", s.requirePermission("project", "read"), s.listProjects)
	v1.GET("/projects/:id", s.requirePermission("project", "read"), s.getProject)

	// Runs.
	v1.GET("/projects/:id/runs", s.requirePermission("pipeline", "read"), s.listRuns)
	v1.GET("/runs/:id", s.requirePermission("pipeline", "read"), s.getRun)
	v1.POST("/runs", s.requirePermission("pipeline", "run"), s.triggerRun)
	v1.GET("/runs/:id/steps", s.requirePermission("pipeline", "read"), s.handleGetRunSteps)
	v1.POST("/runs/:id/cancel", s.requirePermission("pipeline", "cancel"), s.handleCancelRun)
	v1.POST("/runs/:id/retry", s.requirePermission("pipeline", "run"), s.handleRetryRun)
	v1.POST("/runs/:id/approve", s.requirePermission("gate", "approve"), s.handleApproveGate)
	v1.POST("/runs/:id/reject", s.requirePermission("gate", "approve"), s.handleRejectGate)

	// Gates.
	v1.GET("/gates", s.requirePermission("pipeline", "read"), s.handleListGates)

	// Logs.
	v1.GET("/runs/:id/logs/:step", s.requirePermission("pipeline", "read"), s.handleGetStepLogs)

	// Secrets.
	v1.GET("/projects/:id/secrets", s.requirePermission("secret", "read"), s.listSecrets)
	v1.POST("/projects/:id/secrets", s.requirePermission("secret", "create"), s.createSecret)
	v1.DELETE("/projects/:id/secrets/:name", s.requirePermission("secret", "delete"), s.deleteSecret)
	v1.GET("/org/secrets", s.requirePermission("secret", "read"), s.handleListOrgSecrets)

	// Runners.
	v1.GET("/runners", s.requirePermission("runner", "read"), s.listRunners)

	// Teams.
	v1.GET("/teams", s.requirePermission("org", "read"), s.handleListTeams)
	v1.POST("/teams", s.requirePermission("org", "manage"), s.handleCreateTeam)
	v1.DELETE("/teams/:id", s.requirePermission("org", "manage"), s.handleDeleteTeam)

	// Users.
	v1.GET("/users", s.requirePermission("org", "read"), s.handleListUsers)

	// Forge.
	v1.GET("/forge-connections", s.requirePermission("org", "read"), s.handleListForgeConnections)

	// API keys.
	v1.GET("/api-keys", s.requirePermission("org", "read"), s.handleListAPIKeys)
	v1.POST("/api-keys", s.requirePermission("org", "manage"), s.handleCreateAPIKey)
	v1.DELETE("/api-keys/:id", s.requirePermission("org", "manage"), s.handleDeleteAPIKey)

	// RBAC — roles and role assignments.
	v1.GET("/roles", s.requirePermission("rbac", "manage"), s.handleListRoles)
	v1.POST("/roles", s.requirePermission("rbac", "manage"), s.handleCreateRole)
	v1.DELETE("/roles/:id", s.requirePermission("rbac", "manage"), s.handleDeleteRole)
	v1.GET("/role-assignments", s.requirePermission("rbac", "manage"), s.handleListAssignments)
	v1.POST("/role-assignments", s.requirePermission("rbac", "manage"), s.handleCreateAssignment)
	v1.DELETE("/role-assignments/:id", s.requirePermission("rbac", "manage"), s.handleDeleteAssignment)

	// Protected environments.
	v1.GET("/environments", s.requirePermission("org", "read"), s.handleListEnvironments)
	v1.POST("/environments", s.requirePermission("org", "manage"), s.handleCreateEnvironment)
	v1.PUT("/environments/:id", s.requirePermission("org", "manage"), s.handleUpdateEnvironment)
	v1.DELETE("/environments/:id", s.requirePermission("org", "manage"), s.handleDeleteEnvironment)

	// Workspaces.
	v1.GET("/workspaces", s.requirePermission("org", "read"), s.handleListWorkspaces)
	v1.POST("/workspaces", s.requirePermission("org", "manage"), s.handleCreateWorkspace)
	v1.DELETE("/workspaces/:id", s.requirePermission("org", "manage"), s.handleDeleteWorkspace)

	// Audit.
	v1.GET("/audit-log", s.requirePermission("audit", "read"), s.handleListAuditLog)

	// Modules.
	v1.GET("/modules", s.requirePermission("org", "read"), s.handleListModules)
}

// ── Projects (sqlc) ─────────────────────────────────────────

func (s *Server) listProjects(ctx context.Context, c *app.RequestContext) {
	projects, err := s.deps.Q.ListProjects(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to list projects")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"projects": projects})
}

func (s *Server) getProject(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	project, err := s.deps.Q.GetProject(ctx, id)
	if err != nil {
		apiNotFound(ctx, c, "project not found")
		return
	}
	c.JSON(consts.StatusOK, project)
}

// ── Runs (sqlc) ─────────────────────────────────────────────

func (s *Server) listRuns(ctx context.Context, c *app.RequestContext) {
	projectID := c.Param("id")
	runs, err := s.deps.Q.ListRunsByProject(ctx, db.ListRunsByProjectParams{
		ProjectID: projectID, Limit: 50,
	})
	if err != nil {
		apiInternal(ctx, c, "failed to list runs")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"runs": runs})
}

func (s *Server) getRun(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	run, err := s.deps.Q.GetRun(ctx, id)
	if err != nil {
		apiNotFound(ctx, c, "run not found")
		return
	}
	c.JSON(consts.StatusOK, run)
}

func (s *Server) triggerRun(ctx context.Context, c *app.RequestContext) {
	var req struct {
		ProjectID    string `json:"projectId"`
		Branch       string `json:"branch"`
		WorkflowFile string `json:"workflowFile"`
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

	err = s.deps.Q.InsertManualRun(ctx, db.InsertManualRunParams{
		ID: runID, ProjectID: req.ProjectID, OrgID: info.OrgID,
		WorkflowFile: req.WorkflowFile, TriggerRef: &req.Branch,
	})
	if err != nil {
		apiInternal(ctx, c, "failed to create run")
		return
	}

	var workflowID string
	if s.deps.Engine != nil {
		workflowID, _ = s.deps.Engine.StartWorkflow(ctx, engine.StartWorkflowInput{
			RunID: runID, OrgID: info.OrgID, ProjectID: req.ProjectID,
			Repo: info.RepoPath, Ref: req.Branch,
			TriggerType: "manual", TriggeredBy: "api",
			WorkflowFile: req.WorkflowFile, PipelinePath: pipelinePath,
		})
	}

	c.JSON(consts.StatusAccepted, utils.H{
		"runID": runID, "workflowID": workflowID, "status": "pending",
	})
}

// ── Secrets (sqlc) ──────────────────────────────────────────

func (s *Server) listSecrets(ctx context.Context, c *app.RequestContext) {
	projectID := c.Param("id")
	secrets, err := s.deps.Q.ListProjectSecrets(ctx, &projectID)
	_ = secrets
	if err != nil {
		apiInternal(ctx, c, "failed to list secrets")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"secrets": secrets})
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
	c.JSON(consts.StatusOK, utils.H{"status": "deleted"})
}

// ── Runners (sqlc) ──────────────────────────────────────────

func (s *Server) listRunners(ctx context.Context, c *app.RequestContext) {
	runners, err := s.deps.Q.ListRunnerPools(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to list runners")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"runners": runners})
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

// ensure imports are used
var (
	_ = auth.RoleViewer
	_ = observe.RequestID
)
