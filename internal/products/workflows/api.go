package workflows

import (
	"context"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/engine"
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
func (a *API) Register(rg *route.RouterGroup) {
	rg.POST("/workflows/runs", a.triggerRun)
	rg.GET("/workflows/runs/:id", a.getRun)
}

// triggerRun parses a workflow definition (YAML body), resolves its DAG, creates
// a run, and starts it on the engine.
func (a *API) triggerRun(ctx context.Context, c *app.RequestContext) {
	orgID := observe.OrgID(ctx)
	if orgID == "" {
		c.JSON(consts.StatusUnauthorized, utils.H{"error": "no organization in context"})
		return
	}

	def, err := Parse(c.Request.Body())
	if err != nil {
		c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	waves, err := def.Resolve()
	if err != nil {
		c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
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
		c.JSON(consts.StatusInternalServerError, utils.H{"error": "failed to create run"})
		return
	}

	wfID, err := a.engine.StartWorkflowWithWaves(ctx, engine.StartWorkflowInput{
		RunID: runID,
		OrgID: orgID,
		Kind:  "workflow",
	}, waves)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, utils.H{"error": err.Error()})
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
		c.JSON(consts.StatusNotFound, utils.H{"error": "run not found"})
		return
	}
	state, err := a.engine.QueryWorkflow(ctx, *wfID)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, utils.H{"error": "failed to query workflow"})
		return
	}
	c.JSON(consts.StatusOK, utils.H{
		"runId":      state.RunID,
		"workflowId": state.WorkflowID,
		"status":     state.Status,
		"steps":      state.Steps,
	})
}
