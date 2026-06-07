package server

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleCapabilities(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/capabilities", nil, authHeaders()...)
	require.Equal(t, 200, w.Code)

	resp := parseJSON(t, w)
	products, ok := resp["products"].([]any)
	require.True(t, ok)
	require.Len(t, products, 3)

	byID := map[string]map[string]any{}
	for _, p := range products {
		entry := p.(map[string]any)
		byID[entry["id"].(string)] = entry
	}

	// Defaults: CI + Workflows on, Load Testing coming soon.
	assert.Equal(t, true, byID["ci"]["enabled"])
	assert.Equal(t, "enabled", byID["ci"]["status"])
	assert.Equal(t, true, byID["workflows"]["enabled"])
	assert.Equal(t, "enabled", byID["workflows"]["status"])
	assert.Equal(t, false, byID["loadtest"]["enabled"])
	assert.Equal(t, "coming_soon", byID["loadtest"]["status"])
}

func TestHandleCapabilities_RequiresAuth(t *testing.T) {
	srv, _ := testServer(t)
	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/capabilities", nil)
	assert.Equal(t, 401, w.Code)
}
