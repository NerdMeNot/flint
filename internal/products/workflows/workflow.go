// Package workflows is the Flint Workflows product: generic declarative
// workflows on the shared engine, with no forge and no CI triggers.
//
// A workflow definition reuses the engine's step IR (pkg/pipeline.Step) and the
// shared DAG resolver, producing execution waves that are handed to
// engine.StartWorkflowWithWaves. The engine executes them generically — it never
// learns that these came from a "workflow" rather than a CI pipeline.
//
// This is the first product under internal/products. Per the family layering it
// may depend on internal/core and pkg/*, but never on another product.
package workflows

import (
	"fmt"

	"github.com/NerdMeNot/flint/pkg/pipeline"
	"gopkg.in/yaml.v3"
)

// Definition is a workflow: a name and a DAG of steps. Unlike a CI pipeline it
// has no triggers, environments, or forge coupling — a workflow document
// describes only the work; how it's triggered is attached at run time.
type Definition struct {
	Name  string          `yaml:"name" json:"name"`
	Steps []pipeline.Step `yaml:"steps" json:"steps"`
}

// Parse decodes and validates a workflow definition from YAML.
func Parse(data []byte) (*Definition, error) {
	var def Definition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("workflows: parse: %w", err)
	}
	if err := def.Validate(); err != nil {
		return nil, err
	}
	return &def, nil
}

// Validate checks the structural invariants the DAG resolver assumes: a name, at
// least one uniquely-named step, and dependsOn references that exist.
func (d *Definition) Validate() error {
	if d.Name == "" {
		return fmt.Errorf("workflows: name is required")
	}
	if len(d.Steps) == 0 {
		return fmt.Errorf("workflows: at least one step is required")
	}
	seen := make(map[string]bool, len(d.Steps))
	for _, s := range d.Steps {
		if s.Name == "" {
			return fmt.Errorf("workflows: every step needs a name")
		}
		if seen[s.Name] {
			return fmt.Errorf("workflows: duplicate step name %q", s.Name)
		}
		seen[s.Name] = true
	}
	for _, s := range d.Steps {
		for _, dep := range s.DependsOn {
			if !seen[dep] {
				return fmt.Errorf("workflows: step %q depends on unknown step %q", s.Name, dep)
			}
		}
	}
	return nil
}

// Resolve produces the execution waves for the engine, reusing the shared DAG
// resolver. The result is passed to engine.StartWorkflowWithWaves.
func (d *Definition) Resolve() ([][]pipeline.Step, error) {
	waves, err := pipeline.ResolveDag(&pipeline.Pipeline{Steps: d.Steps})
	if err != nil {
		return nil, fmt.Errorf("workflows: resolve DAG: %w", err)
	}
	return waves, nil
}
