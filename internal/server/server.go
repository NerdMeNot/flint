package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/NerdMeNot/flint/internal/db"
	"github.com/NerdMeNot/flint/internal/engine"
	"github.com/NerdMeNot/flint/internal/observe"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// Server wraps the Hertz HTTP server with Flint's routes and middleware.
type Server struct {
	hertz *server.Hertz
	deps  Deps
}

// New creates a new Flint HTTP server.
func New(deps Deps) *Server {
	addr := fmt.Sprintf(":%d", deps.Config.Server.PortOrDefault())

	h := server.Default(
		server.WithHostPorts(addr),
		server.WithMaxRequestBodySize(2<<20),
		server.WithExitWaitTime(10*time.Second),
	)

	s := &Server{hertz: h, deps: deps}

	h.Use(s.corsMiddleware())
	h.Use(s.requestIDMiddleware())

	s.registerRoutes()

	return s
}

// Run starts the HTTP server.
func (s *Server) Run() {
	s.hertz.Spin()
}

func (s *Server) corsMiddleware() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Flint-Org-ID")
		c.Header("Access-Control-Max-Age", "86400")

		if string(c.Method()) == "OPTIONS" {
			c.AbortWithStatus(consts.StatusNoContent)
			return
		}
		c.Next(ctx)
	}
}

func (s *Server) registerRoutes() {
	mode := s.deps.Mode
	if mode == "" {
		mode = "all"
	}

	// Health — always registered.
	s.hertz.GET("/health/live", s.handleLive)
	s.hertz.GET("/health/ready", s.handleReady)

	// Auth routes — no JWT required (used to obtain JWT).
	s.registerAuthRoutes()

	// Webhooks — internet-facing, forge signature auth.
	if mode == "all" || mode == "webhook" {
		s.hertz.POST("/webhooks/github", s.handleWebhook)
		s.hertz.POST("/webhooks/gitlab", s.handleWebhook)
		s.hertz.POST("/webhooks/bitbucket", s.handleWebhook)
	}

	// Internal agent endpoints + API — internal/user-facing.
	if mode == "all" || mode == "api" {
		internal := s.hertz.Group("/internal")
		internal.GET("/secrets", s.handleAgentSecrets)
		internal.GET("/clone-token", s.handleAgentCloneToken)
		internal.POST("/complete", s.handleAgentComplete)
		internal.POST("/logs", s.handleAgentLogIngestion)

		s.registerAPIRoutes()
	}
}

func (s *Server) handleLive(_ context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, utils.H{"status": "alive"})
}

func (s *Server) handleReady(ctx context.Context, c *app.RequestContext) {
	checks := make(map[string]string)
	healthy := true

	if s.deps.DB != nil {
		if err := s.deps.DB.Ping(ctx); err != nil {
			checks["database"] = fmt.Sprintf("unhealthy: %v", err)
			healthy = false
		} else {
			checks["database"] = "healthy"
		}
	}

	if s.deps.Engine != nil {
		checks["engine"] = "ready"
	}

	status := consts.StatusOK
	if !healthy {
		status = consts.StatusServiceUnavailable
	}

	c.JSON(status, utils.H{
		"status": boolToStatus(healthy),
		"checks": checks,
	})
}

// handleWebhook processes incoming forge webhook events.
// Much simpler now: create pipeline_run → engine.StartWorkflow (one call).
func (s *Server) handleWebhook(ctx context.Context, c *app.RequestContext) {
	ctx = observe.WithRequestID(ctx, "")
	logger := observe.Logger(ctx)

	body, _ := c.Body()
	headers := hertzToHTTPHeaders(c)

	// Determine forge type from URL path.
	urlPath := string(c.Path())
	var forgeType string
	switch {
	case len(urlPath) > 10 && urlPath[len(urlPath)-6:] == "github":
		forgeType = "github"
	case len(urlPath) > 10 && urlPath[len(urlPath)-6:] == "gitlab":
		forgeType = "gitlab"
	default:
		forgeType = "bitbucket"
	}

	// Look up webhook secret.
	webhookSecret, _ := s.deps.Q.GetWebhookSecret(ctx, forgeType)
	if webhookSecret == "" {
		c.JSON(consts.StatusNotFound, utils.H{"error": "no forge connection configured"})
		return
	}

	event, err := s.deps.Forge.ParseWebhook(headers, body, webhookSecret)
	if err != nil {
		observe.WebhooksInvalid.Add(ctx, 1)
		c.JSON(consts.StatusBadRequest, utils.H{"error": "invalid webhook"})
		return
	}

	observe.WebhooksReceived.Add(ctx, 1)
	logger.Info().
		Str("kind", string(event.Kind)).
		Str("repo", event.Repo).
		Str("sha", event.CommitSHA).
		Msg("webhook received")

	// Look up project.
	proj, err := s.deps.Q.GetProjectByRepoPath(ctx, event.Repo)
	if err != nil {
		c.JSON(consts.StatusNotFound, utils.H{"error": "no project for this repo"})
		return
	}
	projectID := proj.ID
	orgID := proj.OrgID
	pipelinePath := ".flint/"
	if pp, ok := proj.PipelinePath.(string); ok && pp != "" {
		pipelinePath = pp
	}

	// Discover workflow files.
	workflowFiles := []string{"ci.yaml"}
	if s.deps.Forge != nil {
		dir, dirErr := s.deps.Forge.GetDirectory(ctx, event.Repo, event.CommitSHA, pipelinePath)
		if dirErr == nil && len(dir) > 0 {
			workflowFiles = workflowFiles[:0]
			for name := range dir {
				if len(name) > 4 && (name[len(name)-5:] == ".yaml" || name[len(name)-4:] == ".yml") {
					workflowFiles = append(workflowFiles, name)
				}
			}
			if len(workflowFiles) == 0 {
				workflowFiles = []string{"ci.yaml"}
			}
		}
	}

	baseRunID := observe.RequestID(ctx)
	var runIDs []string

	for _, workflowFile := range workflowFiles {
		runID := baseRunID
		if len(workflowFiles) > 1 {
			runID = fmt.Sprintf("%s-%s", baseRunID, workflowFile[:len(workflowFile)-5])
		}

		// Create pipeline_run record.
		err = s.deps.Q.InsertPipelineRun(ctx, db.InsertPipelineRunParams{
			ID:            runID,
			ProjectID:     projectID,
			OrgID:         orgID,
			WorkflowFile:  workflowFile,
			TriggerType:   string(event.Kind),
			TriggerRef:    &event.Branch,
			CommitSha:     &event.CommitSHA,
			CommitMessage: &event.Message,
			TriggeredBy:   &event.Sender,
		})
		if err != nil {
			logger.Error().Err(err).Str("workflow", workflowFile).Msg("failed to insert pipeline run")
			continue
		}

		// Start workflow — single call, handles everything.
		if s.deps.Engine != nil {
			_, startErr := s.deps.Engine.StartWorkflow(ctx, engine.StartWorkflowInput{
				RunID:        runID,
				OrgID:        orgID,
				ProjectID:    projectID,
				Repo:         event.Repo,
				Ref:          event.Branch,
				CommitSHA:    event.CommitSHA,
				TriggerType:  string(event.Kind),
				TriggeredBy:  event.Sender,
				WorkflowFile: workflowFile,
				PipelinePath: pipelinePath,
			})
			if startErr != nil {
				logger.Error().Err(startErr).Str("workflow", workflowFile).Msg("engine.StartWorkflow failed")
				// Update pipeline_run to failed.
				_ = s.deps.Q.UpdateRunStatus(ctx, db.UpdateRunStatusParams{
					ID: runID, Status: "failed",
				})
			}
		}

		runIDs = append(runIDs, runID)
		logger.Info().Str("runID", runID).Str("workflow", workflowFile).Msg("pipeline run created")
	}

	c.JSON(consts.StatusAccepted, utils.H{
		"status":    "accepted",
		"runIDs":    runIDs,
		"workflows": workflowFiles,
	})
}

// handleAgentComplete receives step completion from the agent.
// Replaces the agent dialing Temporal directly.
func (s *Server) handleAgentComplete(ctx context.Context, c *app.RequestContext) {
	var req struct {
		TaskToken string           `json:"taskToken"`
		Result    engine.StepResult `json:"result"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusBadRequest, utils.H{"error": "invalid request body"})
		return
	}
	if req.TaskToken == "" {
		c.JSON(consts.StatusBadRequest, utils.H{"error": "taskToken is required"})
		return
	}

	if err := s.deps.Engine.CompleteStep(ctx, req.TaskToken, req.Result); err != nil {
		c.JSON(consts.StatusInternalServerError, utils.H{"error": err.Error()})
		return
	}

	c.JSON(consts.StatusOK, utils.H{"status": "ok"})
}

func boolToStatus(ok bool) string {
	if ok {
		return "healthy"
	}
	return "unhealthy"
}

func hertzToHTTPHeaders(c *app.RequestContext) http.Header {
	h := make(http.Header)
	c.Request.Header.VisitAll(func(key, value []byte) {
		h.Set(string(key), string(value))
	})
	return h
}
