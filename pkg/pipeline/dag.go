package pipeline

import "fmt"

// ResolveDag performs a topological sort of pipeline steps by their after:
// dependencies. It returns waves — groups of steps that can execute in
// parallel. Each wave's dependencies are satisfied by all previous waves.
//
// Returns ErrCycleDetected if the dependency graph contains a cycle.
func ResolveDag(p *Pipeline) ([][]Step, error) {
	stepMap := make(map[string]*Step, len(p.Steps))
	for i := range p.Steps {
		stepMap[p.Steps[i].Name] = &p.Steps[i]
	}

	// Build in-degree and adjacency.
	inDegree := make(map[string]int, len(p.Steps))
	dependents := make(map[string][]string, len(p.Steps))

	for _, s := range p.Steps {
		if _, ok := inDegree[s.Name]; !ok {
			inDegree[s.Name] = 0
		}
		for _, dep := range s.After {
			inDegree[s.Name]++
			dependents[dep] = append(dependents[dep], s.Name)
		}
	}

	// Kahn's algorithm — collect waves instead of a flat list.
	var waves [][]Step
	resolved := make(map[string]bool, len(p.Steps))

	// Seed: all steps with no dependencies.
	var queue []string
	for _, s := range p.Steps {
		if inDegree[s.Name] == 0 {
			queue = append(queue, s.Name)
		}
	}

	for len(queue) > 0 {
		// This entire queue is one parallel wave.
		wave := make([]Step, 0, len(queue))
		for _, name := range queue {
			wave = append(wave, *stepMap[name])
			resolved[name] = true
		}
		waves = append(waves, wave)

		// Find the next wave.
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

	if len(resolved) != len(p.Steps) {
		// Find one step in the cycle for the error message.
		for _, s := range p.Steps {
			if !resolved[s.Name] {
				return nil, fmt.Errorf("%w: involving step %q", ErrCycleDetected, s.Name)
			}
		}
	}

	return waves, nil
}
