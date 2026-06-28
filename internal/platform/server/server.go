package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/core/observe"
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
		s.registerSCIMRoutes()
	}
}

// registerSCIMRoutes mounts the SCIM 2.0 provisioning data-plane. It uses its
// own bearer-token middleware (not JWT) and SCIM-formatted errors.
func (s *Server) registerSCIMRoutes() {
	// Per-IP rate limit, generous enough for an IdP's initial bulk provisioning
	// sync but a cap against abuse.
	scim := s.hertz.Group("/scim/v2", s.ipRateLimit(newIPRateLimiter(50, 100)), s.scimAuthMiddleware())

	scim.GET("/ServiceProviderConfig", s.handleSCIMServiceProviderConfig)

	scim.GET("/Users", s.handleSCIMListUsers)
	scim.POST("/Users", s.handleSCIMCreateUser)
	scim.GET("/Users/:id", s.handleSCIMGetUser)
	scim.PUT("/Users/:id", s.handleSCIMReplaceUser)
	scim.PATCH("/Users/:id", s.handleSCIMPatchUser)
	scim.DELETE("/Users/:id", s.handleSCIMDeleteUser)

	scim.GET("/Groups", s.handleSCIMListGroups)
	scim.POST("/Groups", s.handleSCIMCreateGroup)
	scim.GET("/Groups/:id", s.handleSCIMGetGroup)
	scim.PUT("/Groups/:id", s.handleSCIMReplaceGroup)
	scim.PATCH("/Groups/:id", s.handleSCIMPatchGroup)
	scim.DELETE("/Groups/:id", s.handleSCIMDeleteGroup)
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
	if s.deps.Runs == nil {
		apiError(ctx, c, consts.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "run creation unavailable")
		return
	}
	body, _ := c.Body()
	headers := hertzToHTTPHeaders(c)
	runIDs, err := s.deps.Runs.HandleWebhook(ctx, headers, body, forgeTypeFromPath(string(c.Path())))
	if err != nil {
		apiBadRequest(ctx, c, err.Error())
		return
	}
	c.JSON(consts.StatusAccepted, utils.H{"status": "accepted", "runIDs": runIDs})
}

// forgeTypeFromPath derives the forge type from the webhook URL suffix.
func forgeTypeFromPath(urlPath string) string {
	switch {
	case len(urlPath) > 10 && urlPath[len(urlPath)-6:] == "github":
		return "github"
	case len(urlPath) > 10 && urlPath[len(urlPath)-6:] == "gitlab":
		return "gitlab"
	default:
		return "bitbucket"
	}
}

// handleAgentComplete receives step completion from the agent.
// Replaces the agent dialing Temporal directly.
func (s *Server) handleAgentComplete(ctx context.Context, c *app.RequestContext) {
	var req struct {
		TaskToken string            `json:"taskToken"`
		Result    engine.StepResult `json:"result"`
	}
	if err := c.BindJSON(&req); err != nil {
		apiBadRequest(ctx, c, "invalid request body")
		return
	}
	if req.TaskToken == "" {
		apiBadRequest(ctx, c, "taskToken is required")
		return
	}

	if err := s.deps.Engine.CompleteStep(ctx, req.TaskToken, req.Result); err != nil {
		apiInternal(ctx, c, err.Error())
		return
	}

	c.JSON(consts.StatusOK, utils.H{"status": "ok"})
}

// handleMetrics serves Prometheus-format metrics via the OTel exporter.
func (s *Server) handleMetrics(_ context.Context, c *app.RequestContext) {
	// The OTel Prometheus exporter registers with the default Prometheus
	// gatherer, so promhttp.Handler() picks up all OTel metrics.
	c.Response.Header.SetContentType("text/plain; version=0.0.4; charset=utf-8")
	// Use the prometheus client_golang handler via an adapter. promhttp reads
	// req.Header (content negotiation), so pass a real request with the incoming
	// headers — never nil, which would nil-deref inside expfmt.Negotiate.
	promHandler := promhttp.Handler()
	httpReq, _ := http.NewRequest(http.MethodGet, "/metrics", nil)
	httpReq.Header = hertzToHTTPHeaders(c)
	writer := &hertzResponseWriter{ctx: c}
	promHandler.ServeHTTP(writer, httpReq)
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
