package pipeline

import "slices"

// ---------------------------------------------------------------------------
// Environment Awareness Detection
// ---------------------------------------------------------------------------

// IsEnvironmentAware returns true if the pipeline references environments
// anywhere — top-level, triggers, or steps.
func IsEnvironmentAware(p *Pipeline) bool {
	if len(p.Environments) > 0 {
		return true
	}

	t := &p.Triggers
	if t.Push != nil && len(t.Push.Environments) > 0 {
		return true
	}
	if t.Manual != nil && len(t.Manual.Environments) > 0 {
		return true
	}
	if t.Schedule != nil && len(t.Schedule.Environments) > 0 {
		return true
	}
	if t.Tag != nil && len(t.Tag.Environments) > 0 {
		return true
	}
	if len(t.Promotion) > 0 {
		return true // promotion always targets environments
	}
	if t.Webhook != nil && len(t.Webhook.Environments) > 0 {
		return true
	}

	for _, s := range p.Steps {
		if len(s.Environments) > 0 {
			return true
		}
	}

	return false
}

// ---------------------------------------------------------------------------
// Environment Simulation
// ---------------------------------------------------------------------------

// EnvSimulation is the result of simulating a pipeline for a target environment.
type EnvSimulation struct {
	TargetEnv      string
	ActiveTriggers []TriggerStatus
	Steps          []StepStatus
}

// TriggerStatus describes a trigger's relevance to the target environment.
type TriggerStatus struct {
	Type   string // "push", "pull_request", "manual", etc.
	Active bool
	Reason string
}

// StepStatus describes a step's status for the target environment.
type StepStatus struct {
	Name               string
	Active             bool
	SkipReason         string // why it was skipped (empty if active)
	ExecType           string
	DependsOn          []string // filtered deps (only active steps)
	MatrixCombinations int      // number of matrix combinations (0 if no matrix)
}

// SimulateEnv produces a preview of what would happen for a given target
// environment — which triggers fire, which steps run, and the filtered DAG.
func SimulateEnv(p *Pipeline, targetEnv string) *EnvSimulation {
	sim := &EnvSimulation{TargetEnv: targetEnv}

	// Evaluate triggers.
	sim.ActiveTriggers = simulateTriggers(p, targetEnv)

	// Evaluate steps.
	activeNames := make(map[string]bool, len(p.Steps))
	for _, s := range p.Steps {
		active := len(s.Environments) == 0 || slices.Contains(s.Environments, targetEnv)
		combos := matrixCombinations(s.Matrix)
		status := StepStatus{
			Name:               s.Name,
			Active:             active,
			ExecType:           s.ExecType(),
			MatrixCombinations: combos,
		}
		if !active {
			status.SkipReason = "environments: " + formatList(s.Environments) + " — not this environment"
		}
		if active {
			activeNames[s.Name] = true
		}
		sim.Steps = append(sim.Steps, status)
	}

	// Filter dependsOn for active steps.
	for i := range sim.Steps {
		if !sim.Steps[i].Active {
			continue
		}
		step := findStepByName(p.Steps, sim.Steps[i].Name)
		if step == nil {
			continue
		}
		for _, dep := range step.DependsOn {
			if activeNames[dep] {
				sim.Steps[i].DependsOn = append(sim.Steps[i].DependsOn, dep)
			}
		}
	}

	return sim
}

func simulateTriggers(p *Pipeline, env string) []TriggerStatus {
	var triggers []TriggerStatus

	if p.Triggers.Push != nil {
		active, reason := evaluateTriggerEnv(p.Triggers.Push.Environments, env)
		triggers = append(triggers, TriggerStatus{Type: "push", Active: active, Reason: reason})
	}

	if p.Triggers.PullRequest != nil {
		triggers = append(triggers, TriggerStatus{
			Type:   "pull_request",
			Active: false,
			Reason: "pull request triggers are always plain CI (no environment)",
		})
	}

	if p.Triggers.Manual != nil {
		active, reason := evaluateTriggerEnv(p.Triggers.Manual.Environments, env)
		triggers = append(triggers, TriggerStatus{Type: "manual", Active: active, Reason: reason})
	}

	if p.Triggers.Schedule != nil {
		active, reason := evaluateTriggerEnv(p.Triggers.Schedule.Environments, env)
		triggers = append(triggers, TriggerStatus{Type: "schedule", Active: active, Reason: reason})
	}

	if p.Triggers.Tag != nil {
		active, reason := evaluateTriggerEnv(p.Triggers.Tag.Environments, env)
		triggers = append(triggers, TriggerStatus{Type: "tag", Active: active, Reason: reason})
	}

	for _, promo := range p.Triggers.Promotion {
		active, reason := evaluateTriggerEnv(promo.Environments, env)
		if active {
			reason = "active (from " + promo.From + ")"
		}
		triggers = append(triggers, TriggerStatus{Type: "promotion", Active: active, Reason: reason})
	}

	if p.Triggers.Webhook != nil {
		active, reason := evaluateTriggerEnv(p.Triggers.Webhook.Environments, env)
		triggers = append(triggers, TriggerStatus{Type: "webhook", Active: active, Reason: reason})
	}

	return triggers
}

func findStepByName(steps []Step, name string) *Step {
	for i := range steps {
		if steps[i].Name == name {
			return &steps[i]
		}
	}
	return nil
}

func matrixCombinations(matrix map[string][]string) int {
	if len(matrix) == 0 {
		return 0
	}
	combos := 1
	for _, vals := range matrix {
		combos *= len(vals)
	}
	return combos
}

func formatList(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	return "[" + joinStrings(items, ", ") + "]"
}

func joinStrings(items []string, sep string) string {
	result := ""
	for i, item := range items {
		if i > 0 {
			result += sep
		}
		result += item
	}
	return result
}
