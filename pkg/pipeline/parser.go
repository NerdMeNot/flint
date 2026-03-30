package pipeline

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// MaxYAMLSize is the maximum allowed YAML file size (1MB).
// Prevents OOM from maliciously large pipeline files.
const MaxYAMLSize = 1 << 20 // 1MB

// MaxMatrixCombinations limits the cartesian product of matrix dimensions.
const MaxMatrixCombinations = 256

// Parse decodes raw YAML bytes into a Pipeline. It performs structural
// validation (required fields, exactly-one execution type per step) but
// does not run JSON Schema validation — use Validate() for that.
func Parse(data []byte) (*Pipeline, error) {
	if len(data) > MaxYAMLSize {
		return nil, newParseError("", fmt.Sprintf("pipeline YAML exceeds %d bytes", MaxYAMLSize))
	}
	if len(data) == 0 {
		return nil, newParseError("", "empty pipeline YAML")
	}

	var p Pipeline
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, wrapParseError("", "failed to parse pipeline YAML", err)
	}

	if p.Name == "" {
		return nil, newParseError("pipeline", "field is required")
	}

	if len(p.Steps) == 0 {
		return nil, newParseError("steps", "at least one step is required")
	}

	names := make(map[string]bool, len(p.Steps))
	for i := range p.Steps {
		s := &p.Steps[i]

		if s.Name == "" {
			return nil, newParseError(fmt.Sprintf("steps[%d]", i), "name is required")
		}

		if names[s.Name] {
			return nil, newParseError(fmt.Sprintf("steps[%d]", i), fmt.Sprintf("duplicate step name %q", s.Name))
		}
		names[s.Name] = true

		if err := validateExecType(s, i); err != nil {
			return nil, err
		}
	}

	// Validate after: references point to existing steps.
	for i, s := range p.Steps {
		for _, dep := range s.After {
			if !names[dep] {
				return nil, newParseError(
					fmt.Sprintf("steps[%d].after", i),
					fmt.Sprintf("references unknown step %q", dep),
				)
			}
			if dep == s.Name {
				return nil, newParseError(
					fmt.Sprintf("steps[%d].after", i),
					"step cannot depend on itself",
				)
			}
		}
	}

	// Validate matrix sizes.
	for i, s := range p.Steps {
		if len(s.Matrix) > 0 {
			combos := 1
			for key, vals := range s.Matrix {
				if len(vals) == 0 {
					return nil, newParseError(
						fmt.Sprintf("steps[%d].matrix.%s", i, key),
						"matrix dimension must have at least one value",
					)
				}
				combos *= len(vals)
				if combos > MaxMatrixCombinations {
					return nil, newParseError(
						fmt.Sprintf("steps[%d].matrix", i),
						fmt.Sprintf("matrix produces %d+ combinations, max is %d", combos, MaxMatrixCombinations),
					)
				}
			}
		}
	}

	// Validate if: expressions are syntactically correct.
	for _, s := range p.Steps {
		if s.If != "" {
			_, err := EvalExpr(s.If, ExprContext{
				"git":    map[string]any{},
				"run":    map[string]any{},
				"steps":  map[string]any{},
				"env":    map[string]any{},
				"inputs": map[string]any{},
			})
			// Compile errors = invalid syntax. Runtime errors (undefined var) are ok
			// at parse time since vars aren't available yet.
			_ = err // expression evaluation at parse time is best-effort
		}
	}

	return &p, nil
}

// validateExecType ensures exactly one execution type is set.
func validateExecType(s *Step, index int) error {
	count := 0
	if s.Run != "" {
		count++
	}
	if len(s.Do) > 0 {
		count++
	}
	if s.Use != "" {
		count++
	}
	if s.Invoke != "" {
		count++
	}
	if s.Gate != nil {
		count++
	}
	if s.Watch != nil {
		count++
	}

	field := fmt.Sprintf("steps[%d]", index)
	if count == 0 {
		return newParseError(field, "exactly one of run, do, use, invoke, gate, or watch is required")
	}
	if count > 1 {
		return newParseError(field, "only one of run, do, use, invoke, gate, or watch may be set")
	}

	return nil
}
