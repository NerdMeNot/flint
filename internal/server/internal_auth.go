package server

import (
	"context"
	"crypto/subtle"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

const internalTokenHeader = "X-Flint-Internal-Token"

// internalAuthMiddleware validates the shared secret on /internal endpoints.
// When InternalToken is empty (dev/test), the middleware is a no-op to avoid
// breaking local development. In production, set server.internalToken (or
// FLINT_SERVER_INTERNALTOKEN) to a strong random value and inject it into
// agent pods as FLINT_INTERNAL_TOKEN.
func (s *Server) internalAuthMiddleware() app.HandlerFunc {
	token := s.deps.Config.Server.InternalToken

	return func(ctx context.Context, c *app.RequestContext) {
		// No token configured — skip auth (dev mode).
		if token == "" {
			c.Next(ctx)
			return
		}

		provided := string(c.GetHeader(internalTokenHeader))
		if provided == "" {
			c.AbortWithStatusJSON(consts.StatusUnauthorized, utils.H{
				"error": "missing " + internalTokenHeader + " header",
			})
			return
		}

		// Constant-time comparison to prevent timing attacks.
		if subtle.ConstantTimeCompare([]byte(token), []byte(provided)) != 1 {
			c.AbortWithStatusJSON(consts.StatusForbidden, utils.H{
				"error": "invalid internal token",
			})
			return
		}

		c.Next(ctx)
	}
}
