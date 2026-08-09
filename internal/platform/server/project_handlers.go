package server

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/internal/core/projectcfg"
	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// pipelineSourceReq is the optional pipeline-source location in create/update.
type pipelineSourceReq struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Repo string `json:"repo"`
	Ref  string `json:"ref"`
}

func (p *pipelineSourceReq) toSpec() *projectcfg.PipelineSource {
	if p == nil {
		return nil
	}
	return &projectcfg.PipelineSource{Type: p.Type, Path: p.Path, Repo: p.Repo, Ref: p.Ref}
}

// handleCreateProject registers a project from the API/UI — the canonical create
// path (the Project CRD is an optional GitOps adapter onto the same row). It
// mirrors the reconciler: org-policy check, upsert via the shared mapper, then
// best-effort provisioning of the inbound forge webhook.
func (s *Server) handleCreateProject(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Repo           string             `json:"repo"`
		ForgeRef       string             `json:"forgeRef"`
		DisplayName    string             `json:"displayName"`
		Description    string             `json:"description"`
		Colour         string             `json:"colour"`
		Icon           string             `json:"icon"`
		Workspace      string             `json:"workspace"`
		DefaultBranch  string             `json:"defaultBranch"`
		PipelineSource *pipelineSourceReq `json:"pipelineSource"`
	}
	if c.BindJSON(&req) != nil || strings.TrimSpace(req.Repo) == "" || strings.TrimSpace(req.ForgeRef) == "" {
		apiBadRequest(ctx, c, "repo and forgeRef are required")
		return
	}

	// Reject a duplicate active project for this repo (an archived one is allowed —
	// the upsert un-archives it).
	if _, err := s.deps.Q.GetProjectByRepoPath(ctx, req.Repo); err == nil {
		apiConflict(ctx, c, "a project for this repo already exists")
		return
	}

	// Org policy: when "require workspace" is on, a project must declare one.
	if org, err := s.deps.Q.GetOrg(ctx); err == nil && org.RequireProjectWorkspace && strings.TrimSpace(req.Workspace) == "" {
		apiBadRequest(ctx, c, "workspace is required by org policy")
		return
	}

	spec := projectcfg.Spec{
		Repo: req.Repo, ForgeRef: req.ForgeRef, DisplayName: req.DisplayName,
		Description: req.Description, Colour: req.Colour, Icon: req.Icon,
		Workspace: req.Workspace, DefaultBranch: req.DefaultBranch,
		PipelineSource: req.PipelineSource.toSpec(),
	}
	projectID, err := s.deps.Q.UpsertProject(ctx, projectcfg.ToUpsertParams(spec))
	if err != nil {
		apiInternal(ctx, c, "failed to create project")
		return
	}

	provisioned := s.provisionForgeWebhook(ctx, projectID, req.Repo, req.ForgeRef)

	c.JSON(consts.StatusCreated, utils.H{
		"id": projectID, "repo": req.Repo, "webhookProvisioned": provisioned,
	})
}

// handleUpdateProject edits a project's metadata (PATCH-merge). Repo and forgeRef
// are immutable — re-registering a different repo is a new project.
func (s *Server) handleUpdateProject(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	cur, err := s.deps.Q.GetProjectConfig(ctx, id)
	if err != nil {
		apiNotFound(ctx, c, "project not found")
		return
	}

	// Pointer fields distinguish "absent" (keep current) from "set to empty".
	var req struct {
		DisplayName    *string            `json:"displayName"`
		Description    *string            `json:"description"`
		Colour         *string            `json:"colour"`
		Icon           *string            `json:"icon"`
		Workspace      *string            `json:"workspace"`
		DefaultBranch  *string            `json:"defaultBranch"`
		PipelineSource *pipelineSourceReq `json:"pipelineSource"`
	}
	if c.BindJSON(&req) != nil {
		apiBadRequest(ctx, c, "invalid request body")
		return
	}

	spec := projectcfg.Spec{
		Repo:          cur.RepoPath,
		ForgeRef:      cur.ForgeRef,
		DisplayName:   pick(req.DisplayName, derefString(cur.DisplayName)),
		Description:   pick(req.Description, derefString(cur.Description)),
		Colour:        pick(req.Colour, cur.Colour),
		Icon:          pick(req.Icon, derefString(cur.Icon)),
		Workspace:     pick(req.Workspace, cur.Workspace),
		DefaultBranch: pick(req.DefaultBranch, cur.DefaultBranch),
	}
	if req.PipelineSource != nil {
		spec.PipelineSource = req.PipelineSource.toSpec()
	} else if len(cur.PipelineSource) > 0 {
		// Keep the existing source as-is (pass through the stored JSON).
		spec.PipelineSource = parsePipelineSource(cur.PipelineSource)
	}

	if _, err := s.deps.Q.UpsertProject(ctx, projectcfg.ToUpsertParams(spec)); err != nil {
		apiInternal(ctx, c, "failed to update project")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"id": id})
}

// handleArchiveProject soft-deletes a project (archived projects don't trigger
// runs) and best-effort removes its inbound forge webhook.
func (s *Server) handleArchiveProject(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	cur, err := s.deps.Q.GetProjectConfig(ctx, id)
	if err != nil {
		apiNotFound(ctx, c, "project not found")
		return
	}
	if err := s.deps.Q.ArchiveProject(ctx, id); err != nil {
		apiInternal(ctx, c, "failed to archive project")
		return
	}
	if s.deps.Forge != nil && cur.ForgeWebhookID != nil && *cur.ForgeWebhookID != "" {
		logger := observe.Logger(ctx)
		if err := s.deps.Forge.DeleteWebhook(ctx, cur.RepoPath, *cur.ForgeWebhookID); err != nil {
			logger.Warn().Err(err).Str("repo", cur.RepoPath).Msg("failed to delete forge webhook")
		}
		_ = s.deps.Q.SetProjectWebhookID(ctx, db.SetProjectWebhookIDParams{ID: id, ForgeWebhookID: nil})
	}
	c.JSON(consts.StatusOK, utils.H{"success": true})
}

// listArchivedProjects returns archived projects (flat) for the admin page.
func (s *Server) listArchivedProjects(ctx context.Context, c *app.RequestContext) {
	rows, err := s.deps.Q.ListArchivedProjects(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to list archived projects")
		return
	}
	items := make([]utils.H, 0, len(rows))
	for _, r := range rows {
		items = append(items, utils.H{
			"id": r.ID, "name": r.Name, "repo": r.RepoPath,
			"workspace": r.Workspace, "colour": r.Colour,
			"createdAt": r.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	c.JSON(consts.StatusOK, utils.H{"items": items})
}

// handleRestoreProject un-archives a project.
func (s *Server) handleRestoreProject(ctx context.Context, c *app.RequestContext) {
	if err := s.deps.Q.RestoreProject(ctx, c.Param("id")); err != nil {
		apiInternal(ctx, c, "failed to restore project")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true})
}

// provisionForgeWebhook registers the inbound webhook on the forge repo so pushes
// arrive, and records its id. Best-effort: returns false (never fails the caller)
// when the forge/base-URL/secret aren't configured or the call errors.
func (s *Server) provisionForgeWebhook(ctx context.Context, projectID, repo, forgeRef string) bool {
	if s.deps.Forge == nil || s.deps.Config == nil || s.deps.Config.Server.BaseURL == "" {
		return false
	}
	secret, _ := s.deps.Q.GetWebhookSecretByName(ctx, forgeRef)
	if secret == "" {
		return false
	}
	logger := observe.Logger(ctx)
	target := s.deps.Config.Server.BaseURL + "/webhooks/" + s.deps.Forge.Type()
	whID, err := s.deps.Forge.CreateWebhook(ctx, repo, target, secret, []string{"push", "pull_request"})
	if err != nil {
		logger.Warn().Err(err).Str("repo", repo).Msg("failed to provision forge webhook")
		return false
	}
	if err := s.deps.Q.SetProjectWebhookID(ctx, db.SetProjectWebhookIDParams{ID: projectID, ForgeWebhookID: &whID}); err != nil {
		logger.Warn().Err(err).Str("projectID", projectID).Msg("failed to persist webhook id")
	}
	return true
}

// pick returns *p when provided, else the fallback.
func pick(p *string, fallback string) string {
	if p != nil {
		return *p
	}
	return fallback
}

// parsePipelineSource decodes a stored pipeline_source JSON blob into the spec
// shape so an update preserves it unchanged.
func parsePipelineSource(raw []byte) *projectcfg.PipelineSource {
	var ps struct {
		Type string `json:"type"`
		Path string `json:"path"`
		Repo string `json:"repo"`
		Ref  string `json:"ref"`
	}
	if err := json.Unmarshal(raw, &ps); err != nil {
		return nil
	}
	return &projectcfg.PipelineSource{Type: ps.Type, Path: ps.Path, Repo: ps.Repo, Ref: ps.Ref}
}

func (s *Server) listProjects(ctx context.Context, c *app.RequestContext) {
	// Admin "archived" view (?archived=true) — a flat list for the settings page,
	// kept on this route to avoid a /projects/:id wildcard conflict.
	if string(c.Query("archived")) == "true" {
		s.listArchivedProjects(ctx, c)
		return
	}

	// Optional server-side filters (repeated query params): ?workspace=slug&tags=key:value
	// Empty slice = no filter for that dimension.
	workspaces := queryStrings(c, "workspace")
	tags := queryStrings(c, "tags")
	needsGrouping := string(c.Query("needsGrouping")) == "true"

	rows, err := s.deps.Q.ListProjectsWithLastRun(ctx, db.ListProjectsWithLastRunParams{
		Workspaces:    workspaces,
		Tags:          tags,
		NeedsGrouping: needsGrouping,
	})
	if err != nil {
		apiInternal(ctx, c, "failed to list projects")
		return
	}

	// Per-project health (recent run outcomes), keyed by project id.
	health := map[string]*projectHealth{}
	if org, oerr := s.deps.Q.GetOrg(ctx); oerr == nil {
		if hrows, herr := s.deps.Q.ProjectHealthByOrg(ctx, org.ID); herr == nil {
			for _, h := range hrows {
				health[h.ProjectID] = buildProjectHealth(h.RecentStatuses, h.TotalRuns, h.SucceededRuns)
			}
		}
	}

	result := make([]projectResponse, 0, len(rows))
	for _, row := range rows {
		p := projectResponse{
			ID:        row.ID,
			Name:      row.Name,
			Repo:      row.RepoPath,
			Workspace: row.Workspace,
			Colour:    row.Colour,
			Tags:      row.Tags,
			Health:    health[row.ID],
			CreatedAt: row.CreatedAt.Format(time.RFC3339),
			Inferred:  row.Inferred,
		}
		if p.Tags == nil {
			p.Tags = []string{}
		}

		if row.LastRunID != "" {
			duration := "0s"
			if row.LastRunDurationMs.Valid {
				duration = formatDuration(row.LastRunDurationMs.Int32)
			}
			startedAt := ""
			if !row.LastRunStartedAt.IsZero() {
				startedAt = row.LastRunStartedAt.Format(time.RFC3339)
			}
			p.LastRun = &lastRunResponse{
				ID:          row.LastRunID,
				Status:      row.LastRunStatus,
				Branch:      derefString(row.LastRunBranch),
				Duration:    duration,
				TriggeredBy: derefString(row.LastRunTriggeredBy),
				StartedAt:   startedAt,
			}
		}

		result = append(result, p)
	}

	c.JSON(consts.StatusOK, utils.H{"items": result})
}

// handleSetProjectTags replaces a project's tags. Tags are constrained to the
// curated registry: only declared key:value pairs are accepted; anything else
// (free tags, undeclared values) is dropped, so projects can only carry
// predeclared tags and legacy free tags normalize away on the next edit.
func (s *Server) handleSetProjectTags(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	var req struct {
		Tags []string `json:"tags"`
	}
	if err := c.BindJSON(&req); err != nil {
		apiBadRequest(ctx, c, "invalid request body")
		return
	}

	// Build the set of allowed "key:value" tags from the registry.
	allowed := map[string]bool{}
	if org, err := s.deps.Q.GetOrg(ctx); err == nil {
		if keys, err := s.deps.Q.ListTagKeys(ctx, org.ID); err == nil {
			for _, k := range keys {
				for _, v := range k.AllowedValues {
					allowed[k.Key+":"+v] = true
				}
			}
		}
	}
	tags := make([]string, 0, len(req.Tags))
	seen := map[string]bool{}
	for _, t := range req.Tags {
		if allowed[t] && !seen[t] {
			tags = append(tags, t)
			seen[t] = true
		}
	}

	if err := s.deps.Q.UpdateProjectTags(ctx, db.UpdateProjectTagsParams{ID: id, Tags: tags}); err != nil {
		apiInternal(ctx, c, "failed to update tags")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true, "tags": tags})
}

func (s *Server) getProject(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")

	row, err := s.deps.Q.GetProjectBasic(ctx, id)
	if err != nil {
		apiNotFound(ctx, c, "project not found")
		return
	}

	p := projectResponse{
		ID:        row.ID,
		Name:      row.Name,
		Repo:      row.RepoPath,
		Workspace: row.Workspace,
		Colour:    row.Colour,
		Tags:      row.Tags,
		CreatedAt: row.CreatedAt.Format(time.RFC3339),
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}

	// Most recent run for the project (separate query keeps nullability clean).
	if runs, _ := s.deps.Q.ListRunsByProject(ctx, db.ListRunsByProjectParams{ProjectID: &id, Limit: 1}); len(runs) > 0 {
		lr := runs[0]
		p.LastRun = &lastRunResponse{
			ID:          lr.ID,
			Status:      lr.Status,
			Branch:      derefString(lr.TriggerRef),
			Duration:    durationFromInt4(lr.DurationMs),
			TriggeredBy: derefString(lr.TriggeredBy),
			StartedAt:   lr.StartedAt.Format(time.RFC3339),
		}
	}

	if h, herr := s.deps.Q.ProjectHealthByID(ctx, &id); herr == nil {
		p.Health = buildProjectHealth(h.RecentStatuses, h.TotalRuns, h.SucceededRuns)
	}

	c.JSON(consts.StatusOK, p)
}

func (s *Server) handleListProjectPipelines(ctx context.Context, c *app.RequestContext) {
	projectID := c.Param("id")

	info, err := s.deps.Q.GetProjectRepoInfo(ctx, projectID)
	if err != nil {
		apiNotFound(ctx, c, "project not found")
		return
	}

	// Fetch pipeline files from the forge.
	files, err := s.deps.Forge.GetDirectory(ctx, info.RepoPath, "HEAD", info.PipelinePath)
	if err != nil {
		// Forge unavailable or no pipeline dir — return empty.
		c.JSON(consts.StatusOK, utils.H{"items": []any{}})
		return
	}

	type pipelineItem struct {
		Filename string   `json:"filename"`
		Yaml     string   `json:"yaml"`
		Status   string   `json:"status"`
		Errors   []string `json:"errors,omitempty"`
		Steps    []any    `json:"steps"`
	}

	var items []pipelineItem
	for name, content := range files {
		if !isYAMLFile(name) {
			continue
		}

		item := pipelineItem{
			Filename: name,
			Yaml:     string(content), // raw source for the YAML tab
			Status:   "valid",
			Steps:    []any{},
		}

		p, parseErr := pipeline.Parse(content)
		if parseErr != nil {
			item.Status = "invalid"
			item.Errors = []string{parseErr.Error()}
		} else {
			for _, step := range p.Steps {
				item.Steps = append(item.Steps, map[string]any{
					"name":     step.Name,
					"execType": step.ExecType(),
					"wave":     0,
				})
			}
		}

		items = append(items, item)
	}

	if items == nil {
		items = []pipelineItem{}
	}
	c.JSON(consts.StatusOK, utils.H{"items": items})
}
