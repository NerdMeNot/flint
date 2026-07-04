package logsink

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPSink_Write(t *testing.T) {
	type ingestReq struct {
		StepName  string    `json:"stepName"`
		MatrixKey string    `json:"matrixKey"`
		Lines     []LogLine `json:"lines"`
	}

	var got ingestReq
	var gotTaskToken, gotInternalToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/internal/logs", r.URL.Path)
		gotTaskToken = r.Header.Get("X-Flint-Task-Token")
		gotInternalToken = r.Header.Get("X-Flint-Internal-Token")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sink := &HTTPSink{ServerURL: srv.URL + "/", TaskToken: "task-tok", InternalToken: "internal-tok"}
	lines := []LogLine{
		{Timestamp: time.Now(), Stream: "stdout", Content: "hello"},
		{Timestamp: time.Now(), Stream: "stdout", Content: "world"},
	}
	ref := LogRef{OrgID: "org-1", RunID: "run-1", StepName: "build", MatrixKey: "node=20"}

	require.NoError(t, sink.Write(context.Background(), ref, lines))

	assert.Equal(t, "task-tok", gotTaskToken)
	assert.Equal(t, "internal-tok", gotInternalToken)
	assert.Equal(t, "build", got.StepName)
	assert.Equal(t, "node=20", got.MatrixKey)
	require.Len(t, got.Lines, 2)
	assert.Equal(t, "hello", got.Lines[0].Content)
}

func TestHTTPSink_Write_EmptyBatchIsNoop(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	sink := &HTTPSink{ServerURL: srv.URL}
	require.NoError(t, sink.Write(context.Background(), LogRef{StepName: "build"}, nil))
	assert.False(t, called, "empty batches must not hit the server")
}

func TestHTTPSink_Write_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	sink := &HTTPSink{ServerURL: srv.URL}
	err := sink.Write(context.Background(), LogRef{StepName: "build"}, []LogLine{{Content: "x"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestHTTPSink_ReadTailUnsupported(t *testing.T) {
	sink := &HTTPSink{ServerURL: "http://example.invalid"}
	_, err := sink.Read(context.Background(), LogRef{})
	require.Error(t, err)
	_, err = sink.Tail(context.Background(), LogRef{})
	require.Error(t, err)
}
