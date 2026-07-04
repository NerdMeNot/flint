package server

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/NerdMeNot/flint/internal/products/ci"
	"github.com/NerdMeNot/flint/internal/testutil"
	"github.com/cloudwego/hertz/pkg/common/ut"
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
