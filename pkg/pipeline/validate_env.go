package pipeline

import (
	"fmt"
	"slices"
	"strings"
)

// ---------------------------------------------------------------------------
// Environment consistency validation
// ---------------------------------------------------------------------------

func validateEnvironments(p *Pipeline, result *ValidationResult, opts ValidateOptions) {
	topEnvs := p.Environments

	// If known environments are provided, check all references against them.
	if len(opts.Environments) > 0 {
		checkEnvRefs := func(envs []string, field string) {
			for _, env := range envs {
				if !slices.Contains(opts.Environments, env) {
					suggestion := findClosest(env, toSet(opts.Environments))
					issue := ValidationIssue{
						Code:     CodeUnknownRef,
						Field:    field,
						Message:  fmt.Sprintf("unknown environment %q", env),
						Severity: SeverityError,
					}
					if suggestion != "" {
						issue.Suggestion = fmt.Sprintf("Did you mean %q? Available: %s", suggestion, strings.Join(opts.Environments, ", "))
					}
					result.Issues = append(result.Issues, issue)
				}
			}
		}

		checkEnvRefs(p.Environments, "environments")
		if p.Triggers.Push != nil {
			checkEnvRefs(p.Triggers.Push.Environments, "triggers.push.environments")
		}
		if p.Triggers.Manual != nil {
			checkEnvRefs(p.Triggers.Manual.Environments, "triggers.manual.environments")
		}
		if p.Triggers.Schedule != nil {
			checkEnvRefs(p.Triggers.Schedule.Environments, "triggers.schedule.environments")
		}
		if p.Triggers.Tag != nil {
			checkEnvRefs(p.Triggers.Tag.Environments, "triggers.tag.environments")
		}
		for i, promo := range p.Triggers.Promotion {
			checkEnvRefs(promo.Environments, fmt.Sprintf("triggers.promotion[%d].environments", i))
			checkEnvRefs([]string{promo.From}, fmt.Sprintf("triggers.promotion[%d].from", i))
		}
		if p.Triggers.Webhook != nil {
			checkEnvRefs(p.Triggers.Webhook.Environments, "triggers.webhook.environments")
		}
		for i, s := range p.Steps {
			checkEnvRefs(s.Environments, fmt.Sprintf("steps[%d].environments", i))
		}
	}

	// Trigger/step environments must be subsets of top-level (if specified).
	if len(topEnvs) > 0 {
		checkSubset := func(envs []string, field string) {
			for _, env := range envs {
				if !slices.Contains(topEnvs, env) {
					result.Issues = append(result.Issues, ValidationIssue{
						Code:     CodeEnvMismatch,
						Field:    field,
						Message:  fmt.Sprintf("environment %q is not in the pipeline's top-level environments %v", env, topEnvs),
						Severity: SeverityError,
					})
				}
			}
		}

		if p.Triggers.Push != nil {
			checkSubset(p.Triggers.Push.Environments, "triggers.push.environments")
		}
		if p.Triggers.Manual != nil {
			checkSubset(p.Triggers.Manual.Environments, "triggers.manual.environments")
		}
		if p.Triggers.Schedule != nil {
			checkSubset(p.Triggers.Schedule.Environments, "triggers.schedule.environments")
		}
		if p.Triggers.Tag != nil {
			checkSubset(p.Triggers.Tag.Environments, "triggers.tag.environments")
		}
		for i, promo := range p.Triggers.Promotion {
			checkSubset(promo.Environments, fmt.Sprintf("triggers.promotion[%d].environments", i))
		}
		if p.Triggers.Webhook != nil {
			checkSubset(p.Triggers.Webhook.Environments, "triggers.webhook.environments")
		}
		for i, s := range p.Steps {
			checkSubset(s.Environments, fmt.Sprintf("steps[%d].environments", i))
		}
	}
}
