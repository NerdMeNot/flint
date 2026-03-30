package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client is a typed HTTP client for the Flint server API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a TUI API client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 10 * 1e9}, // 10s
	}
}

func (c *Client) get(ctx context.Context, path string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API %d: %s", resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

func (c *Client) post(ctx context.Context, path string, body any, result any) error {
	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path,
		strings.NewReader(string(jsonBody)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API %d: %s", resp.StatusCode, string(body))
	}

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// ListProjects fetches all projects.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	var resp struct {
		Projects []Project `json:"projects"`
	}
	err := c.get(ctx, "/api/v1/projects", &resp)
	return resp.Projects, err
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
func (c *Client) TriggerRun(ctx context.Context, projectID, branch, workflowFile string) (*Run, error) {
	var resp struct {
		RunID string `json:"runID"`
	}
	err := c.post(ctx, "/api/v1/runs", map[string]string{
		"projectId":    projectID,
		"branch":       branch,
		"workflowFile": workflowFile,
	}, &resp)
	if err != nil {
		return nil, err
	}
	return &Run{ID: resp.RunID, Status: "pending"}, nil
}

// ApproveGate approves a pending gate step.
func (c *Client) ApproveGate(ctx context.Context, runID, stepName string) error {
	return c.post(ctx, fmt.Sprintf("/api/v1/runs/%s/approve", runID),
		map[string]string{"stepName": stepName}, nil)
}
