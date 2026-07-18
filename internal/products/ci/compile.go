package ci

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strconv"
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

	// 3. Build one group-step per surviving job. Module references must have
	// been resolved before Compile — an unresolved use:/inject: here would
	// compile to an empty (silently succeeding) sub-step.
	groups := make([]pipeline.Step, 0, len(surviving))
	for _, name := range sortedExpandedNames(expanded) {
		if !surviving[name] {
			continue
		}
		job := expanded[name]
		if job.Use != "" {
			return nil, fmt.Errorf("ci: job %q has an unresolved module reference %q (module resolution must run before compile)", name, job.Use)
		}
		for i, s := range job.Steps {
			if s.Use != "" || s.Inject != "" {
				return nil, fmt.Errorf("ci: job %q step %d has an unresolved module reference (module resolution must run before compile)", name, i)
			}
		}
		groups = append(groups, p.compileJob(name, job, expanded, surviving))
	}

	// 4. Order into waves via the shared DAG resolver.
	waves, err := pipeline.ResolveDag(&pipeline.Pipeline{Steps: groups})
	if err != nil {
		return nil, fmt.Errorf("ci: resolve DAG: %w", err)
	}
	return waves, nil
}

func (p *Pipeline) compileJob(name string, job Job, expanded map[string]Job, surviving map[string]bool) pipeline.Step {
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
		When:           job.When,
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

	// Artifacts: the producer declares `artifacts: [paths]` → uploaded after
	// the job succeeds; every job that `needs` it gets those paths downloaded
	// before it starts (the spec's "artifacts flow along needs edges").
	for _, path := range job.Artifacts {
		step.Outputs = append(step.Outputs, pipeline.ArtifactOutput{Path: path})
	}
	for _, dep := range deps {
		for _, path := range expanded[dep].Artifacts {
			step.Inputs = append(step.Inputs, pipeline.ArtifactInput{From: dep, Path: path})
		}
	}

	// Job outputs (values): evaluated in-pod after the sub-steps run, then
	// read downstream as needs.<job>.outputs.<name>.
	if len(job.Outputs) > 0 {
		step.DeclaredOutputs = job.Outputs
	}
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

// matrixExpr matches the whole-template form: ${{ matrix.X }} — used to splice
// matrix values into plain strings (image, run, env, workingDir, cache key).
var matrixExpr = regexp.MustCompile(`\$\{\{\s*matrix\.(\w+)\s*\}\}`)

// matrixTokenExpr matches a matrix.X reference anywhere — including inside a
// larger expression like ${{ matrix.go == "1.26" }} — used to splice quoted
// values into if: expressions.
var matrixTokenExpr = regexp.MustCompile(`\bmatrix\.(\w+)\b`)

// expandMatrix replaces each matrix job with one concrete job per combination,
// named "<job>::<v1>-<v2>". Non-matrix jobs pass through unchanged. Every
// field an author can reference matrix values from is interpolated: image,
// if:, env values, cache key, and per-step run/if/env/workingDir. Partial
// interpolation (some fields but not others) is the "decorative field" bug
// class — keep this list exhaustive.
func expandMatrix(jobs map[string]Job) map[string]Job {
	out := make(map[string]Job, len(jobs))
	for name, job := range jobs {
		if job.Matrix.Empty() {
			out[name] = job
			continue
		}
		for _, combo := range expandMatrixCombos(job.Matrix) {
			variant := job
			variant.Matrix = nil
			variant.Image = interpolateMatrix(variant.Image, combo)
			variant.If = interpolateMatrixExpr(variant.If, combo)
			variant.Env = interpolateEnv(variant.Env, combo)
			if variant.Cache != nil {
				c := *variant.Cache
				c.Key = interpolateMatrix(c.Key, combo)
				variant.Cache = &c
			}
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
		s.If = interpolateMatrixExpr(s.If, combo)
		s.Env = interpolateEnv(s.Env, combo)
		s.WorkingDir = interpolateMatrix(s.WorkingDir, combo)
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

func interpolateEnv(env map[string]string, combo map[string]string) map[string]string {
	if len(env) == 0 {
		return env
	}
	out := make(map[string]string, len(env))
	for k, val := range env {
		out[k] = interpolateMatrix(val, combo)
	}
	return out
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

// interpolateMatrixExpr splices matrix values into an if: expression by
// replacing matrix.X tokens with quoted string literals, so
// ${{ matrix.go == "1.26" }} becomes ${{ "1.26" == "1.26" }} for the variant.
// The engine's runtime context has no matrix namespace — after expansion no
// matrix token may survive.
func interpolateMatrixExpr(s string, combo map[string]string) string {
	if s == "" {
		return s
	}
	return matrixTokenExpr.ReplaceAllStringFunc(s, func(m string) string {
		key := matrixTokenExpr.FindStringSubmatch(m)[1]
		if v, ok := combo[key]; ok {
			return strconv.Quote(v)
		}
		return m
	})
}

// matrixCombos returns the cartesian product of the matrix dimensions, with keys
// processed in sorted order for deterministic naming.
// expandMatrixCombos produces the final combination set: the cartesian product
// of the base dimensions, minus excludes, plus includes. Semantics follow GitHub
// Actions:
//   - exclude: drop every combination that matches all key/values in the entry
//     (a partial filter — exclude {os: mac} removes every mac combination).
//   - include: for each entry, split its keys into base-dimension keys (a filter)
//     and extra keys. Combinations matching the filter gain the extra keys
//     (without overwriting existing values). If the entry's filter matches no
//     existing combination (it names dimension values not in the product), it is
//     appended as a brand-new combination. An entry with only extra keys merges
//     them into every combination.
func expandMatrixCombos(m *MatrixSpec) []map[string]string {
	combos := matrixCombos(m.Dimensions)

	if len(m.Exclude) > 0 {
		kept := combos[:0:0]
		for _, c := range combos {
			if !comboMatchesAny(c, m.Exclude) {
				kept = append(kept, c)
			}
		}
		combos = kept
	}

	baseKeys := make(map[string]bool, len(m.Dimensions))
	for k := range m.Dimensions {
		baseKeys[k] = true
	}
	for _, inc := range m.Include {
		filter := map[string]string{}
		extra := map[string]string{}
		for k, v := range inc {
			if baseKeys[k] {
				filter[k] = v
			} else {
				extra[k] = v
			}
		}
		matched := false
		for _, c := range combos {
			if comboMatches(c, filter) {
				matched = true
				for k, v := range extra {
					if _, exists := c[k]; !exists {
						c[k] = v
					}
				}
			}
		}
		if !matched {
			nc := make(map[string]string, len(inc))
			maps.Copy(nc, inc)
			combos = append(combos, nc)
		}
	}
	return dedupCombos(combos)
}

// comboMatches reports whether every key/value in filter is present and equal in
// combo. An empty filter matches any combination.
func comboMatches(combo, filter map[string]string) bool {
	for k, v := range filter {
		if combo[k] != v {
			return false
		}
	}
	return true
}

// comboMatchesAny reports whether combo matches any of the (non-empty) entries.
func comboMatchesAny(combo map[string]string, entries []map[string]string) bool {
	for _, e := range entries {
		if len(e) > 0 && comboMatches(combo, e) {
			return true
		}
	}
	return false
}

// dedupCombos drops exact-duplicate combinations (same key/value set), which
// include entries can introduce, keeping first-seen order.
func dedupCombos(combos []map[string]string) []map[string]string {
	seen := map[string]bool{}
	out := combos[:0]
	for _, c := range combos {
		keys := make([]string, 0, len(c))
		for k := range c {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var sig strings.Builder
		for _, k := range keys {
			sig.WriteString(k)
			sig.WriteByte('=')
			sig.WriteString(c[k])
			sig.WriteByte('\x00')
		}
		if !seen[sig.String()] {
			seen[sig.String()] = true
			out = append(out, c)
		}
	}
	return out
}

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
