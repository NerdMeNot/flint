package ci

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/NerdMeNot/flint/pkg/pipeline"
	"gopkg.in/yaml.v3"
)

var inputExpr = regexp.MustCompile(`\$\{\{\s*inputs\.(\w+)\s*\}\}`)

// ResolveModules expands every use:/extends: reference in the pipeline into
// concrete jobs and steps, applying typed inputs, the steps-hole (inject:), and
// the requires: environment-contract check. The result has no use:/extends: left
// and is ready for Validate + Compile. A pipeline with no module references is
// returned unchanged.
func ResolveModules(ctx context.Context, p *Pipeline, r ModuleResolver) (*Pipeline, error) {
	if p.Extends != "" {
		if err := expandExtends(ctx, p, r); err != nil {
			return nil, err
		}
	}
	for name := range p.Jobs {
		job := p.Jobs[name]
		if job.Use != "" {
			expanded, err := expandJobModule(ctx, name, job, r)
			if err != nil {
				return nil, err
			}
			job = expanded
		}
		steps, err := expandStepModules(ctx, name, job, r)
		if err != nil {
			return nil, err
		}
		job.Steps = steps
		p.Jobs[name] = job
	}
	return p, nil
}

// expandExtends merges a pipeline module's jobs into the consumer (consumer jobs
// override by name; consumer keeps its own triggers/env/concurrency).
func expandExtends(ctx context.Context, p *Pipeline, r ModuleResolver) error {
	mod, err := r.Resolve(ctx, p.Extends)
	if err != nil {
		return err
	}
	if mod.Kind != "pipeline" {
		return fmt.Errorf("ci: extends %q is a %q module, want pipeline", p.Extends, mod.Kind)
	}
	vals, err := buildInputs(mod, p.With)
	if err != nil {
		return fmt.Errorf("ci: extends %q: %w", p.Extends, err)
	}
	jobs := make(map[string]Job, len(mod.Jobs)+len(p.Jobs))
	for name, job := range mod.Jobs {
		jobs[name] = interpolateJob(job, vals)
	}
	for name, job := range p.Jobs { // consumer overrides/additions win
		jobs[name] = job
	}
	p.Jobs = jobs
	p.Extends = ""
	p.With = nil
	return nil
}

// expandJobModule replaces a job's body with a job module's, keeping the
// consumer's graph fields (needs/environments/if) and filling the steps-hole.
func expandJobModule(ctx context.Context, name string, job Job, r ModuleResolver) (Job, error) {
	mod, err := r.Resolve(ctx, job.Use)
	if err != nil {
		return job, err
	}
	if mod.Kind != "job" {
		return job, fmt.Errorf("ci: job %q use %q is a %q module, want job", name, job.Use, mod.Kind)
	}
	vals, err := buildInputs(mod, job.With)
	if err != nil {
		return job, fmt.Errorf("ci: job %q: %w", name, err)
	}
	body := interpolateJob(*mod.Job, vals)
	body.Steps, err = injectSteps(body.Steps, vals)
	if err != nil {
		return job, fmt.Errorf("ci: job %q: %w", name, err)
	}
	if mod.Requires != nil {
		if err := checkEnv(mod.Requires, body.Image, name); err != nil {
			return job, err
		}
	}
	// Caller owns the graph; module owns the body.
	body.Needs = job.Needs
	body.Environments = job.Environments
	body.If = job.If
	body.Use = ""
	body.With = nil
	return body, nil
}

// expandStepModules inlines step-level use: references (kind steps or action).
func expandStepModules(ctx context.Context, jobName string, job Job, r ModuleResolver) ([]Step, error) {
	var out []Step
	for _, s := range job.Steps {
		if s.Use == "" {
			out = append(out, s)
			continue
		}
		mod, err := r.Resolve(ctx, s.Use)
		if err != nil {
			return nil, err
		}
		vals, err := buildInputs(mod, s.With)
		if err != nil {
			return nil, fmt.Errorf("ci: job %q step use %q: %w", jobName, s.Use, err)
		}
		switch mod.Kind {
		case "steps":
			if mod.Requires != nil {
				if err := checkEnv(mod.Requires, job.Image, jobName); err != nil {
					return nil, err
				}
			}
			inlined, err := injectSteps(interpStepsInputs(mod.Steps, vals), vals)
			if err != nil {
				return nil, err
			}
			out = append(out, inlined...)
		case "action":
			// Containerized (own-image) modules need per-step image execution,
			// which is executor/runtime work — supported once that lands.
			return nil, fmt.Errorf("ci: job %q step use %q: action modules not yet supported (needs per-step image execution)", jobName, s.Use)
		default:
			return nil, fmt.Errorf("ci: job %q step use %q is a %q module, want steps or action", jobName, s.Use, mod.Kind)
		}
	}
	return out, nil
}

// ── inputs ──────────────────────────────────────────────────────────────────

type inputValues struct {
	strs  map[string]string
	steps map[string][]Step
}

func buildInputs(mod *Module, with map[string]any) (inputValues, error) {
	v := inputValues{strs: map[string]string{}, steps: map[string][]Step{}}
	for name, decl := range mod.Inputs {
		raw, given := with[name]
		if decl.Type == "steps" {
			if !given {
				continue // empty hole is allowed
			}
			steps, err := toSteps(raw)
			if err != nil {
				return v, fmt.Errorf("input %q: %w", name, err)
			}
			v.steps[name] = steps
			continue
		}
		var val string
		switch {
		case given:
			val = fmt.Sprintf("%v", raw)
		case decl.Default != "":
			val = decl.Default
		case decl.Required:
			return v, fmt.Errorf("required input %q not provided", name)
		}
		if decl.Type == "enum" && val != "" && !slices.Contains(decl.Options, val) {
			return v, fmt.Errorf("input %q value %q not in options %v", name, val, decl.Options)
		}
		v.strs[name] = val
	}
	return v, nil
}

// toSteps converts a `with:` steps value (YAML list) into []Step.
func toSteps(raw any) ([]Step, error) {
	b, err := yaml.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var steps []Step
	if err := yaml.Unmarshal(b, &steps); err != nil {
		return nil, fmt.Errorf("not a valid steps list: %w", err)
	}
	return steps, nil
}

// ── interpolation & injection ───────────────────────────────────────────────

func interpolate(s string, v inputValues) string {
	if s == "" {
		return s
	}
	return inputExpr.ReplaceAllStringFunc(s, func(m string) string {
		key := inputExpr.FindStringSubmatch(m)[1]
		if val, ok := v.strs[key]; ok {
			return val
		}
		return m
	})
}

func interpolateJob(j Job, v inputValues) Job {
	j.Image = interpolate(j.Image, v)
	j.Env = interpolateMap(j.Env, v)
	j.Steps = interpStepsInputs(j.Steps, v)
	return j
}

func interpStepsInputs(steps []Step, v inputValues) []Step {
	out := make([]Step, len(steps))
	for i, s := range steps {
		s.Run = interpolateRunCmd(s.Run, v)
		s.Env = interpolateMap(s.Env, v)
		out[i] = s
	}
	return out
}

func interpolateRunCmd(rc pipeline.RunCommand, v inputValues) pipeline.RunCommand {
	if len(rc.Commands) == 0 {
		return rc
	}
	out := make([]string, len(rc.Commands))
	for i, c := range rc.Commands {
		out[i] = interpolate(c, v)
	}
	return pipeline.RunCommand{Commands: out}
}

func interpolateMap(m map[string]string, v inputValues) map[string]string {
	if len(m) == 0 {
		return m
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		out[k] = interpolate(val, v)
	}
	return out
}

// injectSteps replaces any `inject:` step with the steps-typed input it names.
func injectSteps(steps []Step, v inputValues) ([]Step, error) {
	var out []Step
	for _, s := range steps {
		if s.Inject == "" {
			out = append(out, s)
			continue
		}
		mm := inputExpr.FindStringSubmatch(s.Inject)
		if mm == nil {
			return nil, fmt.Errorf("inject %q must reference a steps input, e.g. ${{ inputs.steps }}", s.Inject)
		}
		out = append(out, v.steps[mm[1]]...)
	}
	return out, nil
}

// ── env contract ────────────────────────────────────────────────────────────

// checkEnv enforces an inline module's requires: against the job's image family.
func checkEnv(req *Requires, image, jobName string) error {
	if req.Family == "" {
		return nil
	}
	fam := imageFamily(image)
	if fam == "" {
		return nil // unknown image — can't disprove; allow (warn at higher layers)
	}
	if fam != req.Family {
		return fmt.Errorf("ci: job %q image %q is family %q, but a step module requires family %q",
			jobName, image, fam, req.Family)
	}
	return nil
}

func imageFamily(image string) string {
	l := strings.ToLower(image)
	switch {
	case strings.Contains(l, "alpine"):
		return "alpine"
	case strings.Contains(l, "ubuntu"), strings.Contains(l, "debian"):
		return "debian"
	case strings.Contains(l, "centos"), strings.Contains(l, "rhel"),
		strings.Contains(l, "fedora"), strings.Contains(l, "rocky"), strings.Contains(l, "alma"):
		return "rhel"
	default:
		return ""
	}
}
