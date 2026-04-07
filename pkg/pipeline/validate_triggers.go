package pipeline

import (
	"fmt"
	"slices"
	"strings"
)

// ---------------------------------------------------------------------------
// Trigger validation
// ---------------------------------------------------------------------------

func validateTriggers(p *Pipeline, result *ValidationResult, opts ValidateOptions) {
	isEnvAware := IsEnvironmentAware(p)

	// If env-aware, automated triggers must have environments specified.
	if isEnvAware {
		if p.Triggers.Push != nil && len(p.Triggers.Push.Environments) == 0 {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:     CodeMissingField,
				Field:    "triggers.push.environments",
				Message:  "environment-aware pipeline requires environments on push trigger",
				Severity: SeverityWarning,
			})
		}
		if p.Triggers.Schedule != nil && len(p.Triggers.Schedule.Environments) == 0 {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:     CodeMissingField,
				Field:    "triggers.schedule.environments",
				Message:  "environment-aware pipeline requires environments on schedule trigger",
				Severity: SeverityWarning,
			})
		}
		if p.Triggers.Tag != nil && len(p.Triggers.Tag.Environments) == 0 {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:     CodeMissingField,
				Field:    "triggers.tag.environments",
				Message:  "environment-aware pipeline requires environments on tag trigger",
				Severity: SeverityWarning,
			})
		}
	}

	// Manual inputs: if pipeline has automated triggers, inputs must have defaults.
	if p.Triggers.Manual != nil && len(p.Triggers.Manual.Inputs) > 0 {
		hasAutomated := p.Triggers.Push != nil || p.Triggers.Schedule != nil ||
			p.Triggers.Tag != nil || len(p.Triggers.Promotion) > 0 || p.Triggers.Webhook != nil
		if hasAutomated {
			for i, input := range p.Triggers.Manual.Inputs {
				if input.Default == "" && input.Required {
					result.Issues = append(result.Issues, ValidationIssue{
						Code:     CodeTriggerConflict,
						Field:    fmt.Sprintf("triggers.manual.inputs[%d]", i),
						Message:  fmt.Sprintf("input %q has no default but pipeline has automated triggers", input.Name),
						Severity: SeverityError,
					})
				}
			}
		}
	}

	// Promotion: check for duplicate from environments.
	promoFromSeen := make(map[string]bool)
	for i, promo := range p.Triggers.Promotion {
		if promoFromSeen[promo.From] {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:     CodeDuplicateField,
				Field:    fmt.Sprintf("triggers.promotion[%d].from", i),
				Message:  fmt.Sprintf("duplicate promotion from environment %q", promo.From),
				Severity: SeverityError,
			})
		}
		promoFromSeen[promo.From] = true
	}

	// Promotion: from and environments cannot overlap.
	for i, promo := range p.Triggers.Promotion {
		if slices.Contains(promo.Environments, promo.From) {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:     CodeTriggerConflict,
				Field:    fmt.Sprintf("triggers.promotion[%d]", i),
				Message:  fmt.Sprintf("promotion from %q cannot target the same environment", promo.From),
				Severity: SeverityError,
			})
		}
		// requireStatus must be a valid value.
		if promo.RequireStatus != "" {
			if !slices.Contains(validPromotionStatuses, promo.RequireStatus) {
				result.Issues = append(result.Issues, ValidationIssue{
					Code:       CodeInvalidValue,
					Field:      fmt.Sprintf("triggers.promotion[%d].requireStatus", i),
					Message:    fmt.Sprintf("invalid requireStatus %q", promo.RequireStatus),
					Suggestion: "Valid values: succeeded, failed",
					Severity:   SeverityError,
				})
			}
		}
	}

	// Manual input type validation.
	if p.Triggers.Manual != nil {
		for i, input := range p.Triggers.Manual.Inputs {
			field := fmt.Sprintf("triggers.manual.inputs[%d]", i)
			// Type must be valid.
			if !slices.Contains(validInputTypes, input.Type) {
				result.Issues = append(result.Issues, ValidationIssue{
					Code:       CodeInvalidValue,
					Field:      field + ".type",
					Message:    fmt.Sprintf("invalid input type %q", input.Type),
					Suggestion: "Valid types: string, boolean, choice",
					Severity:   SeverityError,
				})
			}
			// Choice type must have options.
			if input.Type == "choice" && len(input.Options) == 0 {
				result.Issues = append(result.Issues, ValidationIssue{
					Code:     CodeMissingField,
					Field:    field + ".options",
					Message:  "choice input must have at least one option",
					Severity: SeverityError,
				})
			}
		}
	}

	// Schedule: basic cron validation.
	if p.Triggers.Schedule != nil && p.Triggers.Schedule.Cron != "" {
		parts := strings.Fields(p.Triggers.Schedule.Cron)
		if len(parts) != 5 {
			result.Issues = append(result.Issues, ValidationIssue{
				Code:       CodeInvalidValue,
				Field:      "triggers.schedule.cron",
				Message:    fmt.Sprintf("cron expression has %d fields, expected 5", len(parts)),
				Suggestion: "Format: minute hour day-of-month month day-of-week (e.g. \"0 2 * * 1-5\")",
				Severity:   SeverityError,
			})
		}
	}
}

// validateTriggerCompatibility checks for invalid trigger combinations.
func validateTriggerCompatibility(p *Pipeline, result *ValidationResult) {
	// Count trigger types (excluding promotion which allows multiples).
	types := make(map[string]int)
	if p.Triggers.Push != nil {
		types["push"]++
	}
	if p.Triggers.PullRequest != nil {
		types["pull_request"]++
	}
	if p.Triggers.Manual != nil {
		types["manual"]++
	}
	if p.Triggers.Schedule != nil {
		types["schedule"]++
	}
	if p.Triggers.Tag != nil {
		types["tag"]++
	}
	if p.Triggers.Webhook != nil {
		types["webhook"]++
	}
	// Promotions are counted separately (multiples allowed).

	// No duplicate triggers of the same type (except promotion — handled by YAML structure).
	// Since our YAML decoding puts each trigger as a single pointer, duplicates aren't possible
	// in the Go struct. This is inherently safe.

	// Warning: pull_request + promotion is unusual.
	if types["pull_request"] > 0 && len(p.Triggers.Promotion) > 0 {
		result.Issues = append(result.Issues, ValidationIssue{
			Code:     CodeTriggerConflict,
			Field:    "triggers",
			Message:  "pull_request and promotion triggers in the same pipeline is unusual — PRs are CI, promotions are CD",
			Severity: SeverityWarning,
		})
	}
}
