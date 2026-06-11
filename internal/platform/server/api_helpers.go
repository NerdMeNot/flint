package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/NerdMeNot/flint/internal/core/httpx"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// ── Error Response ──────────────────────────────────────────

// apiError sends the consistent error envelope. It delegates to the shared
// httpx helper so the server and product packages produce identical responses.
func apiError(ctx context.Context, c *app.RequestContext, status int, code, message string) {
	httpx.Error(ctx, c, status, code, message)
}

func apiBadRequest(ctx context.Context, c *app.RequestContext, msg string) {
	apiError(ctx, c, consts.StatusBadRequest, "INVALID_INPUT", msg)
}

func apiNotFound(ctx context.Context, c *app.RequestContext, msg string) {
	apiError(ctx, c, consts.StatusNotFound, "NOT_FOUND", msg)
}

func apiUnauthorized(ctx context.Context, c *app.RequestContext, msg string) {
	apiError(ctx, c, consts.StatusUnauthorized, "UNAUTHORIZED", msg)
}

func apiForbidden(ctx context.Context, c *app.RequestContext, msg string) {
	apiError(ctx, c, consts.StatusForbidden, "FORBIDDEN", msg)
}

func apiConflict(ctx context.Context, c *app.RequestContext, msg string) {
	apiError(ctx, c, consts.StatusConflict, "CONFLICT", msg)
}

func apiInternal(ctx context.Context, c *app.RequestContext, msg string) {
	apiError(ctx, c, consts.StatusInternalServerError, "INTERNAL", msg)
}

// ── Pagination ──────────────────────────────────────────────

// PaginationParams holds parsed pagination query params.
type PaginationParams struct {
	Cursor    string
	Limit     int
	Direction string // "next" or "prev"
}

// parsePagination extracts pagination params from query string.
func parsePagination(c *app.RequestContext) PaginationParams {
	p := PaginationParams{
		Cursor:    string(c.Query("cursor")),
		Direction: string(c.Query("direction")),
	}

	if limit := string(c.Query("limit")); limit != "" {
		if n, err := strconv.Atoi(limit); err == nil && n > 0 && n <= 100 {
			p.Limit = n
		}
	}
	if p.Limit == 0 {
		p.Limit = 25
	}
	if p.Direction == "" {
		p.Direction = "next"
	}

	return p
}

// PaginationResponse is the pagination metadata in list responses.
type PaginationResponse struct {
	HasMore    bool   `json:"hasMore"`
	NextCursor string `json:"nextCursor,omitempty"`
	PrevCursor string `json:"prevCursor,omitempty"`
	Total      *int   `json:"total,omitempty"`
}

// encodeCursor creates an opaque cursor from an ID and sort value.
func encodeCursor(id, sortValue string) string {
	data, _ := json.Marshal(map[string]string{"id": id, "ts": sortValue})
	return base64.StdEncoding.EncodeToString(data)
}

// decodeCursor extracts ID and sort value from an opaque cursor.
func decodeCursor(cursor string) (id, sortValue string, err error) {
	data, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return "", "", fmt.Errorf("invalid cursor")
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return "", "", fmt.Errorf("invalid cursor payload")
	}
	return m["id"], m["ts"], nil
}

// listOffset parses the opaque pagination cursor into a row offset. Admin list
// endpoints use limit+offset pagination (keyset is reserved for the high-volume
// runs/audit feeds); the cursor stays opaque to callers either way.
func listOffset(c *app.RequestContext) int {
	cur := string(c.Query("cursor"))
	if cur == "" {
		return 0
	}
	if id, sv, err := decodeCursor(cur); err == nil && id == "offset" {
		if n, e := strconv.Atoi(sv); e == nil && n >= 0 {
			return n
		}
	}
	return 0
}

// nextOffsetCursor returns the cursor for the next page, or "" when this was the
// last page (fewer rows returned than the limit).
func nextOffsetCursor(offset, limit, returned int) string {
	if returned < limit {
		return ""
	}
	return encodeCursor("offset", strconv.Itoa(offset+limit))
}

// paginatedResponse sends a paginated list response using the standard envelope.
func paginatedResponse(c *app.RequestContext, data any, pagination PaginationResponse) {
	resp := utils.H{"items": data}
	if pagination.NextCursor != "" {
		resp["nextCursor"] = pagination.NextCursor
	}
	c.JSON(consts.StatusOK, resp)
}

// ── Query Helpers ───────────────────────────────────────────

// queryString safely extracts a string query param.
func queryString(c *app.RequestContext, key string) string {
	return string(c.Query(key))
}
