package ci

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// RerunFailed re-runs a finished run, carrying over every step that already
// succeeded and re-executing only the steps that did not succeed plus everything
// transitively downstream of them. This is the common "re-run failed jobs" CI UX.
func (s *Service) RerunFailed(ctx context.Context, runID string) (newRunID, workflowID string, err error) {
	return s.rerunSeeded(ctx, runID, func(prior []engine.StepState) (map[string]bool, error) {
		triggers := map[string]bool{}
		for _, st := range prior {
			if st.Status != "succeeded" {
				triggers[st.Name] = true
			}
		}
		if len(triggers) == 0 {
			return nil, fmt.Errorf("nothing to re-run: all steps succeeded")
		}
		return triggers, nil
	})
}

// RetryFromStep re-runs a finished run starting at stepName: that step and every
// step downstream of it re-execute, while everything upstream is carried over from
// the prior run's results.
func (s *Service) RetryFromStep(ctx context.Context, runID, stepName string) (newRunID, workflowID string, err error) {
	return s.rerunSeeded(ctx, runID, func(prior []engine.StepState) (map[string]bool, error) {
		for _, st := range prior {
			if st.Name == stepName {
				return map[string]bool{stepName: true}, nil
			}
		}
		return nil, fmt.Errorf("step %q not found in run", stepName)
	})
}

// rerunSeeded is the shared machinery behind RerunFailed and RetryFromStep. It
// re-fetches the pipeline at the original commit (so the DAG matches the prior
// run), computes which steps to carry over vs re-run, creates a new run, and
// starts it via the engine's seeded entry point.
func (s *Service) rerunSeeded(
	ctx context.Context,
	runID string,
	triggersFn func(prior []engine.StepState) (map[string]bool, error),
) (newRunID, workflowID string, err error) {
	if s.engine == nil {
		return "", "", fmt.Errorf("engine unavailable")
	}

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

	priorWfID, err := s.q.GetRunWorkflowID(ctx, runID)
	if err != nil || priorWfID == nil {
		return "", "", fmt.Errorf("run has no workflow to retry")
	}
	priorState, err := s.engine.QueryWorkflow(ctx, *priorWfID)
	if err != nil {
		return "", "", fmt.Errorf("failed to load prior run state")
	}
	priorOutputs := s.loadStepOutputs(ctx, *priorWfID)

	workflowFile := "ci.yaml"
	if orig.WorkflowFile != nil && *orig.WorkflowFile != "" {
		workflowFile = *orig.WorkflowFile
	}
	ref := deref(orig.TriggerRef)
	sha := deref(orig.CommitSha)
	env := deref(orig.Environment)
	fetchRef := ref
	if sha != "" {
		fetchRef = sha
	}

	p, err := s.fetchPipeline(ctx, info.RepoPath, fetchRef, info.PipelinePath, workflowFile)
	if err != nil {
		return "", "", err
	}
	if s.q != nil {
		if err := p.ValidateWithPools(ctx, s.q); err != nil {
			return "", "", err
		}
	}
	waves, err := Compile(p, env)
	if err != nil {
		return "", "", fmt.Errorf("compile pipeline: %w", err)
	}

	triggers, err := triggersFn(priorState.Steps)
	if err != nil {
		return "", "", err
	}
	seed := buildRerunSeed(waves, priorState.Steps, priorOutputs, triggers)

	newRunID = observe.RequestID(ctx)
	if err := s.q.InsertRetryRun(ctx, db.InsertRetryRunParams{
		ID: newRunID, ProjectID: orig.ProjectID, OrgID: orig.OrgID,
		WorkflowFile: orig.WorkflowFile, TriggerRef: orig.TriggerRef,
		CommitSha: orig.CommitSha, Environment: orig.Environment,
	}); err != nil {
		return "", "", fmt.Errorf("failed to create retry run")
	}

	wfID, err := s.engine.StartWorkflowSeeded(ctx, engine.StartWorkflowInput{
		RunID: newRunID, OrgID: orig.OrgID, ProjectID: *orig.ProjectID,
		Repo: info.RepoPath, Ref: ref, CommitSHA: sha,
		TriggerType: "retry", TriggeredBy: "api", Environment: env,
	}, waves, seed)
	if err != nil {
		s.failRun(ctx, newRunID, err)
		return newRunID, "", err
	}
	return newRunID, wfID, nil
}

// loadStepOutputs reads the prior workflow's accumulated step_outputs map.
func (s *Service) loadStepOutputs(ctx context.Context, workflowID string) map[string]engine.StepResult {
	out := map[string]engine.StepResult{}
	raw, err := s.q.GetWorkflowStepOutputs(ctx, workflowID)
	if err != nil || len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

// buildRerunSeed decides which steps to carry over. A step is seeded (skipped on
// the new run, results reused) only if it previously succeeded AND is not in the
// re-run closure (the trigger steps plus everything downstream of them). Every
// other step is left out of the seed and runs fresh, gated normally by its deps.
func buildRerunSeed(
	waves [][]pipeline.Step,
	priorSteps []engine.StepState,
	priorOutputs map[string]engine.StepResult,
	triggers map[string]bool,
) map[string]engine.StepResult {
	closure := downstreamClosure(waves, triggers)
	seed := map[string]engine.StepResult{}
	for _, st := range priorSteps {
		if st.Status != "succeeded" || closure[st.Name] {
			continue
		}
		if res, ok := priorOutputs[st.Name]; ok {
			seed[st.Name] = res
		} else {
			// Succeeded but no recorded outputs — seed a minimal success so
			// downstream dependency gating still treats it as satisfied.
			seed[st.Name] = engine.StepResult{StepName: st.Name, Success: true}
		}
	}
	return seed
}

// downstreamClosure returns the set of steps reachable from triggers by following
// the dependency edges forward (a step depends on its `dependsOn`; we walk the
// reverse direction — "is depended on by"). The trigger steps themselves are
// included, so they re-run.
func downstreamClosure(waves [][]pipeline.Step, triggers map[string]bool) map[string]bool {
	dependents := map[string][]string{}
	for _, wave := range waves {
		for _, st := range wave {
			for _, d := range st.DependsOn {
				dependents[d] = append(dependents[d], st.Name)
			}
		}
	}
	closure := map[string]bool{}
	var visit func(string)
	visit = func(n string) {
		if closure[n] {
			return
		}
		closure[n] = true
		for _, dep := range dependents[n] {
			visit(dep)
		}
	}
	for t := range triggers {
		visit(t)
	}
	return closure
}
