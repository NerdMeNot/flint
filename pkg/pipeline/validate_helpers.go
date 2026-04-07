package pipeline

import (
	"regexp"
	"strings"
	"time"
)

// Valid-value lists used across validation functions.
var (
	validWhenValues        = []string{"onSuccess", "onFailure", "always"}
	validShells            = []string{"sh", "bash", "python"}
	validInputTypes        = []string{"string", "boolean", "choice"}
	validPromotionStatuses = []string{"succeeded", "failed"}
)

func collectStepNames(p *Pipeline) map[string]bool {
	names := make(map[string]bool, len(p.Steps))
	for _, s := range p.Steps {
		names[s.Name] = true
	}
	return names
}

// findClosest finds the closest match in a set using Levenshtein distance.
// Returns empty string if no close match is found (threshold: distance <= 3).
func findClosest(target string, candidates map[string]bool) string {
	best := ""
	bestDist := 4 // Max edit distance for "did you mean" suggestions

	for candidate := range candidates {
		d := levenshtein(target, candidate)
		if d < bestDist {
			bestDist = d
			best = candidate
		}
	}
	return best
}

// levenshtein computes the edit distance between two strings.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)

	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(
				prev[j]+1,      // deletion
				curr[j-1]+1,    // insertion
				prev[j-1]+cost, // substitution
			)
		}
		prev, curr = curr, prev
	}

	return prev[lb]
}

func isValidApprover(approver string) bool {
	// role:slug — must have non-empty slug
	if strings.HasPrefix(approver, "role:") {
		slug := strings.TrimPrefix(approver, "role:")
		return slug != "" && !strings.Contains(slug, " ")
	}
	// team:slug — must have non-empty slug
	if strings.HasPrefix(approver, "team:") {
		slug := strings.TrimPrefix(approver, "team:")
		return slug != "" && !strings.Contains(slug, " ")
	}
	// email — must have text before and after @
	if idx := strings.Index(approver, "@"); idx > 0 && idx < len(approver)-1 {
		return true
	}
	return false
}

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func isValidEnvName(name string) bool {
	return envNamePattern.MatchString(name)
}

func toSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, item := range items {
		set[item] = true
	}
	return set
}

// parseDuration parses a duration string (Go format: 30s, 5m, 1h, 2h30m).
func parseDuration(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}

// evaluateTriggerEnv checks whether a trigger's environment list includes the
// target environment. Returns (active, reason). If triggerEnvs is empty the
// trigger is considered active for all environments.
func evaluateTriggerEnv(triggerEnvs []string, targetEnv string) (bool, string) {
	if len(triggerEnvs) == 0 {
		return true, "active"
	}
	for _, e := range triggerEnvs {
		if e == targetEnv {
			return true, "active"
		}
	}
	return false, "environments: " + formatList(triggerEnvs) + " — not this environment"
}
