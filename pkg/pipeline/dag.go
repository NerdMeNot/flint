package pipeline

import (
	"fmt"
	"slices"
)

// ResolveDag performs a topological sort of pipeline steps by their dependsOn
// dependencies. It returns waves — groups of steps that can execute in
// parallel. Each wave's dependencies are satisfied by all previous waves.
//
// Nested steps (steps with sub-steps) are treated as single nodes in the DAG.
// Sub-steps are not part of the dependency graph.
//
// Returns ErrCycleDetected if the dependency graph contains a cycle.
func ResolveDag(p *Pipeline) ([][]Step, error) {
	return resolveDag(p.Steps)
}

// ResolveDagForEnv resolves the DAG for a specific target environment.
// Steps whose Environments field doesn't include the target are filtered out.
// Dependencies on filtered steps are removed. If env is empty, all steps
// are included (equivalent to ResolveDag).
func ResolveDagForEnv(p *Pipeline, env string) ([][]Step, error) {
	if env == "" {
		return resolveDag(p.Steps)
	}

	// Filter steps to only those active for this environment.
	var active []Step
	activeNames := make(map[string]bool)
	for _, s := range p.Steps {
		if len(s.Environments) == 0 || slices.Contains(s.Environments, env) {
			active = append(active, s)
			activeNames[s.Name] = true
		}
	}

	// Remove dependencies on filtered-out steps.
	for i := range active {
		var filtered []string
		for _, dep := range active[i].DependsOn {
			if activeNames[dep] {
				filtered = append(filtered, dep)
			}
		}
		active[i].DependsOn = filtered
	}

	return resolveDag(active)
}

// resolveDag implements Kahn's topological sort, returning waves of parallel steps.
func resolveDag(steps []Step) ([][]Step, error) {
	stepMap := make(map[string]*Step, len(steps))
	for i := range steps {
		stepMap[steps[i].Name] = &steps[i]
	}

	// Build in-degree and adjacency.
	inDegree := make(map[string]int, len(steps))
	dependents := make(map[string][]string, len(steps))

	for _, s := range steps {
		if _, ok := inDegree[s.Name]; !ok {
			inDegree[s.Name] = 0
		}
		for _, dep := range s.DependsOn {
			inDegree[s.Name]++
			dependents[dep] = append(dependents[dep], s.Name)
		}
	}

	// Kahn's algorithm — collect waves instead of a flat list.
	var waves [][]Step
	resolved := make(map[string]bool, len(steps))

	// Seed: all steps with no dependencies.
	var queue []string
	for _, s := range steps {
		if inDegree[s.Name] == 0 {
			queue = append(queue, s.Name)
		}
	}

	for len(queue) > 0 {
		wave := make([]Step, 0, len(queue))
		for _, name := range queue {
			wave = append(wave, *stepMap[name])
			resolved[name] = true
		}
		waves = append(waves, wave)

		var next []string
		for _, name := range queue {
			for _, dep := range dependents[name] {
				inDegree[dep]--
				if inDegree[dep] == 0 {
					next = append(next, dep)
				}
			}
		}
		queue = next
	}

	if len(resolved) != len(steps) {
		for _, s := range steps {
			if !resolved[s.Name] {
				return nil, fmt.Errorf("%w: involving step %q", ErrCycleDetected, s.Name)
			}
		}
	}

	return waves, nil
}
