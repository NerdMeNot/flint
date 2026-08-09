package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// insights_handlers.go — queue visibility, status badges, and cost-per-run.
// Cost is a first-class concept: Flint schedules the pods, so it knows exactly
// what compute each step requested for how long.

// handleQueueStats reports queue depth, wait time, and current load.
// GET /api/v1/queue
func (s *Server) handleQueueStats(ctx context.Context, c *app.RequestContext) {
	stats, err := s.deps.Q.QueueStats(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to read queue stats")
		return
	}
	c.JSON(consts.StatusOK, utils.H{
		"queued":           stats.Queued,
		"running":          stats.Running,
		"waitingGates":     stats.WaitingGates,
		"oldestQueuedSecs": stats.OldestQueuedSecs,
	})
}

// handleStatusBadge renders an SVG status badge for a project's latest run.
// Public (no auth): badge URLs live in READMEs.
// GET /badges/:project.svg?branch=main
func (s *Server) handleStatusBadge(ctx context.Context, c *app.RequestContext) {
	projectID := strings.TrimSuffix(c.Param("project"), ".svg")
	branch := queryString(c, "branch")

	status, err := s.deps.Q.LatestRunStatusForProject(ctx, db.LatestRunStatusForProjectParams{
		ProjectID: &projectID,
		Branch:    branch,
	})
	if err != nil {
		status = "unknown"
	}

	label := status
	color := "#9f9f9f" // unknown gray
	switch status {
	case "succeeded":
		label, color = "passing", "#3fb950"
	case "failed":
		label, color = "failing", "#f85149"
	case "running", "pending":
		label, color = "running", "#58a6ff"
	case "cancelled":
		label, color = "cancelled", "#8b949e"
	}

	c.Response.Header.SetContentType("image/svg+xml")
	c.Response.Header.Set("Cache-Control", "no-cache, max-age=60")
	c.SetStatusCode(consts.StatusOK)
	c.Response.SetBodyString(renderBadgeSVG("flint", label, color))
}

// renderBadgeSVG produces a shields-style two-segment badge.
func renderBadgeSVG(name, value, color string) string {
	// ~6.5px per char at 11px Verdana + padding — close enough for a badge.
	nameW := len(name)*7 + 12
	valueW := len(value)*7 + 12
	total := nameW + valueW
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="%s: %s">
<linearGradient id="s" x2="0" y2="100%%"><stop offset="0" stop-color="#bbb" stop-opacity=".1"/><stop offset="1" stop-opacity=".1"/></linearGradient>
<clipPath id="r"><rect width="%d" height="20" rx="3" fill="#fff"/></clipPath>
<g clip-path="url(#r)">
<rect width="%d" height="20" fill="#555"/>
<rect x="%d" width="%d" height="20" fill="%s"/>
<rect width="%d" height="20" fill="url(#s)"/>
</g>
<g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" font-size="11">
<text x="%d" y="14">%s</text>
<text x="%d" y="14">%s</text>
</g>
</svg>`,
		total, name, value,
		total,
		nameW,
		nameW, valueW, color,
		total,
		nameW/2, name,
		nameW+valueW/2, value)
}

// stepResources mirrors the compiled step's resources JSON.
type stepResources struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

// handleRunCost breaks a run's compute down per step: requested cpu/memory ×
// wall-clock duration, with a dollar estimate from the configured rates.
// GET /api/v1/runs/:id/cost
func (s *Server) handleRunCost(ctx context.Context, c *app.RequestContext) {
	runID := c.Param("id")
	rows, err := s.deps.Q.RunStepCosts(ctx, runID)
	if err != nil {
		apiNotFound(ctx, c, "run not found")
		return
	}

	cpuRate := s.deps.Config.Costs.CPUCoreHourOrDefault()
	memRate := s.deps.Config.Costs.MemoryGBHourOrDefault()

	type stepCost struct {
		Name         string  `json:"name"`
		DurationSecs float64 `json:"durationSecs"`
		CPUCores     float64 `json:"cpuCores"`
		MemoryGB     float64 `json:"memoryGb"`
		CoreSecs     float64 `json:"coreSecs"`
		GBSecs       float64 `json:"gbSecs"`
		EstimatedUSD float64 `json:"estimatedUsd"`
	}
	steps := make([]stepCost, 0, len(rows))
	var totalUSD, totalCoreSecs, totalGBSecs float64

	for _, row := range rows {
		// Only container steps consume compute.
		if row.ExecType != "run" && row.ExecType != "use" && row.ExecType != "steps" {
			continue
		}
		if row.StartedAt == nil {
			continue
		}
		end := time.Now()
		if row.FinishedAt != nil {
			end = *row.FinishedAt
		}
		dur := end.Sub(*row.StartedAt).Seconds()
		if dur < 0 {
			dur = 0
		}

		// Requested resources; the dispatch defaults when the step didn't ask.
		cpu, mem := 0.5, 0.5 // 500m / 512Mi
		var res stepResources
		if raw, ok := row.Resources.([]byte); ok && len(raw) > 0 {
			if json.Unmarshal(raw, &res) == nil {
				if v, err := parseCPU(res.CPU); err == nil && v > 0 {
					cpu = v
				}
				if v, err := parseMemoryGB(res.Memory); err == nil && v > 0 {
					mem = v
				}
			}
		}

		coreSecs := cpu * dur
		gbSecs := mem * dur
		cost := coreSecs/3600*cpuRate + gbSecs/3600*memRate
		totalUSD += cost
		totalCoreSecs += coreSecs
		totalGBSecs += gbSecs

		steps = append(steps, stepCost{
			Name: row.Name, DurationSecs: round2(dur),
			CPUCores: cpu, MemoryGB: round2(mem),
			CoreSecs: round2(coreSecs), GBSecs: round2(gbSecs),
			EstimatedUSD: round6(cost),
		})
	}

	c.JSON(consts.StatusOK, utils.H{
		"steps":         steps,
		"totalCoreSecs": round2(totalCoreSecs),
		"totalGbSecs":   round2(totalGBSecs),
		"estimatedUsd":  round6(totalUSD),
		"rates": utils.H{
			"cpuCoreHourUsd": cpuRate,
			"memoryGbHrUsd":  memRate,
		},
	})
}

// parseCPU parses a Kubernetes CPU quantity ("500m", "2") into cores.
func parseCPU(s string) (float64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	if strings.HasSuffix(s, "m") {
		var milli float64
		if _, err := fmt.Sscanf(s, "%fm", &milli); err != nil {
			return 0, err
		}
		return milli / 1000, nil
	}
	var cores float64
	_, err := fmt.Sscanf(s, "%f", &cores)
	return cores, err
}

// parseMemoryGB parses a Kubernetes memory quantity ("512Mi", "4Gi") into GB.
func parseMemoryGB(s string) (float64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	units := []struct {
		suffix string
		gb     float64
	}{
		{"Gi", 1.074}, {"Mi", 0.001049}, {"Ki", 0.000001},
		{"G", 1}, {"M", 0.001}, {"K", 0.000001},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			var v float64
			if _, err := fmt.Sscanf(strings.TrimSuffix(s, u.suffix), "%f", &v); err != nil {
				return 0, err
			}
			return v * u.gb, nil
		}
	}
	var bytes float64
	if _, err := fmt.Sscanf(s, "%f", &bytes); err != nil {
		return 0, err
	}
	return bytes / 1e9, nil
}

func round2(v float64) float64 { return float64(int(v*100+0.5)) / 100 }
func round6(v float64) float64 { return float64(int(v*1e6+0.5)) / 1e6 }
