package observe

import (
	"context"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type ctxKey string

const (
	keyRequestID ctxKey = "request_id"
	keyOrgID     ctxKey = "org_id"
	keyRunID     ctxKey = "run_id"
	keyUserID    ctxKey = "user_id"
	keyStepName  ctxKey = "step_name"
)

// WithRequestID attaches a request ID to the context. Generates one if empty.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	if requestID == "" {
		requestID = uuid.NewString()
	}
	return context.WithValue(ctx, keyRequestID, requestID)
}

// WithOrgID attaches an org ID to the context.
func WithOrgID(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, keyOrgID, orgID)
}

// WithRunID attaches a pipeline run ID to the context.
func WithRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(ctx, keyRunID, runID)
}

// WithUserID attaches a user ID to the context.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, keyUserID, userID)
}

// WithStepName attaches a step name to the context.
func WithStepName(ctx context.Context, stepName string) context.Context {
	return context.WithValue(ctx, keyStepName, stepName)
}

// RequestID extracts the request ID from the context.
func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(keyRequestID).(string)
	return v
}

// OrgID extracts the org ID from the context.
func OrgID(ctx context.Context) string {
	v, _ := ctx.Value(keyOrgID).(string)
	return v
}

// RunID extracts the run ID from the context.
func RunID(ctx context.Context) string {
	v, _ := ctx.Value(keyRunID).(string)
	return v
}

// Logger returns a zerolog.Logger enriched with all correlation IDs from the context.
// Zero-allocation — zerolog builds the logger without allocating.
func Logger(ctx context.Context) zerolog.Logger {
	l := log.Logger.With()

	if v := RequestID(ctx); v != "" {
		l = l.Str("request_id", v)
	}
	if v := OrgID(ctx); v != "" {
		l = l.Str("org_id", v)
	}
	if v := RunID(ctx); v != "" {
		l = l.Str("run_id", v)
	}
	if v, ok := ctx.Value(keyUserID).(string); ok && v != "" {
		l = l.Str("user_id", v)
	}
	if v, ok := ctx.Value(keyStepName).(string); ok && v != "" {
		l = l.Str("step_name", v)
	}

	return l.Logger()
}
