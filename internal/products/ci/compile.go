package ci

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// Compile turns a validated pipeline into execution waves for the engine, for a
// given target environment ("" for a plain, non-environment run). Each job
// becomes one engine step-group (a pod that runs the job's steps); job `needs`
// become DependsOn edges; matrix jobs fan out into one group per combination.
//
// Environment filtering is compile-time: jobs whose `environments` exclude the
// target are dropped and their edges removed (a skipped need is satisfied, so
// downstream jobs still run). Conditions (`if:`) are runtime — they are carried
// onto the group-step and evaluated by the engine during advancement.
func Compile(p *Pipeline, env string) ([][]pipeline.Step, error) {
	// 1. Expand matrix jobs into concrete jobs.
	expanded := expandMatrix(p.Jobs)

	// 2. Drop jobs whose environments exclude the target.
	surviving := map[string]bool{}
	for name, job := range expanded {
		if jobRunsInEnv(job, env) {
			surviving[name] = true
		}
	}

	// 3. Build one group-step per surviving job.
	groups := make([]pipeline.Step, 0, len(surviving))
	for _, name := range sortedExpandedNames(expanded) {
		if !surviving[name] {
			continue
		}
		groups = append(groups, p.compileJob(name, expanded[name], surviving))
	}

	// 4. Order into waves via the shared DAG resolver.
	waves, err := pipeline.ResolveDag(&pipeline.Pipeline{Steps: groups})
	if err != nil {
		return nil, fmt.Errorf("ci: resolve DAG: %w", err)
	}
	return waves, nil
}

func (p *Pipeline) compileJob(name string, job Job, surviving map[string]bool) pipeline.Step {
	// Drop needs to jobs that were filtered out (skipped need = satisfied).
	var deps []string
	for _, d := range job.Needs {
		// `d` may be a matrix-expanded base name; keep any surviving variant.
		for s := range surviving {
			if s == d || strings.HasPrefix(s, d+matrixSep) {
				deps = append(deps, s)
			}
		}
	}
	sort.Strings(deps)

	step := pipeline.Step{
		Name:           name,
		DependsOn:      deps,
		Image:          firstNonEmpty(job.Image, p.Image),
		Runner:         firstNonEmpty(job.Runner, p.Runner),
		ServiceAccount: firstNonEmpty(job.ServiceAccount, p.ServiceAccount),
		Disk:           job.Disk,
		Resources:      compileResources(job.Resources),
		Environments:   job.Environments,
		If:             job.If,
		Timeout:        job.Timeout,
		Env:            mergeEnv(p.Env, job.Env),
		Secrets:        compileSecrets(p.Secrets, job.Secrets),
		Services:       job.Services,
		Cache:          job.Cache,
		Gate:           job.Gate,
	}
	if job.ExecType() == "steps" {
		step.Steps = compileSteps(job.Steps)
	}
	// NOTE: job.Resources / file+external secrets are carried in the CI schema but
	// not yet mapped to the engine IR — wired in the executor/secrets PRs.
	return step
}

func compileSteps(steps []Step) []pipeline.Step {
	out := make([]pipeline.Step, 0, len(steps))
	for _, s := range steps {
		out = append(out, pipeline.Step{
			Name: s.Name,
			Run:  s.Run,
			// Use/With are resolved away by module expansion before Compile; a
			// compiled step is always a concrete run/action.
			Env:             s.Env,
			Secrets:         compileSecrets(nil, s.Secrets),
			Shell:           s.Shell,
			WorkingDir:      s.WorkingDir,
			Timeout:         s.Timeout,
			ContinueOnError: s.ContinueOnError,
			Retry:           s.Retry,
			If:              s.If,
		})
	}
	return out
}

// compileSecrets maps env-target, built-in-store bindings to the engine's current
// env-injection map (name → store key). File targets and external providers are
// carried in the CI schema and wired by the secrets subsystem PR.
func compileSecrets(pipelineSecrets, scopeSecrets []Secret) map[string]string {
	out := map[string]string{}
	add := func(list []Secret) {
		for _, s := range list {
			if s.Env != "" && s.Name != "" {
				out[s.Env] = s.Name
			}
		}
	}
	add(pipelineSecrets)
	add(scopeSecrets)
	if len(out) == 0 {
		return nil
	}
	return out
}

func compileResources(r *Resources) *pipeline.StepResources {
	if r == nil {
		return nil
	}
	out := &pipeline.StepResources{CPU: r.CPU, Memory: r.Memory, GPU: r.GPU}
	if r.Limits != nil {
		out.Limits = &pipeline.StepResourceLimits{CPU: r.Limits.CPU, Memory: r.Limits.Memory}
	}
	return out
}

func jobRunsInEnv(job Job, env string) bool {
	if len(job.Environments) == 0 {
		return true // job runs in any environment (and in plain runs)
	}
	if env == "" {
		return false // environment-scoped job, but this is a plain run
	}
	return slices.Contains(job.Environments, env)
}

// ── Matrix expansion ────────────────────────────────────────────────────────

const matrixSep = "::"

var matrixExpr = regexp.MustCompile(`\$\{\{\s*matrix\.(\w+)\s*\}\}`)

// expandMatrix replaces each matrix job with one concrete job per combination,
// named "<job>::<v1>-<v2>". Non-matrix jobs pass through unchanged.
func expandMatrix(jobs map[string]Job) map[string]Job {
	out := make(map[string]Job, len(jobs))
	for name, job := range jobs {
		if len(job.Matrix) == 0 {
			out[name] = job
			continue
		}
		for _, combo := range matrixCombos(job.Matrix) {
			variant := job
			variant.Matrix = nil
			variant.Image = interpolateMatrix(variant.Image, combo)
			variant.Steps = interpolateSteps(variant.Steps, combo)
			out[name+matrixSep+comboSuffix(combo)] = variant
		}
	}
	return out
}

func interpolateSteps(steps []Step, combo map[string]string) []Step {
	out := make([]Step, len(steps))
	for i, s := range steps {
		s.Run = interpolateRun(s.Run, combo)
		out[i] = s
	}
	return out
}

func interpolateRun(rc pipeline.RunCommand, combo map[string]string) pipeline.RunCommand {
	cmds := make([]string, len(rc.Commands))
	for i, c := range rc.Commands {
		cmds[i] = interpolateMatrix(c, combo)
	}
	return pipeline.RunCommand{Commands: cmds}
}

func interpolateMatrix(s string, combo map[string]string) string {
	if s == "" {
		return s
	}
	return matrixExpr.ReplaceAllStringFunc(s, func(m string) string {
		key := matrixExpr.FindStringSubmatch(m)[1]
		if v, ok := combo[key]; ok {
			return v
		}
		return m
	})
}

// matrixCombos returns the cartesian product of the matrix dimensions, with keys
// processed in sorted order for deterministic naming.
func matrixCombos(m map[string][]string) []map[string]string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	combos := []map[string]string{{}}
	for _, k := range keys {
		var next []map[string]string
		for _, base := range combos {
			for _, v := range m[k] {
				c := make(map[string]string, len(base)+1)
				maps.Copy(c, base)
				c[k] = v
				next = append(next, c)
			}
		}
		combos = next
	}
	return combos
}

func comboSuffix(combo map[string]string) string {
	keys := make([]string, 0, len(combo))
	for k := range combo {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, combo[k])
	}
	return strings.Join(parts, "-")
}

func sortedExpandedNames(jobs map[string]Job) []string {
	names := make([]string, 0, len(jobs))
	for n := range jobs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ── small helpers ───────────────────────────────────────────────────────────

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func mergeEnv(ms ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range ms {
		maps.Copy(out, m)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
