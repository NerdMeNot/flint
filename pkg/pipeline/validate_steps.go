package pipeline

import (
	"fmt"
	"slices"
	"strings"
)

// ---------------------------------------------------------------------------
// Step semantic validation
// ---------------------------------------------------------------------------

func validateStepSemantics(s *Step, field string, stepNames map[string]bool, result *ValidationResult, opts ValidateOptions) {
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

	// when: must be a valid value.
	if s.When != "" {
		if !slices.Contains(validWhenValues, s.When) {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:       CodeInvalidValue,
				Field:      field + ".when",
				Message:    fmt.Sprintf("invalid when value %q", s.When),
				Suggestion: "Valid values: onSuccess, onFailure, always",
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
				Suggestion: "Valid values: sh, bash, python",
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
	}

	// Artifact from: should reference a step that has outputs.
	for j, input := range s.Inputs {
		if stepNames[input.From] {
			// Check if referenced step has outputs (best-effort — we only have names here).
			// This would need the full step list to check properly.
			_ = j // future: add cross-reference check
		}
	}

	// Cache key expression validation.
	if s.Cache != nil && s.Cache.Key != "" && strings.Contains(s.Cache.Key, "${{") {
		// Try to compile the cache key as an expression.
		_, err := EvalExpr(s.Cache.Key, ExprContext{
			"hashFiles": func(pattern string) string { return "placeholder" },
		})
		if err != nil && strings.Contains(err.Error(), "compile") {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:     CodeInvalidExpression,
				Field:    field + ".cache.key",
				Message:  "invalid expression in cache key: " + err.Error(),
				Severity: SeverityError,
			})
		}
	}

	// Validate if: expression syntax.
	if s.If != "" {
		_, err := EvalExpr(s.If, ExprContext{
			"branch":      "",
			"commitSha":   "",
			"shortSha":    "",
			"tag":         "",
			"environment": "",
			"triggeredBy": "",
			"triggerType": "",
			"status":      "",
			"project":     map[string]any{"name": "", "repo": ""},
			"run":         map[string]any{"id": ""},
			"inputs":      map[string]any{},
			"env":         map[string]any{},
			"secrets":     map[string]any{},
			"matrix":      map[string]any{},
			"steps":       map[string]any{},
		})
		// Only flag compile errors, not runtime errors (vars won't resolve at validate time).
		if err != nil && strings.Contains(err.Error(), "compile") {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:     CodeInvalidExpression,
				Field:    field + ".if",
				Message:  "invalid expression: " + err.Error(),
				Severity: SeverityError,
			})
		}
	}
}
