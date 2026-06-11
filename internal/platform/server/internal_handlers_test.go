package server

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/products/ci"
	"github.com/NerdMeNot/flint/internal/testutil"
	"github.com/NerdMeNot/flint/pkg/logsink"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func testServer(t *testing.T) (*Server, *testutil.Mocks) {
	t.Helper()
	m := testutil.NewMocks(t)
	deps := Deps{
		Config:       testutil.TestConfig(),
		DB:           m.Pool,
		Q:            m.Querier,
		Engine:       m.Engine,
		Forge:        m.Forge,
		Secrets:      m.Secrets,
		Logs:         m.Logs,
		LogBroadcast: m.LogStream,
		Sessions:     m.Sessions,
		// Wire the CI product as the composition root would, so handlers that
		// create runs (webhook / manual / retry) exercise the real service.
		Runs: ci.NewService(m.Engine, m.Forge, m.Querier),
	}
	srv := New(deps)
	return srv, m
}

func internalAuthHeader() ut.Header {
	return ut.Header{Key: "X-Flint-Internal-Token", Value: "test-internal-token"}
}

func jsonBody(t *testing.T, v any) *ut.Body {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return &ut.Body{Body: bytes.NewReader(b), Len: len(b)}
}

func parseJSON(t *testing.T, rec *ut.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	return result
}

// ── POST /internal/complete ─────────────────────────────────

func TestHandleAgentComplete_Success(t *testing.T) {
	srv, m := testServer(t)

	m.Engine.On("CompleteStep", mock.Anything, "tok-123", engine.StepResult{
		StepName: "build",
		Success:  true,
		ExitCode: 0,
	}).Return(nil)

	body := jsonBody(t, map[string]any{
		"taskToken": "tok-123",
		"result": map[string]any{
			"stepName": "build",
			"success":  true,
			"exitCode": 0,
		},
	})

	w := ut.PerformRequest(srv.Engine(), "POST", "/internal/complete", body,
		internalAuthHeader(),
		ut.Header{Key: "Content-Type", Value: "application/json"},
	)

	assert.Equal(t, 200, w.Code)
	resp := parseJSON(t, w)
	assert.Equal(t, "ok", resp["status"])
}

func TestHandleAgentComplete_MissingToken(t *testing.T) {
	srv, _ := testServer(t)

	w := ut.PerformRequest(srv.Engine(), "POST", "/internal/complete", nil)

	assert.Equal(t, 401, w.Code)
	resp := parseJSON(t, w)
	assert.Contains(t, resp["error"], "missing")
}

func TestHandleAgentComplete_InvalidToken(t *testing.T) {
	srv, _ := testServer(t)

	body := jsonBody(t, map[string]any{
		"taskToken": "tok-123",
		"result":    map[string]any{"stepName": "build", "success": true},
	})

	w := ut.PerformRequest(srv.Engine(), "POST", "/internal/complete", body,
		ut.Header{Key: "X-Flint-Internal-Token", Value: "wrong-token"},
		ut.Header{Key: "Content-Type", Value: "application/json"},
	)

	assert.Equal(t, 403, w.Code)
	resp := parseJSON(t, w)
	assert.Contains(t, resp["error"], "invalid")
}

func TestHandleAgentComplete_MissingTaskToken(t *testing.T) {
	srv, _ := testServer(t)

	body := jsonBody(t, map[string]any{
		"result": map[string]any{"stepName": "build", "success": true},
	})

	w := ut.PerformRequest(srv.Engine(), "POST", "/internal/complete", body,
		internalAuthHeader(),
		ut.Header{Key: "Content-Type", Value: "application/json"},
	)

	assert.Equal(t, 400, w.Code)
	resp := parseJSON(t, w)
	errObj, ok := resp["error"].(map[string]any)
	assert.True(t, ok, "expected standard error envelope")
	assert.Contains(t, errObj["message"], "taskToken is required")
}

func TestHandleAgentComplete_InvalidBody(t *testing.T) {
	srv, _ := testServer(t)

	badBody := &ut.Body{Body: bytes.NewReader([]byte("not json")), Len: 8}

	w := ut.PerformRequest(srv.Engine(), "POST", "/internal/complete", badBody,
		internalAuthHeader(),
		ut.Header{Key: "Content-Type", Value: "application/json"},
	)

	assert.Equal(t, 400, w.Code)
}

// ── POST /internal/logs ─────────────────────────────────────

func TestHandleAgentLogIngestion_Success(t *testing.T) {
	srv, m := testServer(t)

	lines := []logsink.LogLine{
		{Stream: "stdout", Content: "hello world"},
	}

	m.Logs.On("Write", mock.Anything, logsink.LogRef{
		OrgID:    "org-1",
		RunID:    "run-1",
		StepName: "build",
	}, mock.MatchedBy(func(l []logsink.LogLine) bool {
		return len(l) == 1 && l[0].Content == "hello world"
	})).Return(nil)

	m.LogStream.On("Publish", "run-1", "build", mock.MatchedBy(func(l []logsink.LogLine) bool {
		return len(l) == 1 && l[0].Content == "hello world"
	})).Return()

	body := jsonBody(t, map[string]any{
		"runId":    "run-1",
		"stepName": "build",
		"orgId":    "org-1",
		"lines":    lines,
	})

	w := ut.PerformRequest(srv.Engine(), "POST", "/internal/logs", body,
		internalAuthHeader(),
		ut.Header{Key: "Content-Type", Value: "application/json"},
	)

	assert.Equal(t, 200, w.Code)
	resp := parseJSON(t, w)
	assert.Equal(t, "ok", resp["status"])
	assert.Equal(t, float64(1), resp["lines"])
}

func TestHandleAgentLogIngestion_InvalidBody(t *testing.T) {
	srv, _ := testServer(t)

	badBody := &ut.Body{Body: bytes.NewReader([]byte("{")), Len: 1}

	w := ut.PerformRequest(srv.Engine(), "POST", "/internal/logs", badBody,
		internalAuthHeader(),
		ut.Header{Key: "Content-Type", Value: "application/json"},
	)

	assert.Equal(t, 400, w.Code)
}

func TestHandleAgentLogIngestion_NoLogSink(t *testing.T) {
	m := testutil.NewMocks(t)
	deps := Deps{
		Config:   testutil.TestConfig(),
		DB:       m.Pool,
		Q:        m.Querier,
		Engine:   m.Engine,
		Sessions: m.Sessions,
		Logs:     nil, // no log sink
	}
	srv := New(deps)

	body := jsonBody(t, map[string]any{
		"runId":    "run-1",
		"stepName": "build",
		"orgId":    "org-1",
		"lines":    []logsink.LogLine{{Stream: "stdout", Content: "hi"}},
	})

	w := ut.PerformRequest(srv.Engine(), "POST", "/internal/logs", body,
		internalAuthHeader(),
		ut.Header{Key: "Content-Type", Value: "application/json"},
	)

	assert.Equal(t, 200, w.Code)
	resp := parseJSON(t, w)
	assert.Equal(t, "logs not configured", resp["status"])
}
