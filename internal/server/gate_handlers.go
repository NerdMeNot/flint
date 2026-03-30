package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func (s *Server) handleListGates(ctx context.Context, c *app.RequestContext) {
	gates, err := s.deps.Q.ListPendingGates(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to list gates")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"data": gates})
}
