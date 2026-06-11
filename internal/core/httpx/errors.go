// Package httpx holds small HTTP helpers shared by the platform server and the
// product packages. Products cannot import the server package, so the canonical
// error envelope lives here and both sides delegate to it.
package httpx

import (
	"context"

	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// Error sends the standard error envelope:
//
//	{"error": {"code": "...", "message": "...", "requestId": "..."}}
func Error(ctx context.Context, c *app.RequestContext, status int, code, message string) {
	c.JSON(status, utils.H{
		"error": utils.H{
			"code":      code,
			"message":   message,
			"requestId": observe.RequestID(ctx),
		},
	})
}

// BadRequest sends a 400 with code INVALID_INPUT.
func BadRequest(ctx context.Context, c *app.RequestContext, msg string) {
	Error(ctx, c, consts.StatusBadRequest, "INVALID_INPUT", msg)
}

// NotFound sends a 404 with code NOT_FOUND.
func NotFound(ctx context.Context, c *app.RequestContext, msg string) {
	Error(ctx, c, consts.StatusNotFound, "NOT_FOUND", msg)
}

// Unauthorized sends a 401 with code UNAUTHORIZED.
func Unauthorized(ctx context.Context, c *app.RequestContext, msg string) {
	Error(ctx, c, consts.StatusUnauthorized, "UNAUTHORIZED", msg)
}

// Forbidden sends a 403 with code FORBIDDEN.
func Forbidden(ctx context.Context, c *app.RequestContext, msg string) {
	Error(ctx, c, consts.StatusForbidden, "FORBIDDEN", msg)
}

// Conflict sends a 409 with code CONFLICT.
func Conflict(ctx context.Context, c *app.RequestContext, msg string) {
	Error(ctx, c, consts.StatusConflict, "CONFLICT", msg)
}

// Internal sends a 500 with code INTERNAL.
func Internal(ctx context.Context, c *app.RequestContext, msg string) {
	Error(ctx, c, consts.StatusInternalServerError, "INTERNAL", msg)
}
