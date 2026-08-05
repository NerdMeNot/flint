package server

// Contract test: validates that the live Go REST API returns the fields the web
// UI requires. The required-field expectations mirror web/src/lib/api/types.ts —
// update both together when the contract changes. See
// docs/design/api-compatibility.md.
//
// Skipped unless FLINT_CONTRACT_URL points at a running server (e.g. a demo-mode
// server started with `flint server --demo`). Optional FLINT_CONTRACT_TOKEN is
// sent as a Bearer token. Once demo mode (Phase 3) lands this runs in CI and
// guards against UI/server drift.
//
//	FLINT_CONTRACT_URL=http://localhost:5000 go test ./internal/platform/server -run TestContract

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

func contractClient(t *testing.T) (string, func(path string) map[string]any) {
	t.Helper()
	base := os.Getenv("FLINT_CONTRACT_URL")
	if base == "" {
		t.Skip("set FLINT_CONTRACT_URL to a running server (e.g. flint server --demo) to run the contract test")
	}
	token := os.Getenv("FLINT_CONTRACT_TOKEN")
	hc := &http.Client{Timeout: 10 * time.Second}

	get := func(path string) map[string]any {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, base+"/api/v1"+path, nil)
		if err != nil {
			t.Fatalf("%s: build request: %v", path, err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := hc.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d: %s", path, res.StatusCode, string(body))
		}
		var out map[string]any
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("%s: decode: %v (body=%s)", path, err, string(body))
		}
		return out
	}
	return base, get
}

// requireFields asserts every key is present (non-absent) on the object.
func requireFields(t *testing.T, where string, obj map[string]any, fields ...string) {
	t.Helper()
	for _, f := range fields {
		if _, ok := obj[f]; !ok {
			t.Errorf("%s: missing required field %q (present: %v)", where, f, keysOf(obj))
		}
	}
}

func keysOf(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func firstItem(t *testing.T, where string, page map[string]any) (map[string]any, bool) {
	t.Helper()
	items, ok := page["items"].([]any)
	if !ok {
		t.Errorf("%s: expected paginated {items,nextCursor}, got keys %v", where, keysOf(page))
		return nil, false
	}
	if len(items) == 0 {
		return nil, false
	}
	obj, ok := items[0].(map[string]any)
	if !ok {
		t.Errorf("%s: items[0] not an object", where)
		return nil, false
	}
	return obj, true
}

func TestContract(t *testing.T) {
	_, get := contractClient(t)

	t.Run("stats", func(t *testing.T) {
		s := get("/stats")
		requireFields(t, "stats", s, "totalRuns", "successRate", "pendingGates", "activeProjects", "runsToday", "avgDuration")
	})

	t.Run("projects", func(t *testing.T) {
		page := get("/projects")
		if p, ok := firstItem(t, "projects", page); ok {
			requireFields(t, "project", p,
				"id", "name", "repo", "workspace", "colour", "tags",
				"pipelineCount", "pipelineErrors", "health")
		}
	})

	t.Run("runs", func(t *testing.T) {
		page := get("/runs")
		run, ok := firstItem(t, "runs", page)
		if !ok {
			t.Skip("no runs to validate")
		}
		requireFields(t, "run", run,
			"id", "projectId", "projectName", "projectColour", "status", "branch",
			"commitSha", "commitMessage", "triggeredBy", "triggerType", "duration",
			"startedAt", "startedAtTs", "workflowFile", "steps")

		id, _ := run["id"].(string)
		if id == "" {
			t.Fatal("run id empty")
		}

		t.Run("steps", func(t *testing.T) {
			s := get(fmt.Sprintf("/runs/%s/steps", id))
			requireFields(t, "runs.steps", s, "steps", "dagWaves")
			if steps, ok := s["steps"].([]any); ok && len(steps) > 0 {
				if st, ok := steps[0].(map[string]any); ok {
					requireFields(t, "step", st,
						"name", "status", "execType", "wave", "attempt", "maxAttempts", "scheduledAt")
				}
			}
		})

		t.Run("combined-logs", func(t *testing.T) {
			l := get(fmt.Sprintf("/runs/%s/logs", id))
			requireFields(t, "runs.logs", l, "logs")
		})
	})

	t.Run("gates", func(t *testing.T) {
		page := get("/gates")
		if g, ok := firstItem(t, "gates", page); ok {
			requireFields(t, "gate", g,
				"runId", "stepName", "status", "message", "projectName",
				"projectColour", "workspace", "environment", "branch", "triggeredBy", "createdAt")
		}
	})
}
