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
)

// ── GET /api/v1/projects/:id/webhooks ───────────────────────

func TestHandleListWebhooks_Success(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	m.Querier.On("GetProjectWorkspaceSlug", mock.Anything, "proj-1").
		Return("default", nil).Maybe()

	eventsJSON, _ := json.Marshal([]string{"run.completed", "run.failed"})
	m.Querier.On("ListProjectWebhooks", mock.Anything, "proj-1").Return([]db.Webhook{
		{
			ID:        "wh-1",
			ProjectID: "proj-1",
			Url:       "https://example.com/hook",
			Secret:    "s3cret",
			Events:    eventsJSON,
			IsActive:  true,
			CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			ID:        "wh-2",
			ProjectID: "proj-1",
			Url:       "https://other.com/hook",
			Secret:    "",
			Events:    eventsJSON,
			IsActive:  false,
			CreatedAt: time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
		},
	}, nil)

	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/projects/proj-1/webhooks", nil,
		authHeaders()...,
	)

	assert.Equal(t, 200, w.Code)
	resp := parseJSON(t, w)

	webhooks, ok := resp["items"].([]any)
	assert.True(t, ok, "expected items array")
	assert.Len(t, webhooks, 2)

	first := webhooks[0].(map[string]any)
	assert.Equal(t, "wh-1", first["id"])
	assert.Equal(t, "https://example.com/hook", first["url"])
	assert.Equal(t, true, first["hasSecret"], "secret should be masked as hasSecret=true")
	assert.Equal(t, true, first["isActive"])

	second := webhooks[1].(map[string]any)
	assert.Equal(t, false, second["hasSecret"])
	assert.Equal(t, false, second["isActive"])
}

func TestHandleListWebhooks_Empty(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	m.Querier.On("GetProjectWorkspaceSlug", mock.Anything, "proj-empty").
		Return("default", nil).Maybe()
	m.Querier.On("ListProjectWebhooks", mock.Anything, "proj-empty").
		Return([]db.Webhook{}, nil)

	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/projects/proj-empty/webhooks", nil,
		authHeaders()...,
	)

	assert.Equal(t, 200, w.Code)
	resp := parseJSON(t, w)
	webhooks := resp["items"].([]any)
	assert.Empty(t, webhooks)
}

func TestHandleListWebhooks_DBError(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	m.Querier.On("GetProjectWorkspaceSlug", mock.Anything, "proj-err").
		Return("default", nil).Maybe()
	m.Querier.On("ListProjectWebhooks", mock.Anything, "proj-err").
		Return(nil, fmt.Errorf("db error"))

	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/projects/proj-err/webhooks", nil,
		authHeaders()...,
	)

	assert.Equal(t, 500, w.Code)
}

// ── POST /api/v1/projects/:id/webhooks ──────────────────────

func TestHandleCreateWebhook_Success(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	m.Querier.On("GetProjectWorkspaceSlug", mock.Anything, "proj-1").
		Return("default", nil).Maybe()

	m.Querier.On("CreateWebhook", mock.Anything, mock.MatchedBy(func(p db.CreateWebhookParams) bool {
		return p.ProjectID == "proj-1" && p.Url == "https://example.com/hook" && p.Secret == "mysecret"
	})).Return("wh-new", nil)

	body := jsonBody(t, map[string]any{
		"url":    "https://example.com/hook",
		"secret": "mysecret",
		"events": []string{"run.completed"},
	})

	w := ut.PerformRequest(srv.Engine(), "POST", "/api/v1/projects/proj-1/webhooks", body,
		authHeaders()...,
	)

	assert.Equal(t, 201, w.Code)
	resp := parseJSON(t, w)
	assert.Equal(t, "wh-new", resp["id"])
}

func TestHandleCreateWebhook_DefaultEvents(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	m.Querier.On("GetProjectWorkspaceSlug", mock.Anything, "proj-1").
		Return("default", nil).Maybe()

	// When no events are provided, defaults to ["run.completed", "run.failed"].
	m.Querier.On("CreateWebhook", mock.Anything, mock.MatchedBy(func(p db.CreateWebhookParams) bool {
		var events []string
		_ = json.Unmarshal(p.Events, &events)
		return len(events) == 2 && events[0] == "run.completed" && events[1] == "run.failed"
	})).Return("wh-default", nil)

	body := jsonBody(t, map[string]any{
		"url": "https://example.com/hook2",
	})

	w := ut.PerformRequest(srv.Engine(), "POST", "/api/v1/projects/proj-1/webhooks", body,
		authHeaders()...,
	)

	assert.Equal(t, 201, w.Code)
}

func TestHandleCreateWebhook_MissingURL(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	m.Querier.On("GetProjectWorkspaceSlug", mock.Anything, "proj-1").
		Return("default", nil).Maybe()

	body := jsonBody(t, map[string]any{
		"secret": "s3cret",
	})

	w := ut.PerformRequest(srv.Engine(), "POST", "/api/v1/projects/proj-1/webhooks", body,
		authHeaders()...,
	)

	assert.Equal(t, 400, w.Code)
}

func TestHandleCreateWebhook_DBError(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	m.Querier.On("GetProjectWorkspaceSlug", mock.Anything, "proj-1").
		Return("default", nil).Maybe()
	m.Querier.On("CreateWebhook", mock.Anything, mock.Anything).
		Return("", fmt.Errorf("unique constraint"))

	body := jsonBody(t, map[string]any{
		"url": "https://example.com/hook",
	})

	w := ut.PerformRequest(srv.Engine(), "POST", "/api/v1/projects/proj-1/webhooks", body,
		authHeaders()...,
	)

	assert.Equal(t, 500, w.Code)
}

// ── DELETE /api/v1/projects/:id/webhooks/:webhook ─────────

func TestHandleDeleteWebhook_Success(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	m.Querier.On("GetProjectWorkspaceSlug", mock.Anything, "proj-1").
		Return("default", nil).Maybe()

	m.Querier.On("DeleteWebhook", mock.Anything, db.DeleteWebhookParams{
		ID:        "wh-del",
		ProjectID: "proj-1",
	}).Return(nil)

	w := ut.PerformRequest(srv.Engine(), "DELETE", "/api/v1/projects/proj-1/webhooks/wh-del", nil,
		authHeaders()...,
	)

	assert.Equal(t, 200, w.Code)
	resp := parseJSON(t, w)
	assert.Equal(t, true, resp["success"])
}

func TestHandleDeleteWebhook_DBError(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	m.Querier.On("GetProjectWorkspaceSlug", mock.Anything, "proj-1").
		Return("default", nil).Maybe()
	m.Querier.On("DeleteWebhook", mock.Anything, mock.Anything).
		Return(fmt.Errorf("not found"))

	w := ut.PerformRequest(srv.Engine(), "DELETE", "/api/v1/projects/proj-1/webhooks/wh-404", nil,
		authHeaders()...,
	)

	assert.Equal(t, 500, w.Code)
}

// ── Webhook CRUD without auth ───────────────────────────────

func TestWebhookEndpoints_RequireAuth(t *testing.T) {
	srv, _ := testServer(t)

	tests := []struct {
		method string
		url    string
	}{
		{"GET", "/api/v1/projects/proj-1/webhooks"},
		{"POST", "/api/v1/projects/proj-1/webhooks"},
		{"DELETE", "/api/v1/projects/proj-1/webhooks/wh-1"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.url, func(t *testing.T) {
			w := ut.PerformRequest(srv.Engine(), tt.method, tt.url, nil)
			assert.Equal(t, 401, w.Code, "expected 401 for unauthenticated %s %s", tt.method, tt.url)
		})
	}
}
