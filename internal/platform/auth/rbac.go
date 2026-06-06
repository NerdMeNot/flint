package auth

// ────────────────────────────────────────────────────────────
// Admin vs CI classification
// ────────────────────────────────────────────────────────────

var adminObjects = map[string]bool{
	ObjWorkspace:   true,
	ObjTeam:        true,
	ObjEnvironment: true,
	ObjRunner:      true,
	ObjConnection:  true,
	ObjAPIKey:      true,
	ObjSecret:      true,
	ObjRole:        true,
	ObjAudit:       true,
}

var ciObjects = map[string]bool{
	ObjProject: true,
	ObjRun:     true,
	ObjGate:    true,
}

// IsAdminObject returns true if the object is an admin resource.
// Admin permissions are always platform-wide (no workspace/env scope).
func IsAdminObject(obj string) bool {
	return adminObjects[obj]
}

// IsCIObject returns true if the object is a CI resource.
// CI permissions can be scoped to workspaces and environments.
func IsCIObject(obj string) bool {
	return ciObjects[obj]
}

// ────────────────────────────────────────────────────────────
// Valid actions per object
// ────────────────────────────────────────────────────────────

// ValidActions maps each object to its valid actions.
var ValidActions = map[string][]string{
	// Admin.
	ObjWorkspace:   {ActRead, ActManage},
	ObjTeam:        {ActRead, ActManage},
	ObjEnvironment: {ActRead, ActManage},
	ObjRunner:      {ActRead, ActManage},
	ObjConnection:  {ActRead, ActManage},
	ObjAPIKey:      {ActRead, ActManage},
	ObjSecret:      {ActRead, ActManage},
	ObjRole:        {ActRead, ActManage},
	ObjAudit:       {ActRead}, // read-only by nature

	// CI.
	ObjProject: {ActRead, ActWrite},
	ObjRun:     {ActRead, ActTrigger, ActCancel},
	ObjGate:    {ActApprove, ActReject},
}

// ────────────────────────────────────────────────────────────
// Permission implications
// ────────────────────────────────────────────────────────────

// Implications maps a permission to the permissions it automatically grants.
// Only explicitly selected permissions are stored; implications are computed.
var Implications = map[string][]string{
	// CI implications.
	"project:write": {"project:read"},
	"run:trigger":   {"project:read"},
	"run:cancel":    {"run:read", "project:read"},
	"gate:approve":  {"run:read", "project:read"},
	"gate:reject":   {"run:read", "project:read"},

	// Admin: manage implies read.
	"workspace:manage":   {"workspace:read"},
	"team:manage":        {"team:read"},
	"environment:manage": {"environment:read"},
	"runner:manage":      {"runner:read"},
	"connection:manage":  {"connection:read"},
	"apikey:manage":      {"apikey:read"},
	"secret:manage":      {"secret:read"},
	"role:manage":        {"role:read"},
}

// ExpandImplications takes a set of explicit permissions and returns
// the full set including all implied permissions.
func ExpandImplications(explicit []Permission) []Permission {
	seen := make(map[string]bool, len(explicit)*2)
	for _, p := range explicit {
		seen[p.Key()] = true
	}

	// Iterate until no new implications are added.
	changed := true
	for changed {
		changed = false
		for key := range seen {
			implied, ok := Implications[key]
			if !ok {
				continue
			}
			for _, dep := range implied {
				if !seen[dep] {
					seen[dep] = true
					changed = true
				}
			}
		}
	}

	result := make([]Permission, 0, len(seen))
	for key := range seen {
		obj, act := splitPermKey(key)
		result = append(result, Permission{Object: obj, Action: act})
	}
	return result
}

// splitPermKey splits "object:action" into its parts.
func splitPermKey(key string) (string, string) {
	for i := range key {
		if key[i] == ':' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}

// IsWildcard returns true if the permission set contains *:*.
func IsWildcard(perms []Permission) bool {
	for _, p := range perms {
		if p.Object == ObjWildcard && p.Action == ActWildcard {
			return true
		}
	}
	return false
}
