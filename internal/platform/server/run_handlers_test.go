package server

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/NerdMeNot/flint/internal/testutil"
)

// authHeaders returns headers for an authenticated API request.
// The mock sessions must be set up to validate "test-jwt-token".
func authHeaders() []ut.Header {
	return []ut.Header{
		{Key: "Authorization", Value: "Bearer test-jwt-token"},
		{Key: "Content-Type", Value: "application/json"},
	}
}

// testClaims returns standard test claims for an authenticated user.
func testClaims() *auth.Claims {
	return &auth.Claims{
		Subject:  "user-1",
		Email:    "test@example.com",
		Name:     "Test User",
		OrgID:    "org-1",
		Provider: "oidc",
	}
}

// setupAuth configures the mock sessions to accept "test-jwt-token".
func setupAuth(m *testutil.Mocks) {
	m.Sessions.On("ValidateSession", "test-jwt-token").
		Return(testClaims(), nil)
}

// ── GET /api/v1/runs/:id/steps ──────────────────────────────

func TestHandleGetRunSteps_Success(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	wfID := "wf-abc"
	m.Querier.On("GetRunWorkflowID", mock.Anything, "run-123").
		Return(&wfID, nil)
	m.Querier.On("GetProjectWorkspaceSlug", mock.Anything, mock.Anything).
		Return("", fmt.Errorf("not a project route")).Maybe()

	now := time.Now()
	m.Engine.On("QueryWorkflow", mock.Anything, "wf-abc").Return(&engine.WorkflowState{
		WorkflowID: "wf-abc",
		RunID:      "run-123",
		Status:     "running",
		StartedAt:  &now,
		Steps: []engine.StepState{
			{Name: "build", Status: "succeeded", ExecType: "run", Wave: 1},
			{Name: "test", Status: "running", ExecType: "run", Wave: 2},
		},
	}, nil)

	dagWaves := json.RawMessage(`[[1],[2]]`)
	m.Querier.On("GetWorkflowDAGWaves", mock.Anything, "wf-abc").
		Return([]byte(dagWaves), nil)

	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/runs/run-123/steps", nil,
		authHeaders()...,
	)

	assert.Equal(t, 200, w.Code)
	resp := parseJSON(t, w)
	assert.Equal(t, "wf-abc", resp["workflowId"])
	assert.Equal(t, "run-123", resp["runId"])
	assert.Equal(t, "running", resp["status"])

	steps, ok := resp["steps"].([]any)
	assert.True(t, ok)
	assert.Len(t, steps, 2)
}

func TestHandleGetRunSteps_NotFound(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	m.Querier.On("GetRunWorkflowID", mock.Anything, "no-such-run").
		Return(nil, fmt.Errorf("not found"))
	m.Querier.On("GetProjectWorkspaceSlug", mock.Anything, mock.Anything).
		Return("", fmt.Errorf("not found")).Maybe()

	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/runs/no-such-run/steps", nil,
		authHeaders()...,
	)

	assert.Equal(t, 404, w.Code)
}

// ── POST /api/v1/runs/:id/cancel ────────────────────────────

func TestHandleCancelRun_Success(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	wfID := "wf-cancel"
	m.Querier.On("GetRunWorkflowID", mock.Anything, "run-cancel").
		Return(&wfID, nil)
	m.Querier.On("GetProjectWorkspaceSlug", mock.Anything, mock.Anything).
		Return("", fmt.Errorf("not a project route")).Maybe()

	m.Engine.On("CancelWorkflow", mock.Anything, "wf-cancel").
		Return(nil)

	w := ut.PerformRequest(srv.Engine(), "POST", "/api/v1/runs/run-cancel/cancel", nil,
		authHeaders()...,
	)

	assert.Equal(t, 200, w.Code)
	resp := parseJSON(t, w)
	assert.Equal(t, true, resp["success"])
}

func TestHandleCancelRun_NotFound(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	m.Querier.On("GetRunWorkflowID", mock.Anything, "no-run").
		Return(nil, fmt.Errorf("not found"))
	m.Querier.On("GetProjectWorkspaceSlug", mock.Anything, mock.Anything).
		Return("", fmt.Errorf("not found")).Maybe()

	w := ut.PerformRequest(srv.Engine(), "POST", "/api/v1/runs/no-run/cancel", nil,
		authHeaders()...,
	)

	assert.Equal(t, 404, w.Code)
}

// ── POST /api/v1/runs/:id/retry ─────────────────────────────

func TestHandleRetryRun_Success(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	branch := "main"
	sha := "abc123"
	projectID := "proj-1"
	workflowFile := "ci.yaml"
	m.Querier.On("GetOriginalRunParams", mock.Anything, "run-retry").Return(db.GetOriginalRunParamsRow{
		ProjectID:    &projectID,
		OrgID:        "org-1",
		WorkflowFile: &workflowFile,
		TriggerRef:   &branch,
		CommitSha:    &sha,
	}, nil)
	m.Querier.On("GetProjectWorkspaceSlug", mock.Anything, mock.Anything).
		Return("", fmt.Errorf("not a project route")).Maybe()

	m.Querier.On("GetProjectRepoInfo", mock.Anything, "proj-1").Return(db.GetProjectRepoInfoRow{
		RepoPath:     "org/repo",
		OrgID:        "org-1",
		PipelinePath: ".flint/",
	}, nil)

	// startResolved validates the pipeline's runner pools against the catalog.
	m.Querier.On("ListMachinePools", mock.Anything).Return([]db.ListMachinePoolsRow{}, nil)

	m.Querier.On("InsertRetryRun", mock.Anything, mock.MatchedBy(func(p db.InsertRetryRunParams) bool {
		return p.ProjectID != nil && *p.ProjectID == "proj-1" && p.OrgID == "org-1" &&
			p.WorkflowFile != nil && *p.WorkflowFile == "ci.yaml"
	})).Return(nil)

	// The CI service re-fetches the pipeline from the forge, compiles it, and
	// starts it on the engine via StartWorkflowWithWaves.
	m.Forge.On("GetFile", mock.Anything, "org/repo", mock.Anything, mock.Anything).Return([]byte(`
triggers:
  push: { branches: [main] }
jobs:
  build:
    image: alpine
    steps:
      - run: echo hi
`), nil)

	m.Engine.On("StartWorkflowWithWaves", mock.Anything, mock.MatchedBy(func(inp engine.StartWorkflowInput) bool {
		return inp.ProjectID == "proj-1" && inp.TriggerType == "retry" && inp.Repo == "org/repo"
	}), mock.Anything).Return("wf-retry-1", nil)

	w := ut.PerformRequest(srv.Engine(), "POST", "/api/v1/runs/run-retry/retry", nil,
		authHeaders()...,
	)

	assert.Equal(t, 202, w.Code)
	resp := parseJSON(t, w)
	assert.Equal(t, "pending", resp["status"])
	assert.Equal(t, "wf-retry-1", resp["workflowId"])
}

func TestHandleRetryRun_OrigNotFound(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	m.Querier.On("GetOriginalRunParams", mock.Anything, "gone").
		Return(db.GetOriginalRunParamsRow{}, fmt.Errorf("not found"))
	m.Querier.On("GetProjectWorkspaceSlug", mock.Anything, mock.Anything).
		Return("", fmt.Errorf("not found")).Maybe()

	w := ut.PerformRequest(srv.Engine(), "POST", "/api/v1/runs/gone/retry", nil,
		authHeaders()...,
	)

	assert.Equal(t, 404, w.Code)
}

// ── Auth middleware rejection ────────────────────────────────

func TestAuthMiddleware_NoToken(t *testing.T) {
	srv, _ := testServer(t)

	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/runs/run-1/steps", nil)

	assert.Equal(t, 401, w.Code)
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	srv, m := testServer(t)

	m.Sessions.On("ValidateSession", "bad-token").
		Return(nil, fmt.Errorf("invalid token"))

	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/runs/run-1/steps", nil,
		ut.Header{Key: "Authorization", Value: "Bearer bad-token"},
	)

	assert.Equal(t, 401, w.Code)
}
