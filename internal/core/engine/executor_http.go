package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// httpExecutor runs an HTTP-request step in-process — no Pod, no container, no
// agent. It is the proof that the StepExecutor seam is execution-model-agnostic:
// a step can be "make an HTTP call" as readily as "run a container". Used by
// Flint Workflows; completion is reported via the shared CompleteFunc.
type httpExecutor struct {
	client   *http.Client
	complete CompleteFunc
}

// NewHTTPExecutor builds the in-process HTTP executor.
func NewHTTPExecutor(complete CompleteFunc) *httpExecutor {
	return &httpExecutor{client: &http.Client{Timeout: 30 * time.Second}, complete: complete}
}

func (e *httpExecutor) Kind() string { return "http" }

// Dispatch fires the request in a goroutine and reports the outcome via the
// completion callback. The handle is the run/step identity.
func (e *httpExecutor) Dispatch(ctx context.Context, step claimedStep) (string, error) {
	var def pipeline.Step
	if err := json.Unmarshal(step.stepDef, &def); err != nil {
		return "", fmt.Errorf("engine: unmarshal step def: %w", err)
	}
	if def.HTTP == nil || def.HTTP.URL == "" {
		return "", fmt.Errorf("engine: http step %q missing url", step.name)
	}
	go e.run(step, def.HTTP)
	return step.runID + "/" + step.name, nil
}

func (e *httpExecutor) run(step claimedStep, spec *pipeline.HTTPStep) {
	ctx := context.Background()
	result := e.do(ctx, step, spec)
	log.Info().Str("step", step.name).Str("kind", "http").
		Bool("ok", result.Success).Int("status", result.ExitCode).
		Msg("engine: http step finished")
	if e.complete != nil {
		if err := e.complete(ctx, step.taskToken, result); err != nil {
			log.Error().Err(err).Str("step", step.name).Msg("engine: http completion callback failed")
		}
	}
}

func (e *httpExecutor) do(ctx context.Context, step claimedStep, spec *pipeline.HTTPStep) StepResult {
	method := spec.Method
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if spec.Body != "" {
		body = bytes.NewReader([]byte(spec.Body))
	}
	req, err := http.NewRequestWithContext(ctx, method, spec.URL, body)
	if err != nil {
		return StepResult{StepName: step.name, Success: false, Error: err.Error()}
	}
	for k, v := range spec.Headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return StepResult{StepName: step.name, Success: false, Error: err.Error()}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20)) // drain to allow connection reuse

	ok := statusAccepted(resp.StatusCode, spec.ExpectStatus)
	res := StepResult{StepName: step.name, Success: ok, ExitCode: resp.StatusCode}
	if !ok {
		res.Error = fmt.Sprintf("unexpected status %d", resp.StatusCode)
	}
	return res
}

// statusAccepted reports whether code is acceptable: present in expect (when
// given), otherwise any 2xx.
func statusAccepted(code int, expect []int) bool {
	if len(expect) == 0 {
		return code >= 200 && code < 300
	}
	return slices.Contains(expect, code)
}
