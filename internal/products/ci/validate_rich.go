package ci

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// validate_rich.go — the comprehensive validator for the jobs→steps dialect.
//
// It collects EVERY issue (not fail-first) as pipeline.ValidationIssue values
// with error codes, did-you-mean suggestions, and actionable fixes — the same
// machinery the legacy dialect's validator built, applied to the language the
// server actually runs. Expressions are compile-checked against
// engine.ValidationExprContext(), the exact context shape the engine provides
// at runtime, so "validates clean, silently breaks at runtime" is structurally
// impossible for if: conditions.

// ValidateDetailed performs comprehensive validation and returns all issues.
// A pipeline with only warnings is runnable.
func (p *Pipeline) ValidateDetailed() *pipeline.ValidationResult {
	v := &richValidator{p: p, exprCtx: engine.ValidationExprContext()}
	v.run()
	return &pipeline.ValidationResult{Issues: v.issues}
}

type richValidator struct {
	p       *Pipeline
	exprCtx pipeline.ExprContext
	issues  []pipeline.ValidationIssue
}

// validJobWhenValues is the allowed set for a job's `when:` outcome gate.
var validJobWhenValues = map[string]bool{"onSuccess": true, "onFailure": true, "always": true}

func (v *richValidator) errorf(code, field, msg string, args ...any) {
	v.issues = append(v.issues, pipeline.ValidationIssue{
		Code: code, Field: field, Severity: pipeline.SeverityError,
		Message: fmt.Sprintf(msg, args...),
	})
}

func (v *richValidator) errorSuggest(code, field, suggestion, msg string, args ...any) {
	v.issues = append(v.issues, pipeline.ValidationIssue{
		Code: code, Field: field, Severity: pipeline.SeverityError,
		Message: fmt.Sprintf(msg, args...), Suggestion: suggestion,
	})
}

func (v *richValidator) warnf(code, field, msg string, args ...any) {
	v.issues = append(v.issues, pipeline.ValidationIssue{
		Code: code, Field: field, Severity: pipeline.SeverityWarning,
		Message: fmt.Sprintf(msg, args...),
	})
}

func (v *richValidator) run() {
	p := v.p

	// ── Pipeline level ──────────────────────────────────────────────────
	if p.Extends == "" && len(p.Jobs) == 0 {
		v.errorf(pipeline.CodeMissingField, "jobs",
			"pipeline must define at least one job (or extend a pipeline module)")
	}
	if len(p.Jobs) > maxJobs {
		v.errorf(pipeline.CodeBoundsExceeded, "jobs",
			"pipeline defines %d jobs (limit %d)", len(p.Jobs), maxJobs)
	}
	if !p.Triggers.HasAny() {
		v.errorSuggest(pipeline.CodeMissingField, "triggers",
			"Add e.g. triggers: { push: { branches: [main] } }",
			"pipeline must define at least one trigger")
	}
	v.checkTriggers()

	if p.Concurrency != nil {
		if p.Concurrency.Group == "" {
			v.errorf(pipeline.CodeMissingField, "concurrency.group",
				"pipeline concurrency requires a group")
		} else if strings.Contains(p.Concurrency.Group, "${{") {
			// Group expressions resolve at run creation with git/run/env.
			v.checkExpr("concurrency.group", p.Concurrency.Group, v.exprCtx)
		}
		if !p.Concurrency.CancelInProgress {
			v.errorSuggest(pipeline.CodeInvalidValue, "concurrency",
				"Set concurrency.cancelInProgress: true (queue-mode serialization is planned)",
				"queued concurrency groups are not yet supported")
		}
	}
	v.checkEnvNames("env", p.Env)

	for _, s := range p.Secrets {
		v.checkSecret("secrets", s)
	}

	// ── Jobs ────────────────────────────────────────────────────────────
	envSet := toSet(p.Environments)
	jobNames := toSet(sortedJobNames(p.Jobs))
	for _, name := range sortedJobNames(p.Jobs) {
		v.checkJob(name, p.Jobs[name], envSet, jobNames)
	}

	v.checkCycles()
}

// ── Triggers ────────────────────────────────────────────────────────────

func (v *richValidator) checkTriggers() {
	t := v.p.Triggers

	// Not-yet-fired trigger kinds: refuse loudly (see the policy in Validate).
	if t.Schedule != nil {
		v.errorSuggest(pipeline.CodeInvalidValue, "triggers.schedule",
			"Remove triggers.schedule (cron scheduling exists in the Workflows product; CI cron is planned)",
			"schedule triggers are not yet supported for CI pipelines")
	}
	if len(t.Promotion) > 0 {
		v.errorSuggest(pipeline.CodeInvalidValue, "triggers.promotion",
			"Remove triggers.promotion",
			"promotion triggers are not yet supported")
	}
	if t.Webhook != nil {
		v.errorSuggest(pipeline.CodeInvalidValue, "triggers.webhook",
			"Remove triggers.webhook (push/pull_request/tag/manual fire today)",
			"webhook triggers are not yet supported")
	}

	if t.Push != nil && len(t.Push.Branches) == 0 {
		v.warnf(pipeline.CodeMissingField, "triggers.push.branches",
			"push trigger has no branches — it will match every branch")
	}
	if t.Tag != nil && len(t.Tag.Patterns) == 0 {
		v.errorSuggest(pipeline.CodeMissingField, "triggers.tag.patterns",
			`Add patterns, e.g. patterns: ["v*"]`,
			"tag trigger requires at least one pattern")
	}

	// Manual trigger inputs: typed form fields.
	if t.Manual != nil {
		seen := map[string]bool{}
		for i, in := range t.Manual.Inputs {
			field := fmt.Sprintf("triggers.manual.inputs[%d]", i)
			if in.Name == "" {
				v.errorf(pipeline.CodeMissingField, field+".name", "manual input requires a name")
			} else if seen[in.Name] {
				v.errorf(pipeline.CodeDuplicateField, field+".name",
					"duplicate manual input %q", in.Name)
			}
			seen[in.Name] = true
			switch in.Type {
			case "string", "boolean", "":
			case "choice":
				if len(in.Options) == 0 {
					v.errorf(pipeline.CodeMissingField, field+".options",
						"choice input %q requires options", in.Name)
				}
			default:
				v.errorSuggest(pipeline.CodeInvalidValue, field+".type",
					pipeline.EnumSuggestion(in.Type, []string{"string", "boolean", "choice"}),
					"invalid input type %q", in.Type)
			}
		}
	}
}

// ── Jobs ────────────────────────────────────────────────────────────────

func (v *richValidator) checkJob(name string, job Job, pipelineEnvs, jobNames map[string]bool) {
	field := "jobs." + name

	// Body: exactly one of steps | gate (module jobs defer body checks).
	if job.Use == "" {
		if len(job.Steps) > 0 && job.Gate != nil {
			v.errorf(pipeline.CodeInvalidValue, field,
				"job %q sets both steps and gate (exactly one)", name)
		}
		switch job.ExecType() {
		case "":
			v.errorf(pipeline.CodeMissingField, field,
				"job %q must define steps or a gate", name)
		case "steps":
			if job.Image == "" && v.p.Image == "" {
				v.errorSuggest(pipeline.CodeMissingField, field+".image",
					"Add image: to this job or set a top-level image: default",
					"job %q has no image", name)
			}
			v.checkSteps(name, job)
		case "gate":
			v.checkGate(field+".gate", name, job.Gate)
		}
	}

	// needs: unknown references get a did-you-mean.
	for i, dep := range job.Needs {
		if !jobNames[dep] {
			suggestion := ""
			if closest := pipeline.FindClosest(dep, jobNames); closest != "" {
				suggestion = fmt.Sprintf("Did you mean %q?", closest)
			}
			v.errorSuggest(pipeline.CodeUnknownRef, fmt.Sprintf("%s.needs[%d]", field, i),
				suggestion, "job %q needs unknown job %q", name, dep)
		}
		if dep == name {
			v.errorf(pipeline.CodeCycleDetected, fmt.Sprintf("%s.needs[%d]", field, i),
				"job %q depends on itself", name)
		}
	}

	// Declared content inputs (D1): each inputs.needs entry must reference a job
	// this job actually `needs` (its output can't feed a content key otherwise),
	// and files/env must be non-empty/valid. This is what lets the engine derive
	// a content cache key instead of the author hand-writing one.
	if !job.Inputs.Empty() {
		needed := toSet(job.Needs)
		for i, ref := range job.Inputs.Needs {
			dep := ref
			if idx := strings.IndexByte(ref, '.'); idx >= 0 {
				dep = ref[:idx] // "build.version" → "build"
			}
			if !jobNames[dep] {
				v.errorf(pipeline.CodeUnknownRef, fmt.Sprintf("%s.inputs.needs[%d]", field, i),
					"job %q inputs reference unknown job %q", name, dep)
			} else if !needed[dep] {
				v.errorSuggest(pipeline.CodeUnknownRef, fmt.Sprintf("%s.inputs.needs[%d]", field, i),
					fmt.Sprintf("Add %q to this job's needs:", dep),
					"job %q inputs depend on %q but it is not in needs", name, dep)
			}
		}
		for i, f := range job.Inputs.Files {
			if strings.TrimSpace(f) == "" {
				v.errorf(pipeline.CodeInvalidValue, fmt.Sprintf("%s.inputs.files[%d]", field, i),
					"job %q has an empty input file glob", name)
				continue
			}
			// A malformed glob never matches, and "no match" means the job is not
			// affected — so a typo here silently drops the job from the run and
			// the pipeline still goes green. Caught at validation, where the author
			// sees it, rather than at runtime where it looks like correct
			// behaviour.
			if !doublestar.ValidatePattern(f) {
				v.errorf(pipeline.CodeInvalidValue, fmt.Sprintf("%s.inputs.files[%d]", field, i),
					"job %q input file glob %q is not a valid pattern", name, f)
			}
		}
		for i, e := range job.Inputs.Env {
			if !pipeline.IsValidEnvName(e) {
				v.errorf(pipeline.CodeInvalidValue, fmt.Sprintf("%s.inputs.env[%d]", field, i),
					"job %q input env %q is not a valid environment variable name", name, e)
			}
		}
	}

	// Environment narrowing.
	if len(pipelineEnvs) > 0 {
		for i, e := range job.Environments {
			if !pipelineEnvs[e] {
				suggestion := "Allowed: " + strings.Join(sortedKeys(pipelineEnvs), ", ")
				if closest := pipeline.FindClosest(e, pipelineEnvs); closest != "" {
					suggestion = fmt.Sprintf("Did you mean %q? %s", closest, suggestion)
				}
				v.errorSuggest(pipeline.CodeEnvMismatch, fmt.Sprintf("%s.environments[%d]", field, i),
					suggestion, "job %q targets environment %q not allowed by the pipeline", name, e)
			}
		}
	}

	for _, s := range job.Secrets {
		v.checkSecret(field+".secrets", s)
	}
	v.checkEnvNames(field+".env", job.Env)

	if job.Resources != nil && job.Resources.Limits != nil &&
		job.Resources.CPU == "" && job.Resources.Memory == "" {
		v.errorf(pipeline.CodeInvalidValue, field+".resources",
			"job %q sets resource limits without requests", name)
	}
	if job.Concurrency != nil {
		v.errorSuggest(pipeline.CodeInvalidValue, field+".concurrency",
			"Use pipeline-level concurrency with cancelInProgress: true",
			"job-level concurrency groups are not yet supported")
	}

	v.checkDuration(field+".timeout", job.Timeout)
	v.checkMatrix(field, name, job)

	// fanOut: a runtime array expression. Mutually exclusive with matrix (both
	// are fan-out mechanisms; matrix is compile-time, fanOut runtime), and
	// compile-checked against the runtime context.
	if job.FanOut != "" {
		if !job.Matrix.Empty() {
			v.errorf(pipeline.CodeInvalidValue, field+".fanOut",
				"job %q sets both matrix and fanOut — use one fan-out mechanism", name)
		}
		v.checkExpr(field+".fanOut", job.FanOut, v.jobExprCtx(job))
	}

	// failFast: false is the current engine behavior (all variants run to
	// completion), so accept it — it lets an author state the intent explicitly.
	// failFast: true would need the engine to cancel sibling variants on the
	// first failure, which it does not do yet, so it is still rejected.
	if job.FailFast != nil && *job.FailFast {
		v.errorSuggest(pipeline.CodeInvalidValue, field+".failFast",
			"Set failFast: false (or remove it) — variants currently always run to completion",
			"matrix failFast: true (cancel siblings on first failure) is not yet enforced by the engine")
	}
	if job.MaxParallel != 0 {
		v.errorSuggest(pipeline.CodeInvalidValue, field+".maxParallel",
			"Remove maxParallel",
			"matrix parallelism caps are not yet enforced by the engine")
	}

	// when: — outcome gate on the job's needs subgraph.
	if job.When != "" && !validJobWhenValues[job.When] {
		suggestion := "Valid values: onSuccess, onFailure, always"
		if closest := pipeline.FindClosest(job.When, validJobWhenValues); closest != "" {
			suggestion = fmt.Sprintf("Did you mean %q? %s", closest, suggestion)
		}
		v.errorSuggest(pipeline.CodeInvalidValue, field+".when",
			suggestion, "job %q has invalid when value %q", name, job.When)
	}

	// if: — compile-checked against the runtime context shape.
	v.checkExpr(field+".if", job.If, v.jobExprCtx(job))

	// cache key gets hashFiles on top of the runtime context.
	if job.Cache != nil {
		if job.Cache.Key == "" {
			v.errorf(pipeline.CodeMissingField, field+".cache.key", "cache requires a key")
		}
		if len(job.Cache.Paths) == 0 {
			v.errorf(pipeline.CodeMissingField, field+".cache.paths", "cache requires paths")
		}
		cacheCtx := v.jobExprCtx(job)
		cacheCtx["hashFiles"] = func(string) string { return "" }
		v.checkExpr(field+".cache.key", job.Cache.Key, cacheCtx)
	}
}

func (v *richValidator) checkSteps(jobName string, job Job) {
	names := map[string]bool{}
	for i, s := range job.Steps {
		field := fmt.Sprintf("jobs.%s.steps[%d]", jobName, i)

		set := 0
		if !s.Run.IsEmpty() {
			set++
		}
		if s.Use != "" {
			set++
		}
		if s.Inject != "" {
			set++
		}
		if set != 1 {
			v.errorf(pipeline.CodeInvalidValue, field,
				"job %q step %d must set exactly one of run, use, or inject", jobName, i)
		}

		if s.Name != "" {
			if names[s.Name] {
				v.errorf(pipeline.CodeDuplicateField, field+".name",
					"duplicate step name %q (step outputs would collide)", s.Name)
			}
			names[s.Name] = true
		}

		if s.Shell != "" {
			valid := pipeline.ValidShells()
			ok := false
			for _, sh := range valid {
				if s.Shell == sh {
					ok = true
				}
			}
			if !ok {
				v.errorSuggest(pipeline.CodeInvalidValue, field+".shell",
					pipeline.EnumSuggestion(s.Shell, valid),
					"invalid shell %q", s.Shell)
			}
		}

		v.checkDuration(field+".timeout", s.Timeout)
		v.checkEnvNames(field+".env", s.Env)
		for _, sec := range s.Secrets {
			v.checkSecret(field+".secrets", sec)
		}
		if s.Retry != nil {
			if s.Retry.Attempts < 1 {
				v.errorf(pipeline.CodeInvalidValue, field+".retry.attempts",
					"retry attempts must be at least 1")
			}
			v.checkDuration(field+".retry.delay", s.Retry.Delay)
		}
		v.checkExpr(field+".if", s.If, v.jobExprCtx(job))
	}
}

func (v *richValidator) checkGate(field, jobName string, g *pipeline.Gate) {
	if g == nil {
		return
	}
	if len(g.Approvers) == 0 {
		v.errorSuggest(pipeline.CodeMissingField, field+".approvers",
			"Use role:slug, team:slug, or user@email.com",
			"gate job %q requires at least one approver", jobName)
	}
	for i, a := range g.Approvers {
		if !pipeline.IsValidApprover(a) {
			v.errorSuggest(pipeline.CodeInvalidApprover, fmt.Sprintf("%s.approvers[%d]", field, i),
				"Use role:slug, team:slug, or user@email.com",
				"invalid approver format %q", a)
		}
	}
	if g.MinApprovals < 0 {
		v.errorf(pipeline.CodeInvalidValue, field+".minApprovals", "minApprovals must not be negative")
	}
	if g.MinApprovals > len(g.Approvers) && len(g.Approvers) > 0 {
		v.errorf(pipeline.CodeInvalidValue, field+".minApprovals",
			"minApprovals (%d) exceeds number of approvers (%d)", g.MinApprovals, len(g.Approvers))
	}
}

// ── Matrix ──────────────────────────────────────────────────────────────

func (v *richValidator) checkMatrix(field, name string, job Job) {
	if !job.Matrix.Empty() {
		for dim, vals := range job.Matrix.Dimensions {
			if len(vals) == 0 {
				v.errorf(pipeline.CodeInvalidValue, field+".matrix."+dim,
					"matrix dimension %q has no values", dim)
			}
		}
		// exclude keys must name real dimensions — an exclude on a non-dimension
		// silently matches nothing, which is almost always a typo.
		dimSet := map[string]bool{}
		for dim := range job.Matrix.Dimensions {
			dimSet[dim] = true
		}
		for i, e := range job.Matrix.Exclude {
			for k := range e {
				if !dimSet[k] {
					suggestion := "Declared dimensions: " + strings.Join(sortedKeys(dimSet), ", ")
					if closest := pipeline.FindClosest(k, dimSet); closest != "" {
						suggestion = fmt.Sprintf("Did you mean %q? %s", closest, suggestion)
					}
					v.errorSuggest(pipeline.CodeUnknownRef, fmt.Sprintf("%s.matrix.exclude[%d]", field, i),
						suggestion, "exclude references unknown matrix dimension %q", k)
				}
			}
		}
		// The bound is on the FINAL combination count (after exclude/include).
		if combos := len(expandMatrixCombos(job.Matrix)); combos > maxMatrixCombos {
			v.errorf(pipeline.CodeBoundsExceeded, field+".matrix",
				"job %q matrix expands to %d combinations (limit %d)", name, combos, maxMatrixCombos)
		}
		if len(job.Artifacts) > 0 {
			v.errorf(pipeline.CodeInvalidValue, field+".artifacts",
				"job %q declares artifacts on a matrix job — artifact paths would collide across variants; this is not yet supported", name)
		}
		if len(job.Outputs) > 0 {
			v.errorf(pipeline.CodeInvalidValue, field+".outputs",
				"job %q declares outputs on a matrix job — output values would collide across variants; this is not yet supported", name)
		}
	}

	// Every ${{ matrix.X }} reference must name a declared dimension (including
	// keys introduced only by include entries), and matrix references are only
	// valid inside matrix jobs.
	dims := job.Matrix.AllKeys()
	check := func(where, s string) {
		for _, key := range matrixRefs(s) {
			if job.Matrix.Empty() {
				v.errorf(pipeline.CodeUnknownRef, where,
					"references matrix.%s but job %q has no matrix", key, name)
				continue
			}
			if !dims[key] {
				suggestion := "Declared dimensions: " + strings.Join(sortedKeys(dims), ", ")
				if closest := pipeline.FindClosest(key, dims); closest != "" {
					suggestion = fmt.Sprintf("Did you mean %q? %s", closest, suggestion)
				}
				v.errorSuggest(pipeline.CodeUnknownRef, where, suggestion,
					"references unknown matrix dimension %q", key)
			}
		}
	}
	check(field+".image", job.Image)
	check(field+".if", job.If)
	for k, val := range job.Env {
		check(field+".env."+k, val)
	}
	if job.Cache != nil {
		check(field+".cache.key", job.Cache.Key)
	}
	for i, s := range job.Steps {
		sf := fmt.Sprintf("%s.steps[%d]", field, i)
		for _, c := range s.Run.Commands {
			check(sf+".run", c)
		}
		check(sf+".if", s.If)
		check(sf+".workingDir", s.WorkingDir)
		for k, val := range s.Env {
			check(sf+".env."+k, val)
		}
	}
}

// matrixRefs extracts the dimension names referenced as matrix.X — both the
// whole-template form (${{ matrix.X }}) and tokens inside larger expressions
// (${{ matrix.X == "y" }}).
func matrixRefs(s string) []string {
	if s == "" {
		return nil
	}
	var keys []string
	for _, m := range matrixTokenExpr.FindAllStringSubmatch(s, -1) {
		keys = append(keys, m[1])
	}
	return keys
}

// ── Shared field checks ─────────────────────────────────────────────────

func (v *richValidator) checkSecret(field string, s Secret) {
	sources := 0
	if s.Name != "" {
		sources++
	}
	if s.From != "" {
		sources++
	}
	if sources != 1 {
		v.errorf(pipeline.CodeInvalidValue, field,
			"secret must set exactly one source (name or from)")
	}
	targets := 0
	if s.Env != "" {
		targets++
	}
	if s.File != "" {
		targets++
	}
	if targets != 1 {
		v.errorf(pipeline.CodeInvalidValue, field,
			"secret must set exactly one target (env or file)")
	}
	if s.Mode != "" && s.File == "" {
		v.errorf(pipeline.CodeInvalidValue, field,
			"secret sets mode without a file target")
	}
	// Schema-accepted but not yet wired — refuse loudly.
	if s.From != "" {
		v.errorSuggest(pipeline.CodeInvalidValue, field,
			"Use name: with the built-in store",
			"secret uses from: %q — external secret providers (vault/aws-sm/gcp-sm/azure-kv/k8s) are not yet supported", s.From)
	}
	if s.File != "" {
		v.errorSuggest(pipeline.CodeInvalidValue, field,
			"Use env: instead",
			"secret uses a file: target — file-mounted secrets are not yet supported")
	}
	if s.Env != "" && !pipeline.IsValidEnvName(s.Env) {
		v.errorf(pipeline.CodeInvalidValue, field,
			"invalid env var name %q", s.Env)
	}
}

func (v *richValidator) checkEnvNames(field string, env map[string]string) {
	for _, k := range sortedKeys(toSet(mapKeys(env))) {
		if !pipeline.IsValidEnvName(k) {
			v.errorSuggest(pipeline.CodeInvalidValue, field+"."+k,
				"Env var names must match [A-Za-z_][A-Za-z0-9_]*",
				"invalid env var name %q", k)
		}
	}
}

func (v *richValidator) checkDuration(field, raw string) {
	if raw == "" {
		return
	}
	if _, err := time.ParseDuration(raw); err != nil {
		v.errorSuggest(pipeline.CodeInvalidValue, field,
			"Use Go duration format: 30s, 5m, 1h, 2h30m",
			"invalid duration %q", raw)
	}
}

// checkExpr compile-checks a ${{ }} expression against a context. Bare strings
// without ${{ }} are constant conditions — flagged as warnings since a literal
// "true"/"false" is almost always a mistake for if:.
//
// matrix.X references are substituted with a benign string literal before
// compiling: matrix values are interpolated away at compile time (expandMatrix),
// so the runtime context never contains a matrix namespace — the dimension
// names themselves are validated separately by checkMatrix.
func (v *richValidator) checkExpr(field, expression string, ctx pipeline.ExprContext) {
	if expression == "" {
		return
	}
	if !strings.Contains(expression, "${{") {
		v.warnf(pipeline.CodeInvalidExpression, field,
			"condition %q is a constant (missing ${{ }}?)", expression)
		return
	}
	stripped := matrixTokenExpr.ReplaceAllString(expression, `"x"`)
	if err := pipeline.CheckExpr(stripped, ctx); err != nil {
		v.errorSuggest(pipeline.CodeInvalidExpression, field,
			"Available contexts: git.*, run.*, env.*, inputs.*, steps.* (and matrix.* in matrix jobs)",
			"invalid expression: %v", err)
	}
}

// jobExprCtx returns the runtime expression context for a job's expressions.
func (v *richValidator) jobExprCtx(_ Job) pipeline.ExprContext {
	return v.exprCtx
}

// ── Cycle detection with path reporting ─────────────────────────────────

func (v *richValidator) checkCycles() {
	p := v.p
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(p.Jobs))
	var stack []string

	var visit func(string) bool
	visit = func(n string) bool {
		color[n] = gray
		stack = append(stack, n)
		deps := append([]string(nil), p.Jobs[n].Needs...)
		sort.Strings(deps)
		for _, d := range deps {
			if _, ok := p.Jobs[d]; !ok {
				continue // unknown ref reported elsewhere
			}
			switch color[d] {
			case gray:
				// Found the cycle: slice the stack from d's position for the path.
				path := cyclePathFrom(stack, d)
				v.errorf(pipeline.CodeCycleDetected, "jobs."+d,
					"dependency cycle through job %q: %s", d, strings.Join(path, " → "))
				return true
			case white:
				if visit(d) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
		return false
	}
	for _, n := range sortedJobNames(p.Jobs) {
		if color[n] == white {
			if visit(n) {
				return
			}
		}
	}
}

func cyclePathFrom(stack []string, start string) []string {
	for i, n := range stack {
		if n == start {
			path := append([]string(nil), stack[i:]...)
			return append(path, start)
		}
	}
	return []string{start}
}

// ── small helpers ───────────────────────────────────────────────────────

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func mapKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
