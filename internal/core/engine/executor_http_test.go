package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

var _ StepExecutor = (*httpExecutor)(nil)

func httpStepDef(t *testing.T, name string, spec *pipeline.HTTPStep) []byte {
	t.Helper()
	b, err := json.Marshal(pipeline.Step{Name: name, HTTP: spec})
	require.NoError(t, err)
	return b
}

func TestHTTPExecutor_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.Header.Get("X-Test") == "yes" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	done := make(chan StepResult, 1)
	e := NewHTTPExecutor(func(_ context.Context, tok string, r StepResult) error {
		assert.Equal(t, "task-token", tok)
		done <- r
		return nil
	})
	step := claimedStep{
		name: "ping", execType: "http", runID: "run-1", taskToken: "task-token",
		stepDef: httpStepDef(t, "ping", &pipeline.HTTPStep{
			Method: http.MethodPost, URL: srv.URL, Body: "{}",
			Headers: map[string]string{"X-Test": "yes"},
		}),
	}

	h, err := e.Dispatch(context.Background(), step)
	require.NoError(t, err)
	require.NotEmpty(t, h)

	select {
	case r := <-done:
		assert.True(t, r.Success)
		assert.Equal(t, http.StatusOK, r.ExitCode)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for http step")
	}
}

func TestHTTPExecutor_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	done := make(chan StepResult, 1)
	e := NewHTTPExecutor(func(_ context.Context, _ string, r StepResult) error { done <- r; return nil })
	_, err := e.Dispatch(context.Background(), claimedStep{
		name: "x", execType: "http", stepDef: httpStepDef(t, "x", &pipeline.HTTPStep{URL: srv.URL}),
	})
	require.NoError(t, err)

	r := <-done
	assert.False(t, r.Success)
	assert.Equal(t, http.StatusInternalServerError, r.ExitCode)
}

func TestHTTPExecutor_ExpectStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted) // 202
	}))
	defer srv.Close()

	done := make(chan StepResult, 1)
	e := NewHTTPExecutor(func(_ context.Context, _ string, r StepResult) error { done <- r; return nil })
	_, err := e.Dispatch(context.Background(), claimedStep{
		name: "x", execType: "http",
		stepDef: httpStepDef(t, "x", &pipeline.HTTPStep{URL: srv.URL, ExpectStatus: []int{202}}),
	})
	require.NoError(t, err)
	assert.True(t, (<-done).Success, "202 should pass when listed in expectStatus")
}

func TestHTTPExecutor_MissingURL(t *testing.T) {
	e := NewHTTPExecutor(nil)
	_, err := e.Dispatch(context.Background(), claimedStep{
		name: "x", execType: "http", stepDef: httpStepDef(t, "x", &pipeline.HTTPStep{}),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing url")
}
