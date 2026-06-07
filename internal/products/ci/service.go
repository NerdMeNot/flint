package ci

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"

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
		Kind:       string(event.Kind),
		Branch:     event.Branch,
		BaseBranch: event.BaseBranch,
		Tag:        event.Tag,
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
		if perr != nil {
			logger.Warn().Err(perr).Str("file", file).Msg("ci: parse pipeline failed (skipping)")
			continue
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
				logger.Error().Err(err).Str("file", file).Str("env", env).Msg("ci: start failed")
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
	waves, err := Compile(p, env)
	if err != nil {
		return "", fmt.Errorf("compile pipeline: %w", err)
	}
	return s.engine.StartWorkflowWithWaves(ctx, input, waves)
}

func (s *Service) fetchPipeline(ctx context.Context, repo, ref, pipelinePath, file string) (*Pipeline, error) {
	raw, err := s.forge.GetFile(ctx, repo, ref, path.Join(pipelinePath, file))
	if err != nil {
		return nil, fmt.Errorf("fetch pipeline %q: %w", file, err)
	}
	return Parse(raw)
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

// reportQueued posts a pending commit status to the forge (best-effort, async).
// This used to live in the engine; it lives here so the engine stays neutral.
func (s *Service) reportQueued(repo, sha, file string) {
	if s.forge == nil || sha == "" {
		return
	}
	go func() {
		_ = s.forge.PostCommitStatus(context.Background(), repo, sha, forge.CommitStatus{
			State:       forge.StatusPending,
			Context:     fmt.Sprintf("flint/%s", file),
			Description: "Flint pipeline queued",
		})
	}()
}

func (s *Service) failRun(ctx context.Context, runID string, cause error) {
	msg := cause.Error()
	_ = s.q.FailRunWithError(ctx, db.FailRunWithErrorParams{ID: runID, ErrorMessage: &msg})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
