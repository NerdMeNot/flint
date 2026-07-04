package server

import (
	"context"

	"github.com/NerdMeNot/flint/schemas"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// handlePipelineSchema serves the pipeline JSON Schema for editor tooling
// (yaml-language-server autocomplete/diagnostics). Public and unauthenticated
// — it describes the language, not any user data.
// GET /schemas/pipeline.json
func (s *Server) handlePipelineSchema(_ context.Context, c *app.RequestContext) {
	c.Response.Header.SetContentType("application/schema+json")
	c.Response.Header.Set("Cache-Control", "public, max-age=3600")
	c.SetStatusCode(consts.StatusOK)
	c.Response.SetBody(schemas.PipelineSchema)
}
