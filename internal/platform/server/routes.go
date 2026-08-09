package server

import (
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/internal/platform/auth"
)

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
	v1.GET("/runners/:name/insights", s.requirePermission(auth.ObjRunner, auth.ActRead), s.handleGetPoolInsights)

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

// ensure imports are used
var (
	_ = auth.RoleViewer
	_ = observe.RequestID
)
