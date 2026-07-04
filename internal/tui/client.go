package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/NerdMeNot/flint/internal/cliauth"
)

// Client is a typed HTTP client for the Flint server API, shared by the TUI
// and the flint CLI verbs (run, logs). It authenticates with the stored
// credentials and transparently refreshes an expired session token once.
type Client struct {
	baseURL    string
	httpClient *http.Client
	creds      *cliauth.Credentials
}

// NewClient creates an unauthenticated client (demo/dev servers only — every
// production API route requires auth; prefer NewAuthedClient).
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// NewAuthedClient creates a client using stored credentials.
func NewAuthedClient(creds *cliauth.Credentials) *Client {
	return &Client{
		baseURL:    strings.TrimRight(creds.ServerURL, "/"),
		httpClient: &http.Client{Timeout: 10 * time.Second},
		creds:      creds,
	}
}

// do executes an authenticated request. On a 401 with a refreshable session
// it rotates the token once and retries.
func (c *Client) do(ctx context.Context, method, path string, body any, result any) error {
	send := func() (*http.Response, error) {
		var reader io.Reader
		if body != nil {
			jsonBody, err := json.Marshal(body)
			if err != nil {
				return nil, err
			}
			reader = strings.NewReader(string(jsonBody))
		}
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
		if err != nil {
			return nil, err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if c.creds != nil && c.creds.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.creds.Token)
		}
		return c.httpClient.Do(req)
	}

	resp, err := send()
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized && c.creds != nil && !c.creds.IsPAT() {
		resp.Body.Close()
		if rerr := cliauth.Refresh(ctx, c.creds); rerr != nil {
			return rerr
		}
		if resp, err = send(); err != nil {
			return fmt.Errorf("request failed: %w", err)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("API %d: %s", resp.StatusCode, apiErrorMessage(raw))
	}
	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// apiErrorMessage extracts the message from the error envelope, falling back
// to the raw body.
func apiErrorMessage(raw []byte) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) == nil && envelope.Error.Message != "" {
		return envelope.Error.Message
	}
	return strings.TrimSpace(string(raw))
}

func (c *Client) get(ctx context.Context, path string, result any) error {
	return c.do(ctx, http.MethodGet, path, nil, result)
}

func (c *Client) post(ctx context.Context, path string, body any, result any) error {
	return c.do(ctx, http.MethodPost, path, body, result)
}

// ListProjects fetches all projects.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	var resp struct {
		Projects []Project `json:"projects"`
	}
	err := c.get(ctx, "/api/v1/projects", &resp)
	return resp.Projects, err
}

// ResolveProject turns a project name (or id) into its id.
func (c *Client) ResolveProject(ctx context.Context, nameOrID string) (string, error) {
	projects, err := c.ListProjects(ctx)
	if err != nil {
		return "", err
	}
	for _, p := range projects {
		if p.ID == nameOrID || p.DisplayName == nameOrID || p.RepoPath == nameOrID {
			return p.ID, nil
		}
	}
	names := make([]string, 0, len(projects))
	for _, p := range projects {
		names = append(names, p.DisplayName)
	}
	return "", fmt.Errorf("project %q not found (available: %s)", nameOrID, strings.Join(names, ", "))
}

// ListRuns fetches runs for a project.
func (c *Client) ListRuns(ctx context.Context, projectID string) ([]Run, error) {
	var resp struct {
		Runs []Run `json:"runs"`
	}
	err := c.get(ctx, fmt.Sprintf("/api/v1/projects/%s/runs", projectID), &resp)
	return resp.Runs, err
}

// GetRun fetches a single run.
func (c *Client) GetRun(ctx context.Context, runID string) (*Run, error) {
	var run Run
	err := c.get(ctx, fmt.Sprintf("/api/v1/runs/%s", runID), &run)
	return &run, err
}

// GetRunSteps fetches workflow step states for a run.
func (c *Client) GetRunSteps(ctx context.Context, runID string) (*WorkflowState, error) {
	var state WorkflowState
	err := c.get(ctx, fmt.Sprintf("/api/v1/runs/%s/steps", runID), &state)
	return &state, err
}

// GetOrg fetches the organization.
func (c *Client) GetOrg(ctx context.Context) (*Org, error) {
	var org Org
	err := c.get(ctx, "/api/v1/org", &org)
	return &org, err
}

// TriggerRun starts a new pipeline run.
func (c *Client) TriggerRun(ctx context.Context, projectID, branch, workflowFile, environment string) (*Run, error) {
	var resp struct {
		ID         string `json:"id"`
		WorkflowID string `json:"workflowId"`
		Status     string `json:"status"`
	}
	err := c.post(ctx, "/api/v1/runs", map[string]string{
		"projectId":    projectID,
		"branch":       branch,
		"workflowFile": workflowFile,
		"environment":  environment,
	}, &resp)
	if err != nil {
		return nil, err
	}
	return &Run{ID: resp.ID, Status: resp.Status}, nil
}

// ApproveGate approves a pending gate step.
func (c *Client) ApproveGate(ctx context.Context, runID, stepName string) error {
	return c.post(ctx, fmt.Sprintf("/api/v1/runs/%s/gates/%s/approve", runID, stepName),
		map[string]string{"stepName": stepName}, nil)
}

// RejectGate rejects a pending gate step.
func (c *Client) RejectGate(ctx context.Context, runID, stepName, reason string) error {
	return c.post(ctx, fmt.Sprintf("/api/v1/runs/%s/gates/%s/reject", runID, stepName),
		map[string]string{"stepName": stepName, "reason": reason}, nil)
}

// ListPendingGates fetches gates awaiting approval.
func (c *Client) ListPendingGates(ctx context.Context) ([]PendingGate, error) {
	var resp struct {
		Items []PendingGate `json:"items"`
	}
	err := c.get(ctx, "/api/v1/gates?status=pending", &resp)
	return resp.Items, err
}

// LogLine is one log line from the server's log store.
type LogLine struct {
	Timestamp time.Time `json:"timestamp"`
	Stream    string    `json:"stream"`
	Content   string    `json:"content"`
}

// GetStepLogs fetches the stored logs for a step; complete reports whether the
// step has reached a terminal state.
func (c *Client) GetStepLogs(ctx context.Context, runID, stepName string) (lines []LogLine, complete bool, err error) {
	var resp struct {
		Lines    []LogLine `json:"lines"`
		Complete bool      `json:"complete"`
	}
	err = c.get(ctx, fmt.Sprintf("/api/v1/runs/%s/logs/%s", runID, stepName), &resp)
	return resp.Lines, resp.Complete, err
}

// GetRunLogs fetches the combined per-step logs for a run (step name → text).
func (c *Client) GetRunLogs(ctx context.Context, runID string) (map[string]string, error) {
	var resp struct {
		Logs map[string]string `json:"logs"`
	}
	err := c.get(ctx, fmt.Sprintf("/api/v1/runs/%s/logs", runID), &resp)
	return resp.Logs, err
}

// StreamStepLogs follows a step's logs via SSE, writing content lines to w
// until the step completes or ctx is cancelled.
func (c *Client) StreamStepLogs(ctx context.Context, runID, stepName string, w io.Writer) error {
	// SSE cannot carry an Authorization header from EventSource clients, so
	// the server accepts ?access_token= for stream endpoints.
	url := fmt.Sprintf("%s/api/v1/runs/%s/logs/%s/stream", c.baseURL, runID, stepName)
	if c.creds != nil {
		url += "?access_token=" + c.creds.Token
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	// No client timeout: the stream lives until the step completes.
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("API %d: %s", resp.StatusCode, apiErrorMessage(raw))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	event, data := "", ""
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		case line == "":
			if data == "" {
				continue
			}
			switch event {
			case "log":
				var lines []LogLine
				if json.Unmarshal([]byte(data), &lines) == nil {
					for _, l := range lines {
						fmt.Fprintln(w, l.Content)
					}
				}
			case "done":
				return nil
			}
			event, data = "", ""
		}
	}
	return scanner.Err()
}
