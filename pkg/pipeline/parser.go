package pipeline

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// MaxYAMLSize is the maximum allowed YAML file size (1 MB).
const MaxYAMLSize = 1 << 20

// MaxMatrixCombinations limits the cartesian product of matrix dimensions.
const MaxMatrixCombinations = 256 // Cap cartesian product to prevent exponential blowup

// Production hardening limits.
const (
	MaxSteps           = 100
	MaxEnvVarsPerStep  = 50
	MaxServicesPerStep = 10
	MaxRunCommandSize  = 64 << 10 // 64 KB
	MaxStepNameLength  = 128
)

// Parse decodes raw YAML bytes into a Pipeline. It performs structural
// validation (required fields, execution type exclusivity, dependency
// references) but does not run rich validation — use Validate() for that.
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

	// At least one trigger is required.
	if !p.Triggers.HasAny() {
		return nil, newParseError("triggers", "at least one trigger is required")
	}

	// At least one step is required.
	if len(p.Steps) == 0 {
		return nil, newParseError("steps", "at least one step is required")
	}

	// Enforce step count limit.
	if len(p.Steps) > MaxSteps {
		return nil, newParseError("steps", fmt.Sprintf("pipeline has %d steps, maximum is %d", len(p.Steps), MaxSteps))
	}

	// Enforce per-step field size limits.
	for i, s := range p.Steps {
		field := fmt.Sprintf("steps[%d]", i)
		if len(s.Name) > MaxStepNameLength {
			return nil, newParseError(field+".name", fmt.Sprintf("step name is %d characters, maximum is %d", len(s.Name), MaxStepNameLength))
		}
		if len(s.Run) > MaxRunCommandSize {
			return nil, newParseError(field+".run", fmt.Sprintf("run command is %d bytes, maximum is %d", len(s.Run), MaxRunCommandSize))
		}
		if len(s.Env) > MaxEnvVarsPerStep {
			return nil, newParseError(field+".env", fmt.Sprintf("step has %d env vars, maximum is %d", len(s.Env), MaxEnvVarsPerStep))
		}
		if len(s.Services) > MaxServicesPerStep {
			return nil, newParseError(field+".services", fmt.Sprintf("step has %d services, maximum is %d", len(s.Services), MaxServicesPerStep))
		}
	}

	// Validate all steps (including nested sub-steps).
	names := make(map[string]bool, len(p.Steps))
	for i := range p.Steps {
		if err := validateStep(&p.Steps[i], i, names, false); err != nil {
			return nil, err
		}
	}

	// Validate dependsOn references point to existing top-level step names.
	for i, s := range p.Steps {
		for _, dep := range s.DependsOn {
			if !names[dep] {
				return nil, newParseError(
					fmt.Sprintf("steps[%d].dependsOn", i),
					fmt.Sprintf("references unknown step %q", dep),
				)
			}
			if dep == s.Name {
				return nil, newParseError(
					fmt.Sprintf("steps[%d].dependsOn", i),
					"step cannot depend on itself",
				)
			}
		}
	}

	// Validate matrix sizes.
	for i, s := range p.Steps {
		if err := validateMatrix(s.Matrix, i); err != nil {
			return nil, err
		}
	}

	// Validate promotion triggers have required fields.
	for i, promo := range p.Triggers.Promotion {
		if promo.From == "" {
			return nil, newParseError(
				fmt.Sprintf("triggers.promotion[%d].from", i),
				"field is required",
			)
		}
		if len(promo.Environments) == 0 {
			return nil, newParseError(
				fmt.Sprintf("triggers.promotion[%d].environments", i),
				"promotion triggers must specify target environments",
			)
		}
	}

	return &p, nil
}

// validateStep checks a single top-level step for structural correctness.
func validateStep(s *Step, index int, names map[string]bool, _ bool) error {
	field := fmt.Sprintf("steps[%d]", index)

	if s.Name == "" {
		return newParseError(field, "name is required")
	}

	if names[s.Name] {
		return newParseError(field, fmt.Sprintf("duplicate step name %q", s.Name))
	}
	names[s.Name] = true

	// Validate exactly one execution type.
	if err := validateExecType(s, field); err != nil {
		return err
	}

	// Recursively validate nested sub-steps.
	if s.IsNested() {
		subNames := make(map[string]bool, len(s.Steps))
		for i := range s.Steps {
			sub := &s.Steps[i]
			subField := fmt.Sprintf("%s.steps[%d]", field, i)

			// Sub-steps using use: without a name are allowed.
			if sub.Name != "" {
				if subNames[sub.Name] {
					return newParseError(subField, fmt.Sprintf("duplicate sub-step name %q", sub.Name))
				}
				subNames[sub.Name] = true
			}

			if err := validateExecType(sub, subField); err != nil {
				return err
			}

			// Sub-step forbidden fields.
			if sub.Runner != "" {
				return newParseError(subField+".runner", "sub-steps cannot specify a runner (inherited from parent)")
			}
			if len(sub.Inputs) > 0 {
				return newParseError(subField+".inputs", "sub-steps cannot declare artifact inputs (declared on parent)")
			}
			if len(sub.Outputs) > 0 {
				return newParseError(subField+".outputs", "sub-steps cannot declare artifact outputs (declared on parent)")
			}
			if len(sub.DependsOn) > 0 {
				return newParseError(subField+".dependsOn", "sub-steps cannot have dependsOn (they run sequentially)")
			}
			if sub.IsNested() {
				return newParseError(subField+".steps", "sub-steps cannot contain further nested steps")
			}
			if len(sub.Matrix) > 0 {
				return newParseError(subField+".matrix", "sub-steps cannot have matrix (declare on the parent step)")
			}
			if len(sub.Services) > 0 {
				return newParseError(subField+".services", "sub-steps cannot declare services (declare on the parent step)")
			}
			if sub.Cache != nil {
				return newParseError(subField+".cache", "sub-steps cannot declare cache (declare on the parent step)")
			}
		}
	}

	return nil
}

// validateExecType ensures exactly one execution type is set.
func validateExecType(s *Step, field string) error {
	count := 0
	if s.Run != "" {
		count++
	}
	if s.Use != "" {
		count++
	}
	if len(s.Steps) > 0 {
		count++
	}
	if s.Gate != nil {
		count++
	}

	if count == 0 {
		return newParseError(field, "exactly one of run, use, steps, or gate is required")
	}
	if count > 1 {
		return newParseError(field, "only one of run, use, steps, or gate may be set")
	}

	return nil
}

// validateMatrix checks matrix dimensions are non-empty and within limits.
func validateMatrix(matrix map[string][]string, stepIndex int) error {
	if len(matrix) == 0 {
		return nil
	}

	combos := 1
	for key, vals := range matrix {
		if len(vals) == 0 {
			return newParseError(
				fmt.Sprintf("steps[%d].matrix.%s", stepIndex, key),
				"matrix dimension must have at least one value",
			)
		}
		combos *= len(vals)
		if combos > MaxMatrixCombinations {
			return newParseError(
				fmt.Sprintf("steps[%d].matrix", stepIndex),
				fmt.Sprintf("matrix produces %d+ combinations, max is %d", combos, MaxMatrixCombinations),
			)
		}
	}

	return nil
}
