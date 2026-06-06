package pipeline

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// ValidateOptions configures the rich validator.
type ValidateOptions struct {
	// Known environment names. If non-nil, environment references are checked against this list.
	Environments []string
	// Known image preset names. If non-nil and PresetsOnly is true, image references are checked.
	ImagePresets []string
	// If true, only preset names are valid in the image: field.
	PresetsOnly bool
	// TemplateResolver for checking use: references. Optional.
	Resolver TemplateResolver
	// RequireImage, when true, flags run: steps that have no image and no
	// pipeline-level default image as an error. Enable this in environments where
	// every step must declare an explicit image.
	RequireImage bool

	// pipelineImage is the pipeline-level default image, propagated internally
	// during step validation so steps can inherit it.
	pipelineImage string
}

// Validate performs rich validation of pipeline YAML with line-level errors.
// It uses yaml.Node decoding to preserve position information.
// This is more thorough than Parse() — it checks semantic correctness,
// environment consistency, trigger compatibility, and more.
func Validate(data []byte, opts ValidateOptions) *ValidationResult {
	result := &ValidationResult{}

	if len(data) == 0 {
		result.Issues = append(result.Issues, ValidationIssue{
			Code:     CodeSyntaxError,
			Message:  "empty pipeline YAML",
			Severity: SeverityError,
		})
		return result
	}
	if len(data) > MaxYAMLSize {
		result.Issues = append(result.Issues, ValidationIssue{
			Code:     CodeBoundsExceeded,
			Message:  fmt.Sprintf("pipeline YAML exceeds %d bytes", MaxYAMLSize),
			Severity: SeverityError,
		})
		return result
	}

	// Parse into yaml.Node tree for position info.
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		result.Issues = append(result.Issues, ValidationIssue{
			Code:     CodeSyntaxError,
			Message:  "invalid YAML syntax: " + err.Error(),
			Severity: SeverityError,
		})
		return result
	}

	// Also parse into the structured type for semantic validation.
	p, parseErr := Parse(data)
	if parseErr != nil {
		// Convert ParseError to ValidationIssue.
		if pe, ok := parseErr.(*ParseError); ok {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:       CodeSyntaxError,
				Line:       pe.Line,
				Column:     pe.Column,
				Field:      pe.Field,
				Message:    pe.Message,
				Suggestion: pe.Suggestion,
				Severity:   SeverityError,
			})
		} else {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:     CodeSyntaxError,
				Message:  parseErr.Error(),
				Severity: SeverityError,
			})
		}
		return result
	}

	// Collect all step names for suggestion matching.
	stepNames := collectStepNames(p)
	stepsWithOutputs := collectStepsWithOutputs(p)

	// --- Trigger validation ---
	validateTriggers(p, result, opts)

	// --- Step validation ---
	// Propagate pipeline-level image into options so step validation can check
	// whether a run: step has at least one image source.
	stepOpts := opts
	stepOpts.pipelineImage = p.Image
	for i, s := range p.Steps {
		field := fmt.Sprintf("steps[%d]", i)
		validateStepSemantics(&s, field, stepNames, stepsWithOutputs, result, stepOpts)
	}

	// --- DAG validation ---
	_, dagErr := ResolveDag(p)
	if dagErr != nil {
		result.Issues = append(result.Issues, ValidationIssue{
			Code:     CodeCycleDetected,
			Field:    "steps",
			Message:  dagErr.Error(),
			Severity: SeverityError,
		})
	}

	// --- Trigger compatibility ---
	validateTriggerCompatibility(p, result)

	// --- Environment consistency ---
	validateEnvironments(p, result, opts)

	return result
}
