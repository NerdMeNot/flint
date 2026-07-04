package logsink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPSink ships log lines to the flint-server /internal/logs endpoint. It is
// the agent-side production sink: the server owns durable storage (filesystem
// or S3 via its own configured sink) and fans lines out to SSE subscribers for
// live tailing. The agent never reads logs back, so Read and Tail are
// unsupported.
//
// Identity (org/run) is derived server-side from the signed task token; the
// sink only sends the step name and matrix key from the LogRef.
type HTTPSink struct {
	// ServerURL is the flint-server base URL (e.g. "http://flint-server.flint:8080").
	ServerURL string
	// TaskToken is the signed per-step engine token (X-Flint-Task-Token).
	TaskToken string
	// InternalToken is the shared /internal endpoint secret (X-Flint-Internal-Token).
	InternalToken string
	// Client is the HTTP client to use. Defaults to a 10s-timeout client.
	Client *http.Client
}

func (h *HTTPSink) httpClient() *http.Client {
	if h.Client != nil {
		return h.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// Write POSTs a batch of log lines to /internal/logs.
func (h *HTTPSink) Write(ctx context.Context, ref LogRef, lines []LogLine) error {
	if len(lines) == 0 {
		return nil
	}
	if h.ServerURL == "" {
		return fmt.Errorf("logsink/http: server URL not set")
	}

	body, err := json.Marshal(map[string]any{
		"stepName":  ref.StepName,
		"matrixKey": ref.MatrixKey,
		"lines":     lines,
	})
	if err != nil {
		return fmt.Errorf("logsink/http: marshal log batch: %w", err)
	}

	url := strings.TrimRight(h.ServerURL, "/") + "/internal/logs"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("logsink/http: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if h.TaskToken != "" {
		req.Header.Set("X-Flint-Task-Token", h.TaskToken)
	}
	if h.InternalToken != "" {
		req.Header.Set("X-Flint-Internal-Token", h.InternalToken)
	}

	resp, err := h.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("logsink/http: POST /internal/logs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("logsink/http: /internal/logs returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// Read is unsupported — the agent only ships logs; reads go through the server.
func (h *HTTPSink) Read(context.Context, LogRef) ([]LogLine, error) {
	return nil, fmt.Errorf("logsink/http: Read is not supported (write-only shipper)")
}

// Tail is unsupported — live tailing is the server's SSE fan-out.
func (h *HTTPSink) Tail(context.Context, LogRef) (<-chan LogLine, error) {
	return nil, fmt.Errorf("logsink/http: Tail is not supported (write-only shipper)")
}
