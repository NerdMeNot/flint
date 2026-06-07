package ci

import (
	"fmt"
	"sort"
)

// Validate checks the structural invariants the compiler and engine assume.
// It does not resolve modules (use:/extends:) — that happens before Validate on
// the merged pipeline.
func (p *Pipeline) Validate() error {
	// A pipeline either defines jobs or extends a pipeline module. Once extends
	// is resolved the merged pipeline must still satisfy the job rules below.
	if p.Extends == "" && len(p.Jobs) == 0 {
		return fmt.Errorf("ci: pipeline must define at least one job (or extends a pipeline module)")
	}
	if !p.Triggers.HasAny() {
		return fmt.Errorf("ci: pipeline must define at least one trigger")
	}
	if p.Concurrency != nil && p.Concurrency.Group == "" {
		return fmt.Errorf("ci: pipeline concurrency requires a group")
	}

	envSet := toSet(p.Environments)

	for _, name := range sortedJobNames(p.Jobs) {
		job := p.Jobs[name]
		if err := p.validateJob(name, job, envSet); err != nil {
			return err
		}
	}

	if err := p.validateNeeds(); err != nil {
		return err
	}
	return nil
}

func (p *Pipeline) validateJob(name string, job Job, pipelineEnvs map[string]bool) error {
	// A job that pulls its body from a module defers body checks until resolved.
	if job.Use == "" {
		if len(job.Steps) > 0 && job.Gate != nil {
			return fmt.Errorf("ci: job %q sets both steps and gate (exactly one)", name)
		}
		switch job.ExecType() {
		case "":
			return fmt.Errorf("ci: job %q must define steps or a gate", name)
		case "steps":
			// Container jobs must resolve an image (job or pipeline default).
			if job.Image == "" && p.Image == "" {
				return fmt.Errorf("ci: job %q has no image (set job.image or top-level image)", name)
			}
			for i, s := range job.Steps {
				if err := validateStep(name, i, s); err != nil {
					return err
				}
			}
		case "gate":
			// gate jobs need no image, steps, or resources.
		}
	}

	// Environment narrowing: job environments must be a subset of the pipeline's.
	if len(pipelineEnvs) > 0 {
		for _, e := range job.Environments {
			if !pipelineEnvs[e] {
				return fmt.Errorf("ci: job %q targets environment %q not allowed by the pipeline", name, e)
			}
		}
	}

	for _, s := range job.Secrets {
		if err := validateSecret(fmt.Sprintf("job %q", name), s); err != nil {
			return err
		}
	}
	if job.Resources != nil && job.Resources.Limits != nil &&
		job.Resources.CPU == "" && job.Resources.Memory == "" {
		return fmt.Errorf("ci: job %q sets resource limits without requests", name)
	}
	if job.Concurrency != nil && job.Concurrency.Group == "" {
		return fmt.Errorf("ci: job %q concurrency requires a group", name)
	}
	return nil
}

func validateStep(job string, idx int, s Step) error {
	// Exactly one of run | use | inject.
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
		return fmt.Errorf("ci: job %q step %d must set exactly one of run, use, or inject", job, idx)
	}
	for _, sec := range s.Secrets {
		if err := validateSecret(fmt.Sprintf("job %q step %d", job, idx), sec); err != nil {
			return err
		}
	}
	return nil
}

func validateSecret(where string, s Secret) error {
	sources := 0
	if s.Name != "" {
		sources++
	}
	if s.From != "" {
		sources++
	}
	if sources != 1 {
		return fmt.Errorf("ci: %s secret must set exactly one source (name or from)", where)
	}
	targets := 0
	if s.Env != "" {
		targets++
	}
	if s.File != "" {
		targets++
	}
	if targets != 1 {
		return fmt.Errorf("ci: %s secret must set exactly one target (env or file)", where)
	}
	if s.Mode != "" && s.File == "" {
		return fmt.Errorf("ci: %s secret sets mode without a file target", where)
	}
	return nil
}

// validateNeeds checks that every need references an existing job and that the
// job graph is acyclic.
func (p *Pipeline) validateNeeds() error {
	for _, name := range sortedJobNames(p.Jobs) {
		for _, dep := range p.Jobs[name].Needs {
			if _, ok := p.Jobs[dep]; !ok {
				return fmt.Errorf("ci: job %q needs unknown job %q", name, dep)
			}
		}
	}
	return p.detectCycle()
}

func (p *Pipeline) detectCycle() error {
	const (
		white = 0 // unvisited
		gray  = 1 // on the current DFS stack
		black = 2 // done
	)
	color := make(map[string]int, len(p.Jobs))
	var visit func(string) error
	visit = func(n string) error {
		color[n] = gray
		deps := append([]string(nil), p.Jobs[n].Needs...)
		sort.Strings(deps)
		for _, d := range deps {
			switch color[d] {
			case gray:
				return fmt.Errorf("ci: dependency cycle through job %q", d)
			case white:
				if err := visit(d); err != nil {
					return err
				}
			}
		}
		color[n] = black
		return nil
	}
	for _, n := range sortedJobNames(p.Jobs) {
		if color[n] == white {
			if err := visit(n); err != nil {
				return err
			}
		}
	}
	return nil
}

func sortedJobNames(jobs map[string]Job) []string {
	names := make([]string, 0, len(jobs))
	for n := range jobs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func toSet(items []string) map[string]bool {
	if len(items) == 0 {
		return nil
	}
	m := make(map[string]bool, len(items))
	for _, i := range items {
		m[i] = true
	}
	return m
}
