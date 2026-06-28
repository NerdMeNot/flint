package pipeline

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Step semantic validation
// ---------------------------------------------------------------------------

func validateStepSemantics(s *Step, field string, stepNames, stepsWithOutputs map[string]bool, result *ValidationResult, opts ValidateOptions) {
	// Gate steps should not have container/execution-related fields.
	if s.Gate != nil {
		gateRestricted := []struct {
			name string
			set  bool
		}{
			{"image", s.Image != ""},
			{"runner", s.Runner != ""},
			{"shell", s.Shell != ""},
			{"workingDir", s.WorkingDir != ""},
			{"matrix", len(s.Matrix) > 0},
			{"services", len(s.Services) > 0},
			{"cache", s.Cache != nil},
			{"retry", s.Retry != nil},
		}
		for _, r := range gateRestricted {
			if r.set {
				result.Issues = append(result.Issues, ValidationIssue{
					Code:     CodeInvalidValue,
					Field:    field + "." + r.name,
					Message:  fmt.Sprintf("gate steps cannot have %s (gates are approval checkpoints, not execution steps)", r.name),
					Severity: SeverityError,
				})
			}
		}
	}

	// Wait steps are pauses for an external signal, not execution steps — they
	// carry no container/execution fields, and their timeout must parse.
	if s.Wait != nil {
		waitRestricted := []struct {
			name string
			set  bool
		}{
			{"image", s.Image != ""},
			{"run", !s.Run.IsEmpty()},
			{"use", s.Use != ""},
			{"runner", s.Runner != ""},
			{"matrix", len(s.Matrix) > 0},
			{"services", len(s.Services) > 0},
			{"cache", s.Cache != nil},
		}
		for _, r := range waitRestricted {
			if r.set {
				result.Issues = append(result.Issues, ValidationIssue{
					Code:     CodeInvalidValue,
					Field:    field + "." + r.name,
					Message:  fmt.Sprintf("wait steps cannot have %s (wait steps pause for an external signal)", r.name),
					Severity: SeverityError,
				})
			}
		}
		if s.Wait.Timeout != "" {
			if _, err := time.ParseDuration(s.Wait.Timeout); err != nil {
				result.Issues = append(result.Issues, ValidationIssue{
					Code:     CodeInvalidValue,
					Field:    field + ".wait.timeout",
					Message:  fmt.Sprintf("invalid wait timeout %q", s.Wait.Timeout),
					Severity: SeverityError,
				})
			}
		}
	}

	// dependsOn: check for typos with suggestions.
	for j, dep := range s.DependsOn {
		if !stepNames[dep] {
			suggestion := findClosest(dep, stepNames)
			issue := ValidationIssue{
				Code:     CodeUnknownRef,
				Field:    fmt.Sprintf("%s.dependsOn[%d]", field, j),
				Message:  fmt.Sprintf("references unknown step %q", dep),
				Severity: SeverityError,
			}
			if suggestion != "" {
				issue.Suggestion = fmt.Sprintf("Did you mean %q?", suggestion)
			}
			result.Issues = append(result.Issues, issue)
		}
	}

	// use: catch malformed cross-repo references that would otherwise be
	// silently treated as a (nonexistent) CRD template name.
	if s.Use != "" {
		if msg := malformedCrossRepoRef(s.Use); msg != "" {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:       CodeInvalidValue,
				Field:      field + ".use",
				Message:    msg,
				Suggestion: "Cross-repo refs are org/repo/path@ref; local files start with ./; otherwise it's a step template name",
				Severity:   SeverityError,
			})
		}
	}

	// when: must be a valid value.
	if s.When != "" {
		if !slices.Contains(validWhenValues, s.When) {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:       CodeInvalidValue,
				Field:      field + ".when",
				Message:    fmt.Sprintf("invalid when value %q", s.When),
				Suggestion: enumSuggestion(s.When, validWhenValues),
				Severity:   SeverityError,
			})
		}
	}

	// shell: must be a valid value.
	if s.Shell != "" {
		if !slices.Contains(validShells, s.Shell) {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:       CodeInvalidValue,
				Field:      field + ".shell",
				Message:    fmt.Sprintf("invalid shell %q", s.Shell),
				Suggestion: enumSuggestion(s.Shell, validShells),
				Severity:   SeverityError,
			})
		}
	}

	// image: check against presets if presetsOnly is enabled.
	if s.Image != "" && opts.PresetsOnly && len(opts.ImagePresets) > 0 {
		if !slices.Contains(opts.ImagePresets, s.Image) {
			suggestion := findClosest(s.Image, toSet(opts.ImagePresets))
			issue := ValidationIssue{
				Code:     CodeUnknownRef,
				Field:    field + ".image",
				Message:  fmt.Sprintf("image %q is not a known preset", s.Image),
				Severity: SeverityError,
			}
			if suggestion != "" {
				issue.Suggestion = fmt.Sprintf("Did you mean %q?", suggestion)
			}
			result.Issues = append(result.Issues, issue)
		}
	}

	// image: when RequireImage is set, run: steps must have an image somewhere
	// in the inheritance chain (step-level or pipeline-level default).
	if opts.RequireImage && s.Gate == nil && len(s.Steps) == 0 && !s.Run.IsEmpty() {
		if s.Image == "" && opts.pipelineImage == "" {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:       CodeMissingField,
				Field:      field + ".image",
				Message:    "run steps must specify an image (no pipeline-level default is set)",
				Suggestion: "Add `image:` to this step or set a top-level `image:` default on the pipeline",
				Severity:   SeverityError,
			})
		}
	}

	// Artifact inputs: from references must be valid step names.
	for j, input := range s.Inputs {
		if !stepNames[input.From] {
			suggestion := findClosest(input.From, stepNames)
			issue := ValidationIssue{
				Code:     CodeUnknownRef,
				Field:    fmt.Sprintf("%s.inputs[%d].from", field, j),
				Message:  fmt.Sprintf("references unknown step %q", input.From),
				Severity: SeverityError,
			}
			if suggestion != "" {
				issue.Suggestion = fmt.Sprintf("Did you mean %q?", suggestion)
			}
			result.Issues = append(result.Issues, issue)
		}
	}

	// Gate: validate approver format and bounds.
	if s.Gate != nil {
		if len(s.Gate.Approvers) == 0 {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:     CodeMissingField,
				Field:    field + ".gate.approvers",
				Message:  "gate must have at least one approver",
				Severity: SeverityError,
			})
		}
		for j, approver := range s.Gate.Approvers {
			if !isValidApprover(approver) {
				result.Issues = append(result.Issues, ValidationIssue{
					Code:       CodeInvalidApprover,
					Field:      fmt.Sprintf("%s.gate.approvers[%d]", field, j),
					Message:    fmt.Sprintf("invalid approver format %q", approver),
					Suggestion: "Use role:slug, team:slug, or user@email.com",
					Severity:   SeverityError,
				})
			}
		}
		minApprovals := s.Gate.MinApprovals
		if minApprovals == 0 {
			minApprovals = 1 // default
		}
		if minApprovals > len(s.Gate.Approvers) && len(s.Gate.Approvers) > 0 {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:     CodeBoundsExceeded,
				Field:    field + ".gate.minApprovals",
				Message:  fmt.Sprintf("minApprovals (%d) exceeds number of approvers (%d)", minApprovals, len(s.Gate.Approvers)),
				Severity: SeverityError,
			})
		}
	}

	// Timeout: validate duration format.
	if s.Timeout != "" {
		if _, err := parseDuration(s.Timeout); err != nil {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:       CodeInvalidValue,
				Field:      field + ".timeout",
				Message:    fmt.Sprintf("invalid duration %q", s.Timeout),
				Suggestion: "Use Go duration format: 30s, 5m, 1h, 2h30m",
				Severity:   SeverityError,
			})
		}
	}

	// Retry: validate delay duration and attempts.
	if s.Retry != nil {
		if s.Retry.Attempts < 1 {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:     CodeBoundsExceeded,
				Field:    field + ".retry.attempts",
				Message:  "retry attempts must be at least 1",
				Severity: SeverityError,
			})
		}
		if s.Retry.Delay != "" {
			if _, err := parseDuration(s.Retry.Delay); err != nil {
				result.Issues = append(result.Issues, ValidationIssue{
					Code:       CodeInvalidValue,
					Field:      field + ".retry.delay",
					Message:    fmt.Sprintf("invalid duration %q", s.Retry.Delay),
					Suggestion: "Use Go duration format: 5s, 30s, 1m",
					Severity:   SeverityError,
				})
			}
		}
	}

	// Env variable name format.
	for k := range s.Env {
		if !isValidEnvName(k) {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:     CodeInvalidValue,
				Field:    field + ".env." + k,
				Message:  fmt.Sprintf("environment variable name %q should match [A-Za-z_][A-Za-z0-9_]*", k),
				Severity: SeverityWarning,
			})
		}
	}

	// Matrix dimension keys should not shadow built-in context variables.
	for key := range s.Matrix {
		if reservedContextVars[key] {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:     CodeInvalidValue,
				Field:    field + ".matrix." + key,
				Message:  fmt.Sprintf("matrix dimension %q shadows a built-in context variable", key),
				Severity: SeverityWarning,
			})
		}
	}

	// Step name length.
	if len(s.Name) > MaxStepNameLength {
		result.Issues = append(result.Issues, ValidationIssue{
			Code:     CodeBoundsExceeded,
			Field:    field + ".name",
			Message:  fmt.Sprintf("step name is %d characters, maximum is %d", len(s.Name), MaxStepNameLength),
			Severity: SeverityError,
		})
	}

	// Artifact path validation.
	for j, input := range s.Inputs {
		if input.Path != "" && !strings.HasPrefix(input.Path, "/") {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:     CodeInvalidValue,
				Field:    fmt.Sprintf("%s.inputs[%d].path", field, j),
				Message:  "artifact path must be absolute (start with /)",
				Severity: SeverityError,
			})
		}
		if strings.Contains(input.Path, "..") {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:     CodeInvalidValue,
				Field:    fmt.Sprintf("%s.inputs[%d].path", field, j),
				Message:  "artifact path must not contain '..'",
				Severity: SeverityError,
			})
		}
	}
	for j, output := range s.Outputs {
		if output.Path != "" && !strings.HasPrefix(output.Path, "/") {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:     CodeInvalidValue,
				Field:    fmt.Sprintf("%s.outputs[%d].path", field, j),
				Message:  "artifact path must be absolute (start with /)",
				Severity: SeverityError,
			})
		}
		if strings.Contains(output.Path, "..") {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:     CodeInvalidValue,
				Field:    fmt.Sprintf("%s.outputs[%d].path", field, j),
				Message:  "artifact path must not contain '..'",
				Severity: SeverityError,
			})
		}
	}

	// Artifact from: a referenced step should actually declare outputs.
	for j, input := range s.Inputs {
		if stepNames[input.From] && !stepsWithOutputs[input.From] {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:     CodeUnknownRef,
				Field:    fmt.Sprintf("%s.inputs[%d].from", field, j),
				Message:  fmt.Sprintf("step %q declares no outputs to consume", input.From),
				Severity: SeverityWarning,
			})
		}
	}

	// Cache key expression validation (compile-only — never evaluated here).
	if s.Cache != nil && s.Cache.Key != "" && strings.Contains(s.Cache.Key, "${{") {
		if err := compileExpr(s.Cache.Key, exprValidationContext()); err != nil {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:     CodeInvalidExpression,
				Field:    field + ".cache.key",
				Message:  "invalid expression in cache key: " + err.Error(),
				Severity: SeverityError,
			})
		}
	}

	// Validate if: expression syntax (compile-only).
	if s.If != "" {
		if err := compileExpr(s.If, exprValidationContext()); err != nil {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:     CodeInvalidExpression,
				Field:    field + ".if",
				Message:  "invalid expression: " + err.Error(),
				Severity: SeverityError,
			})
		}
	}
}
