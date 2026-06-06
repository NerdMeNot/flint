package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/NerdMeNot/flint/internal/db"
	"github.com/NerdMeNot/flint/internal/engine"
	"github.com/NerdMeNot/flint/internal/observe"
	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
		server.WithStreamBody(true), // required for SSE log streaming
	)

	s := &Server{hertz: h, deps: deps}

	h.Use(s.corsMiddleware())
	h.Use(s.requestIDMiddleware())

	s.registerRoutes()

	return s
}

// Engine returns the underlying Hertz route engine for unit testing.
// Use with ut.PerformRequest to test handlers without network.
func (s *Server) Engine() *route.Engine {
	return s.hertz.Engine
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

	// Health + metrics — always registered.
	s.hertz.GET("/health/live", s.handleLive)
	s.hertz.GET("/health/ready", s.handleReady)
	s.hertz.GET("/metrics", s.handleMetrics)

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
		internal := s.hertz.Group("/internal", s.internalAuthMiddleware())
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

	// Build trigger event for matching.
	triggerEvent := pipeline.TriggerEvent{
		Kind:       string(event.Kind),
		Branch:     event.Branch,
		BaseBranch: event.BaseBranch,
		Tag:        event.Tag,
	}

	baseRunID := observe.RequestID(ctx)
	var runIDs []string
	runCounter := 0

	for _, workflowFile := range workflowFiles {
		// Fetch and parse pipeline YAML to evaluate triggers.
		filePath := pipelinePath + workflowFile
		rawYAML, fetchErr := s.deps.Forge.GetFile(ctx, event.Repo, event.CommitSHA, filePath)
		if fetchErr != nil {
			logger.Warn().Err(fetchErr).Str("file", filePath).Msg("failed to fetch pipeline (skipping)")
			continue
		}

		p, parseErr := pipeline.Parse(rawYAML)
		if parseErr != nil {
			logger.Warn().Err(parseErr).Str("file", filePath).Msg("failed to parse pipeline (skipping)")
			continue
		}

		// Evaluate triggers — skip if no trigger matches this event.
		matches := pipeline.MatchTriggers(p, triggerEvent)
		if len(matches) == 0 {
			logger.Debug().Str("workflow", workflowFile).Msg("no matching trigger, skipping")
			continue
		}

		// Collect unique environments from matching triggers.
		environments := pipeline.CollectEnvironments(matches)

		// Create one run per environment.
		ref := event.Branch
		if event.Tag != "" {
			ref = event.Tag
		}

		for _, env := range environments {
			runCounter++
			runID := baseRunID
			if runCounter > 1 {
				runID = fmt.Sprintf("%s-%d", baseRunID, runCounter)
			}

			var envPtr *string
			if env != "" {
				envPtr = &env
			}

			err = s.deps.Q.InsertPipelineRun(ctx, db.InsertPipelineRunParams{
				ID:            runID,
				ProjectID:     projectID,
				OrgID:         orgID,
				WorkflowFile:  workflowFile,
				TriggerType:   string(event.Kind),
				TriggerRef:    &ref,
				CommitSha:     &event.CommitSHA,
				CommitMessage: &event.Message,
				TriggeredBy:   &event.Sender,
				Environment:   envPtr,
			})
			if err != nil {
				logger.Error().Err(err).Str("workflow", workflowFile).Msg("failed to insert pipeline run")
				continue
			}

			if s.deps.Engine != nil {
				_, startErr := s.deps.Engine.StartWorkflow(ctx, engine.StartWorkflowInput{
					RunID:        runID,
					OrgID:        orgID,
					ProjectID:    projectID,
					Repo:         event.Repo,
					Ref:          ref,
					CommitSHA:    event.CommitSHA,
					TriggerType:  string(event.Kind),
					TriggeredBy:  event.Sender,
					WorkflowFile: workflowFile,
					PipelinePath: pipelinePath,
					Environment:  env,
				})
				if startErr != nil {
					logger.Error().Err(startErr).Str("workflow", workflowFile).Str("env", env).
						Msg("engine.StartWorkflow failed")
					errMsg := startErr.Error()
					_ = s.deps.Q.FailRunWithError(ctx, db.FailRunWithErrorParams{
						ID:           runID,
						ErrorMessage: &errMsg,
					})
				}
			}

			runIDs = append(runIDs, runID)
			logger.Info().Str("runID", runID).Str("workflow", workflowFile).Str("env", env).
				Msg("pipeline run created")
		}
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
		TaskToken string            `json:"taskToken"`
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

// handleMetrics serves Prometheus-format metrics via the OTel exporter.
func (s *Server) handleMetrics(_ context.Context, c *app.RequestContext) {
	// The OTel Prometheus exporter registers with the default Prometheus
	// gatherer, so promhttp.Handler() picks up all OTel metrics.
	c.Response.Header.SetContentType("text/plain; version=0.0.4; charset=utf-8")
	// Use the prometheus client_golang handler via an adapter.
	promHandler := promhttp.Handler()
	writer := &hertzResponseWriter{ctx: c}
	promHandler.ServeHTTP(writer, nil)
}

// hertzResponseWriter adapts Hertz's RequestContext to http.ResponseWriter
// for serving Prometheus metrics.
type hertzResponseWriter struct {
	ctx *app.RequestContext
}

func (w *hertzResponseWriter) Header() http.Header {
	return make(http.Header) // promhttp only sets Content-Type, which we set above
}

func (w *hertzResponseWriter) Write(b []byte) (int, error) {
	return w.ctx.Write(b)
}

func (w *hertzResponseWriter) WriteHeader(int) {} // not needed

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
