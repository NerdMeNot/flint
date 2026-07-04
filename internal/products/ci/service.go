package ci

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/pkg/forge"
	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// Service owns CI run creation: it turns forge webhooks, manual triggers, and
// re-runs into pipeline_runs and engine workflows. It fetches the pipeline from
// the forge, parses + compiles it (this package), and starts it on the engine
// via StartWorkflowWithWaves — the engine never touches the forge or YAML.
//
// It depends only on core (engine, db, observe) and pkg (forge, pipeline); the
// platform server reaches it through an interface, so platform never imports
// this package.
type Service struct {
	engine engine.Engine
	forge  forge.ForgeProvider
	q      db.Querier

	// Dedupe for completion reporting (see ReportRunFinished).
	reportedMu sync.Mutex
	reported   map[string]bool
}

// NewService builds the CI run-creation service.
func NewService(eng engine.Engine, fg forge.ForgeProvider, q db.Querier) *Service {
	return &Service{engine: eng, forge: fg, q: q}
}

// HandleWebhook processes a forge webhook: it validates the signature, finds the
// project, discovers and evaluates pipeline files, and starts one run per
// matching (file, environment). Returns the created run IDs.
func (s *Service) HandleWebhook(ctx context.Context, headers http.Header, body []byte, forgeType string) ([]string, error) {
	logger := observe.Logger(ctx)

	secret, _ := s.q.GetWebhookSecret(ctx, forgeType)
	if secret == "" {
		return nil, fmt.Errorf("no forge connection configured")
	}
	event, err := s.forge.ParseWebhook(headers, body, secret)
	if err != nil {
		observe.WebhooksInvalid.Add(ctx, 1)
		return nil, fmt.Errorf("invalid webhook: %w", err)
	}
	observe.WebhooksReceived.Add(ctx, 1)

	proj, err := s.q.GetProjectByRepoPath(ctx, event.Repo)
	if err != nil {
		return nil, fmt.Errorf("no project for repo %q", event.Repo)
	}
	pipelinePath := ".flint/"
	if pp, ok := proj.PipelinePath.(string); ok && pp != "" {
		pipelinePath = pp
	}

	triggerEvent := pipeline.TriggerEvent{
		Kind:         string(event.Kind),
		Branch:       event.Branch,
		BaseBranch:   event.BaseBranch,
		Tag:          event.Tag,
		ChangedFiles: event.ChangedFiles,
	}

	// PR payloads don't carry file lists — fetch lazily, once, only when a
	// pipeline actually filters by paths. Unknown (nil) fails open in the
	// matcher, so a fetch failure degrades to "run it" rather than "lose it".
	prFilesFetched := false
	ensurePRFiles := func() {
		if prFilesFetched || event.Kind != forge.EventPullRequest || triggerEvent.ChangedFiles != nil {
			return
		}
		prFilesFetched = true
		files, ferr := s.forge.ListPullRequestFiles(ctx, event.Repo, event.PRNumber)
		if ferr != nil {
			logger.Warn().Err(ferr).Int("pr", event.PRNumber).Msg("ci: changed-files fetch failed (paths filters fail open)")
			return
		}
		triggerEvent.ChangedFiles = files
	}
	ref := event.Branch
	if event.Tag != "" {
		ref = event.Tag
	}

	baseRunID := observe.RequestID(ctx)
	var runIDs []string
	counter := 0

	for _, file := range s.discoverPipelineFiles(ctx, event.Repo, event.CommitSHA, pipelinePath) {
		raw, ferr := s.forge.GetFile(ctx, event.Repo, event.CommitSHA, path.Join(pipelinePath, file))
		if ferr != nil {
			logger.Warn().Err(ferr).Str("file", file).Msg("ci: fetch pipeline failed (skipping)")
			continue
		}
		p, perr := Parse(raw)
		if perr == nil {
			p, perr = s.resolvePipeline(ctx, p, event.Repo, event.CommitSHA)
		}
		if perr != nil {
			// The author must see this on their commit — a broken pipeline
			// that silently never runs is indistinguishable from CI being down.
			logger.Warn().Err(perr).Str("file", file).Msg("ci: parse pipeline failed")
			s.reportBroken(event.Repo, event.CommitSHA, file, perr)
			continue
		}
		if t := p.Triggers.PullRequest; t != nil && len(t.Paths) > 0 {
			ensurePRFiles()
		}
		matches := pipeline.MatchTriggers(&pipeline.Pipeline{Triggers: p.Triggers}, triggerEvent)
		if len(matches) == 0 {
			continue
		}
		for _, env := range pipeline.CollectEnvironments(matches) {
			counter++
			runID := baseRunID
			if counter > 1 {
				runID = fmt.Sprintf("%s-%d", baseRunID, counter)
			}
			var envPtr *string
			if env != "" {
				e := env
				envPtr = &e
			}
			pid, wf := proj.ID, file
			if err := s.q.InsertPipelineRun(ctx, db.InsertPipelineRunParams{
				ID: runID, ProjectID: &pid, OrgID: proj.OrgID, WorkflowFile: &wf,
				TriggerType: string(event.Kind), TriggerRef: &ref,
				CommitSha: &event.CommitSHA, CommitMessage: &event.Message,
				TriggeredBy: &event.Sender, Environment: envPtr,
			}); err != nil {
				logger.Error().Err(err).Str("file", file).Msg("ci: insert run failed")
				continue
			}
			if err := s.start(ctx, p, env, engine.StartWorkflowInput{
				RunID: runID, OrgID: proj.OrgID, ProjectID: proj.ID,
				Repo: event.Repo, Ref: ref, CommitSHA: event.CommitSHA,
				TriggerType: string(event.Kind), TriggeredBy: event.Sender, Environment: env,
			}); err != nil {
				// The run row exists — mark it failed (not queued-forever) and
				// surface the compile/pool error on the commit.
				logger.Error().Err(err).Str("file", file).Str("env", env).Msg("ci: start failed")
				s.failRun(ctx, runID, err)
				s.reportBroken(event.Repo, event.CommitSHA, file, err)
				continue
			}
			s.reportQueued(event.Repo, event.CommitSHA, file)
			runIDs = append(runIDs, runID)
		}
	}
	return runIDs, nil
}

// TriggerManual starts a run from a manual/API trigger for a project.
func (s *Service) TriggerManual(ctx context.Context, projectID, branch, workflowFile, environment string) (runID, workflowID string, err error) {
	if workflowFile == "" {
		workflowFile = "ci.yaml"
	}
	if branch == "" {
		branch = "main"
	}
	info, err := s.q.GetProjectRepoInfo(ctx, projectID)
	if err != nil {
		return "", "", fmt.Errorf("project not found")
	}
	pipelinePath := info.PipelinePath
	if pipelinePath == "" {
		pipelinePath = ".flint/"
	}
	runID = observe.RequestID(ctx)
	var envPtr *string
	if environment != "" {
		environment := environment
		envPtr = &environment
	}
	if err := s.q.InsertManualRun(ctx, db.InsertManualRunParams{
		ID: runID, ProjectID: &projectID, OrgID: info.OrgID,
		WorkflowFile: &workflowFile, TriggerRef: &branch, Environment: envPtr,
	}); err != nil {
		return "", "", fmt.Errorf("failed to create run")
	}

	p, err := s.fetchPipeline(ctx, info.RepoPath, branch, pipelinePath, workflowFile)
	if err != nil {
		s.failRun(ctx, runID, err)
		return runID, "", err
	}
	wfID, err := s.startResolved(ctx, p, environment, engine.StartWorkflowInput{
		RunID: runID, OrgID: info.OrgID, ProjectID: projectID,
		Repo: info.RepoPath, Ref: branch, TriggerType: "manual", TriggeredBy: "api",
		Environment: environment,
	})
	if err != nil {
		s.failRun(ctx, runID, err)
		return runID, "", err
	}
	return runID, wfID, nil
}

// Rerun re-runs an existing CI run from its original commit/ref.
func (s *Service) Rerun(ctx context.Context, runID string) (newRunID, workflowID string, err error) {
	orig, err := s.q.GetOriginalRunParams(ctx, runID)
	if err != nil {
		return "", "", fmt.Errorf("run not found")
	}
	if orig.ProjectID == nil {
		return "", "", fmt.Errorf("run cannot be retried: not a CI run")
	}
	info, err := s.q.GetProjectRepoInfo(ctx, *orig.ProjectID)
	if err != nil {
		return "", "", fmt.Errorf("failed to get project info")
	}
	workflowFile := "ci.yaml"
	if orig.WorkflowFile != nil && *orig.WorkflowFile != "" {
		workflowFile = *orig.WorkflowFile
	}
	ref := deref(orig.TriggerRef)
	sha := deref(orig.CommitSha)
	env := deref(orig.Environment)

	newRunID = observe.RequestID(ctx)
	if err := s.q.InsertRetryRun(ctx, db.InsertRetryRunParams{
		ID: newRunID, ProjectID: orig.ProjectID, OrgID: orig.OrgID,
		WorkflowFile: orig.WorkflowFile, TriggerRef: orig.TriggerRef,
		CommitSha: orig.CommitSha, Environment: orig.Environment,
	}); err != nil {
		return "", "", fmt.Errorf("failed to create retry run")
	}

	fetchRef := ref
	if sha != "" {
		fetchRef = sha
	}
	p, err := s.fetchPipeline(ctx, info.RepoPath, fetchRef, info.PipelinePath, workflowFile)
	if err != nil {
		s.failRun(ctx, newRunID, err)
		return newRunID, "", err
	}
	wfID, err := s.startResolved(ctx, p, env, engine.StartWorkflowInput{
		RunID: newRunID, OrgID: orig.OrgID, ProjectID: *orig.ProjectID,
		Repo: info.RepoPath, Ref: ref, CommitSHA: sha,
		TriggerType: "retry", TriggeredBy: "api", Environment: env,
	})
	if err != nil {
		s.failRun(ctx, newRunID, err)
		return newRunID, "", err
	}
	return newRunID, wfID, nil
}

// ── internals ───────────────────────────────────────────────────────────────

// start compiles an already-parsed pipeline for an env and starts it.
func (s *Service) start(ctx context.Context, p *Pipeline, env string, input engine.StartWorkflowInput) error {
	_, err := s.startResolved(ctx, p, env, input)
	return err
}

func (s *Service) startResolved(ctx context.Context, p *Pipeline, env string, input engine.StartWorkflowInput) (string, error) {
	if s.engine == nil {
		return "", nil
	}
	// Validate runner pools + resource requests against the pool catalog before
	// compiling, so bad compute asks fail here (named fix) not as a Pending pod.
	if s.q != nil {
		if err := p.ValidateWithPools(ctx, s.q); err != nil {
			return "", err
		}
	}
	waves, err := Compile(p, env)
	if err != nil {
		return "", fmt.Errorf("compile pipeline: %w", err)
	}

	// ci-dialect jobs are self-contained pods; files cross jobs via artifacts
	// (object storage), never workspace sync — the engine skips the per-run
	// workspace pod entirely.
	input.WorkspaceFlow = "artifacts"

	// Concurrency: cancel-in-progress. Resolve the group (expressions like
	// "ci-${{ git.branch }}"), stamp it on this run, and cancel still-running
	// runs of the same project+group — the new commit supersedes them.
	if p.Concurrency != nil && p.Concurrency.Group != "" && s.q != nil {
		if err := s.enforceConcurrency(ctx, p.Concurrency, env, input); err != nil {
			return "", err
		}
	}

	return s.engine.StartWorkflowWithWaves(ctx, input, waves)
}

// enforceConcurrency stamps the run's resolved concurrency group and cancels
// superseded running runs in the same group (cancelInProgress semantics —
// queue-mode is rejected at validation).
func (s *Service) enforceConcurrency(ctx context.Context, c *Concurrency, env string, input engine.StartWorkflowInput) error {
	logger := observe.Logger(ctx)

	group, err := pipeline.Interpolate(c.Group, pipeline.ExprContext{
		"git": map[string]any{"sha": input.CommitSHA, "branch": input.Ref, "repoUrl": input.Repo},
		"run": map[string]any{"id": input.RunID, "trigger": input.TriggerType},
		"env": input.Env,
	})
	if err != nil {
		return fmt.Errorf("concurrency group %q: %w", c.Group, err)
	}
	// Environment-scoped runs get distinct groups per environment, otherwise a
	// staging run would cancel the production run of the same commit.
	if env != "" {
		group += "@" + env
	}

	if err := s.q.SetRunConcurrencyGroup(ctx, db.SetRunConcurrencyGroupParams{
		ID: input.RunID, ConcurrencyGroup: &group,
	}); err != nil {
		return fmt.Errorf("set concurrency group: %w", err)
	}
	if !c.CancelInProgress {
		return nil
	}

	pid := input.ProjectID
	superseded, err := s.q.ActiveRunsInConcurrencyGroup(ctx, db.ActiveRunsInConcurrencyGroupParams{
		ProjectID: &pid, ConcurrencyGroup: &group, ID: input.RunID,
	})
	if err != nil {
		logger.Warn().Err(err).Str("group", group).Msg("ci: concurrency lookup failed (continuing)")
		return nil
	}
	for _, r := range superseded {
		if r.WorkflowID == nil {
			continue
		}
		if err := s.engine.CancelWorkflow(ctx, *r.WorkflowID); err != nil {
			logger.Warn().Err(err).Str("run", r.ID).Msg("ci: cancel superseded run failed")
			continue
		}
		logger.Info().Str("run", r.ID).Str("group", group).Msg("ci: cancelled superseded run")
	}
	return nil
}

func (s *Service) fetchPipeline(ctx context.Context, repo, ref, pipelinePath, file string) (*Pipeline, error) {
	raw, err := s.forge.GetFile(ctx, repo, ref, path.Join(pipelinePath, file))
	if err != nil {
		return nil, fmt.Errorf("fetch pipeline %q: %w", file, err)
	}
	p, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	return s.resolvePipeline(ctx, p, repo, ref)
}

// resolvePipeline expands use:/extends: module references (built-ins, in-repo
// files, cross-repo refs) and re-validates the resolved result — module bodies
// defer their checks until this point.
func (s *Service) resolvePipeline(ctx context.Context, p *Pipeline, repo, sha string) (*Pipeline, error) {
	if !hasModuleRefs(p) {
		return p, nil
	}
	resolved, err := ResolveModules(ctx, p, NewForgeResolver(s.forge, repo, sha))
	if err != nil {
		return nil, err
	}
	if err := resolved.Validate(); err != nil {
		return nil, err
	}
	return resolved, nil
}

func (s *Service) discoverPipelineFiles(ctx context.Context, repo, ref, pipelinePath string) []string {
	dir, err := s.forge.GetDirectory(ctx, repo, ref, pipelinePath)
	if err != nil || len(dir) == 0 {
		return []string{"ci.yaml"}
	}
	var files []string
	for name := range dir {
		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			files = append(files, name)
		}
	}
	if len(files) == 0 {
		return []string{"ci.yaml"}
	}
	return files
}

// reportQueued posts a pending status to the forge (best-effort, async): a
// check run when the forge supports it (GitHub App auth), else a commit status.
func (s *Service) reportQueued(repo, sha, file string) {
	if s.forge == nil || sha == "" {
		return
	}
	go func() {
		ctx := context.Background()
		if cr, ok := s.forge.(forge.CheckReporter); ok {
			if err := cr.CreateCheckRun(ctx, repo, sha, forge.CheckRun{
				Name:   fmt.Sprintf("flint/%s", file),
				Status: "queued",
				Title:  "Flint pipeline queued",
			}); err == nil {
				return
			}
		}
		_ = s.forge.PostCommitStatus(ctx, repo, sha, forge.CommitStatus{
			State:       forge.StatusPending,
			Context:     fmt.Sprintf("flint/%s", file),
			Description: "Flint pipeline queued",
		})
	}()
}

// reportBroken posts a failure for a pipeline file that could not be parsed,
// validated, or started (best-effort, async). With check-run support the full
// error lands as a file annotation on the pipeline; the commit-status fallback
// truncates to GitHub's 140-char description cap.
func (s *Service) reportBroken(repo, sha, file string, cause error) {
	if s.forge == nil || sha == "" {
		return
	}
	go func() {
		ctx := context.Background()
		if cr, ok := s.forge.(forge.CheckReporter); ok {
			if err := cr.CreateCheckRun(ctx, repo, sha, forge.CheckRun{
				Name:       fmt.Sprintf("flint/%s", file),
				Status:     "completed",
				Conclusion: "failure",
				Title:      "Pipeline is broken",
				Summary:    cause.Error(),
				Annotations: []forge.CheckAnnotation{{
					Path:    ".flint/" + file,
					Level:   "failure",
					Message: cause.Error(),
				}},
			}); err == nil {
				return
			}
		}
		desc := cause.Error()
		if len(desc) > 140 {
			desc = desc[:137] + "..."
		}
		_ = s.forge.PostCommitStatus(ctx, repo, sha, forge.CommitStatus{
			State:       forge.StatusFailure,
			Context:     fmt.Sprintf("flint/%s", file),
			Description: desc,
		})
	}()
}

func (s *Service) failRun(ctx context.Context, runID string, cause error) {
	msg := cause.Error()
	_ = s.q.FailRunWithError(ctx, db.FailRunWithErrorParams{ID: runID, ErrorMessage: &msg})
}

// ReportRunFinished posts the run's outcome to the forge as a commit status —
// the counterpart of the "queued" status posted at run creation. Without it a
// PR shows "pending" forever. Deduped per (run, status): the state observer
// fires on every post-terminal transition.
func (s *Service) ReportRunFinished(ctx context.Context, runID, status string) {
	if s.forge == nil || s.q == nil {
		return
	}
	key := runID + "/" + status
	s.reportedMu.Lock()
	if s.reported == nil {
		s.reported = make(map[string]bool)
	}
	if s.reported[key] {
		s.reportedMu.Unlock()
		return
	}
	// Bound the dedupe set; it only needs to absorb the burst of post-terminal
	// transitions for recent runs.
	if len(s.reported) > 4096 {
		s.reported = make(map[string]bool)
	}
	s.reported[key] = true
	s.reportedMu.Unlock()

	info, err := s.q.GetRunStatusInfo(ctx, runID)
	if err != nil || info.Repo == nil || info.CommitSha == nil || *info.CommitSha == "" {
		return // not a commit-triggered run (manual/workflow) — nothing to report
	}
	file := "ci.yaml"
	if info.WorkflowFile != nil && *info.WorkflowFile != "" {
		file = *info.WorkflowFile
	}

	state := forge.StatusSuccess
	conclusion := "success"
	desc := "Flint pipeline succeeded"
	switch status {
	case "failed":
		state, conclusion = forge.StatusFailure, "failure"
		desc = "Flint pipeline failed"
	case "cancelled":
		state, conclusion = forge.StatusError, "cancelled"
		desc = "Flint pipeline cancelled"
	}

	repo, sha := *info.Repo, *info.CommitSha
	go func() {
		bg := context.Background()
		if cr, ok := s.forge.(forge.CheckReporter); ok {
			if err := cr.CreateCheckRun(bg, repo, sha, forge.CheckRun{
				Name:       fmt.Sprintf("flint/%s", file),
				Status:     "completed",
				Conclusion: conclusion,
				Title:      desc,
			}); err == nil {
				return
			}
		}
		_ = s.forge.PostCommitStatus(bg, repo, sha, forge.CommitStatus{
			State:       state,
			Context:     fmt.Sprintf("flint/%s", file),
			Description: desc,
		})
	}()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
