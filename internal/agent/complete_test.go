package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompleteActivity_SuccessFirstTry(t *testing.T) {
	var called int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&called, 1)

		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/internal/complete", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		defer r.Body.Close()

		var payload map[string]any
		require.NoError(t, json.Unmarshal(body, &payload))
		assert.Equal(t, "test-token", payload["taskToken"])

		w.WriteHeader(200)
	}))
	defer srv.Close()

	cfg := &Config{
		ServerURL: srv.URL,
		TaskToken: "test-token",
	}
	result := &StepResult{
		StepName: "build",
		Success:  true,
		ExitCode: 0,
	}

	err := completeActivity(cfg, result)
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&called))
}

func TestCompleteActivity_InternalTokenHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "my-secret-token", r.Header.Get("X-Flint-Internal-Token"))
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cfg := &Config{
		ServerURL:     srv.URL,
		TaskToken:     "test-token",
		InternalToken: "my-secret-token",
	}
	result := &StepResult{StepName: "build", Success: true}

	err := completeActivity(cfg, result)
	require.NoError(t, err)
}

func TestCompleteActivity_RetryOnServerError(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(502)
			_, _ = w.Write([]byte("bad gateway"))
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cfg := &Config{
		ServerURL: srv.URL,
		TaskToken: "test-token",
	}
	result := &StepResult{StepName: "build", Success: true, ExitCode: 0}

	err := completeActivity(cfg, result)
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

func TestCompleteActivity_NoRetryOnClientError(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(400)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	cfg := &Config{
		ServerURL: srv.URL,
		TaskToken: "test-token",
	}
	result := &StepResult{StepName: "build", Success: true, ExitCode: 0}

	err := completeActivity(cfg, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
	assert.Contains(t, err.Error(), "bad request")
	// Only called once — no retry for 4xx.
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestCompleteActivity_FailAfterMaxRetries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(500)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer srv.Close()

	cfg := &Config{
		ServerURL: srv.URL,
		TaskToken: "test-token",
	}
	result := &StepResult{StepName: "build", Success: true, ExitCode: 0}

	err := completeActivity(cfg, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed after")
	// Initial attempt + 3 retries = 4 total calls.
	assert.Equal(t, int32(completeMaxRetries+1), atomic.LoadInt32(&calls))
}

func TestCompleteActivity_MissingServerURL(t *testing.T) {
	cfg := &Config{
		ServerURL: "",
		TaskToken: "test-token",
	}
	result := &StepResult{StepName: "build", Success: true, ExitCode: 0}

	err := completeActivity(cfg, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FLINT_SERVER_URL not set")
}

func TestCompleteActivity_TrailingSlashInURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/internal/complete", r.URL.Path)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cfg := &Config{
		ServerURL: srv.URL + "/",
		TaskToken: "test-token",
	}
	result := &StepResult{StepName: "build", Success: true, ExitCode: 0}

	err := completeActivity(cfg, result)
	require.NoError(t, err)
}
