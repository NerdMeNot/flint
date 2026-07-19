package runner

import "time"

// ResolvePolicy computes the effective economics policy for one run by applying
// the pool's per-branch/per-event overrides to its base policy.
//
// First-match-wins: overrides are tried in declaration order and the FIRST whose
// match selects the run is applied; later overrides are ignored. This gives a
// platform team explicit control — order specific rules (a named release branch)
// before general ones (a catch-all with an empty match). With no matching
// override the base policy is returned unchanged.
//
// The function is pure: it reads nothing but its arguments and mutates nothing,
// so it is the load-bearing, exhaustively-testable core of the fleet's economics
// decisions (the provisioner resolves a policy per demand group before quoting).
// `branch` is the run's git ref, `event` its trigger type (push/pull_request/
// tag/manual/…); an empty value means "unknown", which only matches an override
// whose corresponding field is also empty (a wildcard).
func ResolvePolicy(base Policy, branch, event string) Policy {
	for _, o := range base.Overrides {
		if o.Match.matches(branch, event) {
			return o.Set.applyTo(base)
		}
	}
	// No override matched: the effective policy is the base, but with its
	// overrides cleared — a resolved policy is always override-free, so callers
	// never re-resolve it.
	out := base
	out.Overrides = nil
	return out
}

// matches reports whether this selector applies to a run. An empty field is a
// wildcard (matches any value); a set field must equal the run's value exactly.
// Exact match is deliberate for v1 — branch/event pattern globbing is a later,
// separately-designed enhancement, not an accident of string comparison.
func (m PolicyMatch) matches(branch, event string) bool {
	if m.Branch != "" && m.Branch != branch {
		return false
	}
	if m.Event != "" && m.Event != event {
		return false
	}
	return true
}

// applyTo returns base with the patch's non-nil fields overridden. The result's
// Overrides list is cleared: the effective policy is already resolved, so
// carrying the overrides forward would invite a second, incorrect application.
func (p PolicyPatch) applyTo(base Policy) Policy {
	out := base
	out.Overrides = nil
	if p.MinWarm != nil {
		out.MinWarm = *p.MinWarm
	}
	if p.CapacityType != nil {
		out.CapacityType = *p.CapacityType
	}
	if p.Objective != nil {
		out.Objective = *p.Objective
	}
	if p.IdleTTLSecs != nil {
		out.IdleTTL = time.Duration(*p.IdleTTLSecs) * time.Second
	}
	return out
}
