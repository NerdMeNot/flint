package workflows

import (
	"context"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/internal/testutil/mocks"
	"github.com/NerdMeNot/flint/pkg/pipeline"
)

func TestTriggerRun_StartsWorkflow(t *testing.T) {
	eng := new(mocks.Engine)
	q := new(mocks.Querier)

	q.On("InsertWorkflowRun", mock.Anything, mock.MatchedBy(func(p db.InsertWorkflowRunParams) bool {
		return p.OrgID == "org-1" && p.TriggerType == "manual"
	})).Return(nil)
	eng.On("StartWorkflowWithWaves", mock.Anything,
		mock.MatchedBy(func(in engine.StartWorkflowInput) bool {
			return in.OrgID == "org-1" && in.Kind == "workflow"
		}),
		mock.MatchedBy(func(waves [][]pipeline.Step) bool {
			// a in wave 0, b in wave 1
			return len(waves) == 2 && waves[0][0].Name == "a" && waves[1][0].Name == "b"
		}),
	).Return("wf-1", nil)

	api := NewAPI(eng, q)
	c := app.NewContext(0)
	c.Request.SetBody([]byte("name: demo\nsteps:\n  - name: a\n    run: echo a\n  - name: b\n    run: echo b\n    dependsOn: [a]\n"))

	api.triggerRun(observe.WithOrgID(context.Background(), "org-1"), c)

	assert.Equal(t, 202, c.Response.StatusCode())
	eng.AssertExpectations(t)
	q.AssertExpectations(t)
}

func TestTriggerRun_RejectsBadYAML(t *testing.T) {
	api := NewAPI(new(mocks.Engine), new(mocks.Querier))
	c := app.NewContext(0)
	c.Request.SetBody([]byte("name: x\nsteps: []\n")) // no steps → invalid

	api.triggerRun(observe.WithOrgID(context.Background(), "org-1"), c)

	assert.Equal(t, 400, c.Response.StatusCode())
}

func TestTriggerRun_RequiresOrg(t *testing.T) {
	api := NewAPI(new(mocks.Engine), new(mocks.Querier))
	c := app.NewContext(0)
	c.Request.SetBody([]byte("name: x\nsteps:\n  - name: a\n    run: echo a\n"))

	api.triggerRun(context.Background(), c) // no org in context
	assert.Equal(t, 401, c.Response.StatusCode())
}
