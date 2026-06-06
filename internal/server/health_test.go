package server

import (
	"fmt"
	"testing"

	"github.com/NerdMeNot/flint/internal/testutil"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// ── GET /health/live ────────────────────────────────────────

func TestHandleLive_ReturnsAlive(t *testing.T) {
	srv, _ := testServer(t)

	w := ut.PerformRequest(srv.Engine(), "GET", "/health/live", nil)

	assert.Equal(t, 200, w.Code)
	resp := parseJSON(t, w)
	assert.Equal(t, "alive", resp["status"])
}

// ── GET /health/ready ───────────────────────────────────────

func TestHandleReady_Healthy(t *testing.T) {
	srv, m := testServer(t)

	m.Pool.On("Ping", mock.Anything).Return(nil)

	w := ut.PerformRequest(srv.Engine(), "GET", "/health/ready", nil)

	assert.Equal(t, 200, w.Code)
	resp := parseJSON(t, w)
	assert.Equal(t, "healthy", resp["status"])

	checks, ok := resp["checks"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "healthy", checks["database"])
	assert.Equal(t, "ready", checks["engine"])
}

func TestHandleReady_DatabaseUnhealthy(t *testing.T) {
	srv, m := testServer(t)

	m.Pool.On("Ping", mock.Anything).Return(fmt.Errorf("connection refused"))

	w := ut.PerformRequest(srv.Engine(), "GET", "/health/ready", nil)

	assert.Equal(t, 503, w.Code)
	resp := parseJSON(t, w)
	assert.Equal(t, "unhealthy", resp["status"])

	checks, ok := resp["checks"].(map[string]any)
	assert.True(t, ok)
	assert.Contains(t, checks["database"], "unhealthy")
}

func TestHandleReady_NoDB(t *testing.T) {
	// Server with nil DB and nil Engine -- should still be "healthy"
	// because there's nothing to fail.
	deps := Deps{
		Config: testutil.TestConfig(),
	}
	srv := New(deps)

	w := ut.PerformRequest(srv.Engine(), "GET", "/health/ready", nil)

	assert.Equal(t, 200, w.Code)
	resp := parseJSON(t, w)
	assert.Equal(t, "healthy", resp["status"])
}

// ── CORS headers ────────────────────────────────────────────

func TestCORS_OptionsRequest(t *testing.T) {
	srv, _ := testServer(t)

	w := ut.PerformRequest(srv.Engine(), "OPTIONS", "/health/live", nil)

	assert.Equal(t, 204, w.Code)
	header := w.Header()
	assert.Equal(t, "*", header.Get("Access-Control-Allow-Origin"))
	assert.Contains(t, header.Get("Access-Control-Allow-Methods"), "GET")
}

func TestCORS_RegularRequest(t *testing.T) {
	srv, _ := testServer(t)

	w := ut.PerformRequest(srv.Engine(), "GET", "/health/live", nil)

	header := w.Header()
	assert.Equal(t, "*", header.Get("Access-Control-Allow-Origin"))
}
