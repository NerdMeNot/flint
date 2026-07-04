package pipeline

import (
	"path/filepath"
	"strings"
)

// TriggerEvent represents a normalized webhook event for trigger matching.
type TriggerEvent struct {
	Kind       string // "push", "pull_request", "tag"
	Branch     string // branch name (push events)
	BaseBranch string // PR target branch (pull_request events)
	Tag        string // tag name (tag events)

	// ChangedFiles are the paths touched by the event, for paths: filters.
	// nil means UNKNOWN (filters fail open — a missing file list must never
	// wedge CI); an empty non-nil slice means "no files changed".
	ChangedFiles []string
}

// TriggerMatch represents a trigger that matched an event.
type TriggerMatch struct {
	TriggerType  string   // "push", "pull_request", "tag"
	Environments []string // target environments from the trigger (nil = plain CI)
}

// MatchTriggers evaluates all triggers in a pipeline against a webhook event.
// Returns the list of matching triggers with their environments.
// An empty result means the pipeline should not run for this event.
func MatchTriggers(p *Pipeline, event TriggerEvent) []TriggerMatch {
	if p == nil {
		return nil
	}

	var matches []TriggerMatch

	switch event.Kind {
	case "push":
		if t := p.Triggers.Push; t != nil {
			if matchesAny(event.Branch, t.Branches) && pathsMatch(t.Paths, event.ChangedFiles) {
				matches = append(matches, TriggerMatch{
					TriggerType:  "push",
					Environments: t.Environments,
				})
			}
		}

	case "pull_request":
		if t := p.Triggers.PullRequest; t != nil {
			if matchesAny(event.BaseBranch, t.Branches) && pathsMatch(t.Paths, event.ChangedFiles) {
				matches = append(matches, TriggerMatch{
					TriggerType:  "pull_request",
					Environments: nil, // PRs never have environments
				})
			}
		}

	case "tag":
		if t := p.Triggers.Tag; t != nil {
			if matchesAny(event.Tag, t.Patterns) {
				matches = append(matches, TriggerMatch{
					TriggerType:  "tag",
					Environments: t.Environments,
				})
			}
		}
	}

	return matches
}

// CollectEnvironments deduplicates environments from trigger matches.
// If no trigger specifies environments, returns [""] (one run, no environment).
func CollectEnvironments(matches []TriggerMatch) []string {
	seen := make(map[string]bool)
	var result []string

	for _, m := range matches {
		if len(m.Environments) == 0 {
			if !seen[""] {
				seen[""] = true
				result = append(result, "")
			}
		} else {
			for _, env := range m.Environments {
				if !seen[env] {
					seen[env] = true
					result = append(result, env)
				}
			}
		}
	}

	if len(result) == 0 {
		return []string{""}
	}
	return result
}

// pathsMatch reports whether an event's changed files satisfy a trigger's
// paths: filter. No filter → always. Unknown file list (nil) → fail open.
// Otherwise at least one changed file must match at least one pattern.
func pathsMatch(patterns, changedFiles []string) bool {
	if len(patterns) == 0 {
		return true
	}
	if changedFiles == nil {
		return true // unknown — a filter must never wedge CI
	}
	for _, f := range changedFiles {
		if matchesAny(f, patterns) {
			return true
		}
	}
	return false
}

// matchesAny returns true if value matches any of the glob patterns.
func matchesAny(value string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchGlob(pattern, value) {
			return true
		}
	}
	return false
}

// matchGlob matches a value against a glob pattern.
// Supports:
//   - exact match: "main"
//   - single-star wildcard: "feature/*" matches "feature/foo" (one level)
//   - double-star wildcard: "feature/**" matches "feature/foo/bar" (any depth)
//   - wildcards within segments: "release-*" matches "release-v1"
func matchGlob(pattern, value string) bool {
	// Handle ** (double-star) by splitting into prefix matching.
	if strings.Contains(pattern, "**") {
		return matchDoubleStar(pattern, value)
	}

	// Use filepath.Match for standard glob patterns.
	// filepath.Match doesn't match path separators with *, which is what we want
	// for single-star patterns like "feature/*".
	matched, err := filepath.Match(pattern, value)
	if err != nil {
		return pattern == value // fallback to exact match on invalid pattern
	}
	return matched
}

// matchDoubleStar handles ** patterns that match any number of path segments.
func matchDoubleStar(pattern, value string) bool {
	// Split on ** and check prefix/suffix.
	parts := strings.SplitN(pattern, "**", 2)
	prefix := parts[0]
	suffix := ""
	if len(parts) > 1 {
		suffix = parts[1]
	}

	// Strip trailing / from prefix for cleaner matching.
	prefix = strings.TrimSuffix(prefix, "/")

	if prefix != "" && !strings.HasPrefix(value, prefix) {
		// Check if prefix is a glob pattern itself.
		prefixParts := strings.Split(prefix, "/")
		valueParts := strings.Split(value, "/")
		if len(valueParts) < len(prefixParts) {
			return false
		}
		for i, pp := range prefixParts {
			matched, _ := filepath.Match(pp, valueParts[i])
			if !matched {
				return false
			}
		}
	}

	if suffix != "" {
		suffix = strings.TrimPrefix(suffix, "/")
		if !strings.HasSuffix(value, suffix) {
			return false
		}
	}

	return true
}
