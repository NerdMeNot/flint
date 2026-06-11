package workflows

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/core/httpx"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/google/uuid"
)

// API is the Flint Workflows HTTP surface. It turns a posted workflow definition
// into a run on the shared engine — no forge, no pipeline YAML, no CI triggers.
// It depends only on core (engine, db, observe) and is wired into the platform
// server's authenticated group by the composition root.
type API struct {
	engine engine.Engine
	q      db.Querier
}

// NewAPI builds the Workflows API.
func NewAPI(eng engine.Engine, q db.Querier) *API {
	return &API{engine: eng, q: q}
}

// Register mounts the workflow routes under the given authenticated /api/v1 group.
//
// The group already applies the platform auth middleware, so every route below
// requires a valid session and is scoped to the caller's org. Workflows are an
// org-level product without per-resource RBAC objects (unlike CI's
// project/run/gate), so authorization here is org-level by design; a dedicated
// ObjWorkflow + scope model can be layered in later without changing this surface.
func (a *API) Register(rg *route.RouterGroup) {
	rg.POST("/workflows/runs", a.triggerRun)
	rg.GET("/workflows/runs", a.listRuns)
	rg.GET("/workflows/runs/:id", a.getRun)
	rg.POST("/workflows/schedules", a.createSchedule)
	rg.GET("/workflows/schedules", a.listSchedules)
}

// createSchedule stores a cron-scheduled workflow. The worker's scheduler fires
// it; this validates the cron expression and the workflow definition up front.
func (a *API) createSchedule(ctx context.Context, c *app.RequestContext) {
	orgID := observe.OrgID(ctx)
	if orgID == "" {
		httpx.Unauthorized(ctx, c, "no organization in context")
		return
	}
	var req struct {
		Name       string `json:"name"`
		Cron       string `json:"cron"`
		Definition string `json:"definition"`
	}
	if c.BindJSON(&req) != nil || req.Name == "" || req.Cron == "" || req.Definition == "" {
		httpx.BadRequest(ctx, c, "name, cron, and definition are required")
		return
	}
	next, err := NextCron(req.Cron, time.Now())
	if err != nil {
		httpx.BadRequest(ctx, c, "invalid cron: "+err.Error())
		return
	}
	if _, err := Parse([]byte(req.Definition)); err != nil {
		httpx.BadRequest(ctx, c, err.Error())
		return
	}

	id, err := a.q.CreateWorkflowSchedule(ctx, db.CreateWorkflowScheduleParams{
		OrgID:      orgID,
		Name:       req.Name,
		Cron:       req.Cron,
		Definition: req.Definition,
		NextRunAt:  next,
	})
	if err != nil {
		httpx.Internal(ctx, c, "failed to create schedule")
		return
	}
	c.JSON(consts.StatusCreated, utils.H{"id": id, "name": req.Name, "cron": req.Cron, "nextRunAt": next})
}

// listSchedules returns the org's workflow schedules.
func (a *API) listSchedules(ctx context.Context, c *app.RequestContext) {
	orgID := observe.OrgID(ctx)
	if orgID == "" {
		httpx.Unauthorized(ctx, c, "no organization in context")
		return
	}
	schedules, err := a.q.ListWorkflowSchedules(ctx, orgID)
	if err != nil {
		httpx.Internal(ctx, c, "failed to list schedules")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"items": schedules})
}

// triggerRun parses a workflow definition (YAML body), resolves its DAG, creates
// a run, and starts it on the engine.
func (a *API) triggerRun(ctx context.Context, c *app.RequestContext) {
	orgID := observe.OrgID(ctx)
	if orgID == "" {
		httpx.Unauthorized(ctx, c, "no organization in context")
		return
	}

	def, err := Parse(extractDefinition(c.Request.Body()))
	if err != nil {
		httpx.BadRequest(ctx, c, err.Error())
		return
	}
	waves, err := def.Resolve()
	if err != nil {
		httpx.BadRequest(ctx, c, err.Error())
		return
	}

	runID := uuid.NewString()
	triggeredBy := "api"
	if err := a.q.InsertWorkflowRun(ctx, db.InsertWorkflowRunParams{
		ID:          runID,
		OrgID:       orgID,
		TriggerType: "manual",
		TriggeredBy: &triggeredBy,
	}); err != nil {
		httpx.Internal(ctx, c, "failed to create run")
		return
	}

	wfID, err := a.engine.StartWorkflowWithWaves(ctx, engine.StartWorkflowInput{
		RunID: runID,
		OrgID: orgID,
		Kind:  "workflow",
	}, waves)
	if err != nil {
		httpx.Internal(ctx, c, err.Error())
		return
	}

	c.JSON(consts.StatusAccepted, utils.H{
		"runId":      runID,
		"workflowId": wfID,
		"name":       def.Name,
		"status":     "pending",
	})
}

// getRun returns the current state of a workflow run.
func (a *API) getRun(ctx context.Context, c *app.RequestContext) {
	runID := c.Param("id")
	wfID, err := a.q.GetRunWorkflowID(ctx, runID)
	if err != nil || wfID == nil {
		httpx.NotFound(ctx, c, "run not found")
		return
	}
	state, err := a.engine.QueryWorkflow(ctx, *wfID)
	if err != nil {
		httpx.Internal(ctx, c, "failed to query workflow")
		return
	}
	c.JSON(consts.StatusOK, utils.H{
		"runId":      state.RunID,
		"workflowId": state.WorkflowID,
		"status":     state.Status,
		"steps":      state.Steps,
	})
}

// listRuns returns the org's workflow runs, newest first, with keyset pagination
// ({items, nextCursor}). The cursor is opaque; pass it back as ?cursor= to page.
func (a *API) listRuns(ctx context.Context, c *app.RequestContext) {
	orgID := observe.OrgID(ctx)
	if orgID == "" {
		httpx.Unauthorized(ctx, c, "no organization in context")
		return
	}

	limit := 25
	if l := string(c.Query("limit")); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	cursorTs, cursorID := decodeRunCursor(string(c.Query("cursor")))

	rows, err := a.q.ListWorkflowRuns(ctx, db.ListWorkflowRunsParams{
		OrgID:    orgID,
		CursorTs: cursorTs,
		CursorID: cursorID,
		Lim:      int32(limit),
	})
	if err != nil {
		httpx.Internal(ctx, c, "failed to list runs")
		return
	}

	items := make([]utils.H, 0, len(rows))
	for _, r := range rows {
		item := utils.H{
			"runId":       r.ID,
			"status":      r.Status,
			"triggerType": r.TriggerType,
			"startedAt":   r.StartedAt.Format(time.RFC3339),
		}
		if r.TriggeredBy != nil {
			item["triggeredBy"] = *r.TriggeredBy
		}
		if r.FinishedAt != nil {
			item["finishedAt"] = r.FinishedAt.Format(time.RFC3339)
		}
		if r.DurationMs.Valid {
			item["durationMs"] = r.DurationMs.Int32
		}
		if r.ErrorMessage != nil {
			item["error"] = *r.ErrorMessage
		}
		items = append(items, item)
	}

	resp := utils.H{"items": items}
	if len(rows) == limit && limit > 0 {
		last := rows[len(rows)-1]
		resp["nextCursor"] = encodeRunCursor(last.StartedAt.Format(time.RFC3339Nano), last.ID)
	}
	c.JSON(consts.StatusOK, resp)
}

// extractDefinition accepts either a raw YAML body or a JSON envelope
// {"definition": "<yaml>"} (the web client's shape) and returns the YAML bytes.
func extractDefinition(body []byte) []byte {
	var env struct {
		Definition string `json:"definition"`
	}
	if err := json.Unmarshal(body, &env); err == nil && env.Definition != "" {
		return []byte(env.Definition)
	}
	return body
}

// encodeRunCursor / decodeRunCursor encode the (started_at, id) keyset cursor as
// an opaque base64 token.
func encodeRunCursor(ts, id string) string {
	b, _ := json.Marshal([2]string{ts, id})
	return base64.StdEncoding.EncodeToString(b)
}

func decodeRunCursor(cur string) (ts, id string) {
	if cur == "" {
		return "", ""
	}
	b, err := base64.StdEncoding.DecodeString(cur)
	if err != nil {
		return "", ""
	}
	var v [2]string
	if json.Unmarshal(b, &v) != nil {
		return "", ""
	}
	return v[0], v[1]
}
