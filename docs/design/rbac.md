# RBAC Model in Flint

## Overview

Flint uses a two-layer access control model:

1. **Platform Admin** — binary flag, grants access to the admin area
2. **CI Roles** — granular permissions for day-to-day CI operations, optionally scoped to workspaces and/or environments

## Core Principles

- Roles carry scope, not assignments. A user is assigned a role, period. The role itself defines which workspaces and environments it applies to.
- Default (system) roles are unscoped — they apply everywhere.
- Custom roles can be scoped to specific workspaces and/or environments.
- Workspace scope and environment scope are independent dimensions.
- An unscoped dimension means "all" — no restriction.

---

## Two-Layer Model

### Layer 1: Platform Admin

Binary. You either have admin access or you don't.

- **Platform Admins** can access the admin area (`/settings`): create workspaces, manage teams, configure runners, set up IdP, manage roles, view audit logs, manage API keys, manage forge connections.
- **Everyone else** has no access to the admin area.

No granular permissions within admin. If you're trusted to configure the platform, you can configure all of it. This keeps admin simple.

### Layer 2: CI Roles

Granular permissions for CI operations, assigned to users or teams.

Each role consists of:
- **Permissions** — a set of `object:action` pairs
- **Workspace scope** — optional list of workspaces (empty = all workspaces)
- **Environment scope** — optional list of environments (empty = all environments)

---

## Role Definition

```
Role {
  id:           string
  name:         string          // "Prod Release Manager"
  slug:         string          // "prod-release-manager"
  description:  string
  isSystem:     boolean         // true = immutable, created at boot
  permissions:  Permission[]    // what this role can do
  workspaces:   string[]        // optional scope — empty means all
  environments: string[]        // optional scope — empty means all
}

Permission {
  object:  string   // "run", "project", "gate", "secret", "environment", "audit"
  action:  string   // "read", "write", "trigger", "cancel", "approve", "reject"
}
```

### Scope Behavior

| Workspaces | Environments | Meaning |
|---|---|---|
| `[]` | `[]` | Full access — all workspaces, all environments |
| `[production]` | `[]` | Only projects in the `production` workspace, but all environments |
| `[]` | `[staging]` | All workspaces, but only runs targeting the `staging` environment |
| `[production]` | `[production]` | Only `production` workspace projects, only `production` environment runs |

The two scopes are independent. This allows combinations like:
- QA team: all workspaces, staging environment only
- Platform operator: platform workspace only, all environments
- Release manager: production workspace, production environment

---

## Default (System) Roles

System roles are created at platform boot. They cannot be modified or deleted. They are always unscoped (apply to all workspaces and environments).

| Role | Admin Permissions | CI Permissions | Notes |
|---|---|---|---|
| **Admin** | `*:*` | `*:*` | Full access to everything |
| **Developer** | `environment:read`, `runner:read`, `secret:read` | `project:read/write`, `run:read/trigger/cancel` | Standard dev — trigger runs, read admin resources |
| **Viewer** | `workspace:read`, `team:read`, `environment:read`, `runner:read` | `project:read`, `run:read` | Read-only across the platform |
| **Platform Manager** | All admin `read` + `manage` | `project:read`, `run:read` | Full admin access, CI read-only |

System roles provide the baseline. Custom roles add specificity.

---

## Custom Roles

Admins can create custom roles with specific permissions and optional scoping. Admin permissions in custom roles are always platform-wide. CI permissions can be scoped.

### Examples

**Prod Release Manager**
```
admin:        (none)
ci:           [run:trigger, gate:approve, gate:reject]
              (+ implied: run:read, project:read)
workspaces:   [production]
environments: [production]
```
Can trigger runs and approve/reject gates, but only in the production workspace targeting the production environment. The implied `run:read` and `project:read` are auto-granted.

**Security Auditor**
```
admin:        [audit:read, secret:read, environment:read]
ci:           [project:read, run:read]
workspaces:   [production, staging]
environments: [production, staging]
```
Read access to audit logs, secrets, and environments platform-wide. CI read access scoped to production and staging.

**Staging Operator**
```
admin:        (none)
ci:           [run:trigger, run:cancel, gate:approve, gate:reject]
              (+ implied: run:read, project:read)
workspaces:   []                    // all workspaces
environments: [staging]             // staging only
```
Full CI operations scoped to the staging environment across all workspaces. Useful for a QA team.

---

## Assignments

Assignments are simple — a subject gets a role. No workspace or environment in the assignment itself. The role carries the scope.

```
Assignment {
  subject:  string    // user email or "team:<slug>"
  role:     string    // role slug
}
```

### Examples

```
assign("alice@acme.dev", "admin")
assign("alice@acme.dev", "prod-release-manager")
assign("bob@acme.dev", "developer")
assign("team:backend-devs", "developer")
assign("team:release-mgrs", "prod-release-manager")
assign("eve@acme.dev", "security-auditor")
```

A user/team can have multiple roles. Permissions are **additive** — the effective permissions are the union of all assigned roles (filtered by scope).

---

## Access Check Algorithm

To check: "Can `subject` perform `action` on `object` in `workspace` W targeting `environment` E?"

```
1. Find all roles assigned to the subject
   (includes roles assigned to any team the subject belongs to)

2. For each role:
   a. Does the role have permission (object, action)? If not → skip
   b. Is the role workspace-scoped?
      - If yes: is W in the role's workspace list? If not → skip
      - If no (empty): workspace check passes
   c. Is the role environment-scoped?
      - If yes: is E in the role's environment list? If not → skip
      - If no (empty): environment check passes
   d. All checks pass → ALLOWED

3. No role matched → DENIED
```

### Context-dependent checks

Not all checks have both dimensions:
- Viewing a project → only workspace scope matters (no environment)
- Triggering a run → workspace scope (project's workspace) + environment scope (target environment)
- Approving a gate → workspace scope + environment scope
- Reading a secret → environment scope (environment-scoped secrets) or no scope (org secrets)

When a dimension is not applicable (e.g., viewing a project has no environment), that dimension is skipped in the check.

---

## Permission Catalog

Permissions are split into two domains. **Admin permissions** are always platform-wide (no scope). **CI permissions** can be scoped to workspaces and/or environments.

### Admin Permissions (platform-wide, no scope)

| Resource | read | manage | Description |
|---|---|---|---|
| `workspace` | View workspace list | Create/edit/delete workspaces |
| `team` | View teams + members | Create/edit teams, manage members |
| `environment` | View env config, variables | Create/edit/delete, change rules |
| `runner` | View runner pools | Create/edit/delete pools |
| `connection` | View forge connections | Create/edit/delete connections |
| `apikey` | View API key names + scopes | Create/revoke keys |
| `secret` | View secret names (never values) | Create/edit/delete secrets |
| `role` | View roles + assignments | Create/edit roles, manage assignments |
| `audit` | View audit log | — (read-only by nature) |

### CI Permissions (scopable to workspaces + environments)

| Resource | read | write | trigger | cancel | approve | reject |
|---|---|---|---|---|---|---|
| `project` | View projects, pipelines | Edit project settings | | | | |
| `run` | View runs, steps, logs | | Trigger pipeline runs | Cancel running/pending | | |
| `gate` | | | | | Approve gates | Reject gates |

### Wildcard

| Permission | Description |
|---|---|
| `*:*` | All permissions across both domains (Admin role only) |

### Scope Behavior

Admin permissions are **always platform-wide** — no workspace or environment scoping. If you have `runner:manage`, you can manage all runners. There is no "manage runners only in the production workspace."

CI permissions can be **optionally scoped** to workspaces and/or environments. The scope selectors only appear in the role form when CI permissions are selected.

---

## Permission Implications

Higher-level permissions automatically imply lower-level ones. This prevents misconfiguration (e.g., granting `gate:approve` without `run:read` would mean the approver can't see what they're approving).

### CI Implications

| Permission | Automatically grants |
|---|---|
| `project:write` | `project:read` |
| `run:trigger` | `project:read` |
| `run:cancel` | `run:read`, `project:read` |
| `gate:approve` | `run:read`, `project:read` |
| `gate:reject` | `run:read`, `project:read` |

### Admin Implications

| Permission | Automatically grants |
|---|---|
| `workspace:manage` | `workspace:read` |
| `team:manage` | `team:read` |
| `environment:manage` | `environment:read` |
| `runner:manage` | `runner:read` |
| `connection:manage` | `connection:read` |
| `apikey:manage` | `apikey:read` |
| `secret:manage` | `secret:read` |
| `role:manage` | `role:read` |

### How implications work

**Storage:** Only explicitly selected permissions are stored in the role definition. Implied permissions are NOT stored.

**UI (edit mode):** When a user checks `gate:approve`, the implied `run:read` and `project:read` checkboxes auto-enable with a lock icon and tooltip "Required by another permission." They cannot be unchecked while the implying permission is active.

**UI (read-only mode):** Explicitly granted permissions show as full-opacity dots. Implied permissions show as dimmed dots (40% opacity) — making it clear what was configured vs. what was auto-derived.

**Backend (access check):** When evaluating access, expand the stored permissions by applying implications before checking. This means implications can evolve without requiring data migrations — the stored role stays clean.

```
function expandPermissions(explicit: Permission[]): Permission[] {
  const result = new Set(explicit.map(p => `${p.object}:${p.action}`))
  for (const perm of result) {
    const implied = IMPLICATIONS[perm]
    if (implied) {
      for (const dep of implied) result.add(dep)
    }
  }
  return [...result].map(key => {
    const [object, action] = key.split(':')
    return { object, action }
  })
}
```

---

## Casbin Policy Mapping

Flint uses Casbin for policy enforcement. The model maps as follows:

### Model Definition (CONF)

```ini
[request_definition]
r = sub, workspace, environment, obj, act

[policy_definition]
p = sub, workspace, environment, obj, act

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && \
    (p.workspace == "*" || p.workspace == r.workspace) && \
    (p.environment == "*" || p.environment == r.environment) && \
    (p.obj == "*" || p.obj == r.obj) && \
    (p.act == "*" || p.act == r.act)
```

### Policy Generation

When a role is assigned to a subject, Flint generates Casbin policies by expanding the role definition:

```
Role "prod-release-manager":
  permissions: [run:trigger, gate:approve, gate:reject]
  workspaces: [production]
  environments: [production]

Assignment: alice@acme.dev → prod-release-manager

Generated Casbin policies:
  p, alice@acme.dev, production, production, run, trigger
  p, alice@acme.dev, production, production, gate, approve
  p, alice@acme.dev, production, production, gate, reject
```

For unscoped dimensions, use wildcard `*`:

```
Role "developer":
  permissions: [project:read, run:read, run:trigger, run:cancel, secret:read]
  workspaces: []
  environments: []

Assignment: bob@acme.dev → developer

Generated Casbin policies:
  p, bob@acme.dev, *, *, project, read
  p, bob@acme.dev, *, *, run, read
  p, bob@acme.dev, *, *, run, trigger
  p, bob@acme.dev, *, *, run, cancel
  p, bob@acme.dev, *, *, secret, read
```

### Team Resolution

Team assignments use Casbin's grouping:

```
Assignment: team:backend-devs → developer

For each member of backend-devs (alice, bob, eve, frank, hiro):
  g, alice@acme.dev, team:backend-devs
  g, bob@acme.dev, team:backend-devs
  ...

Then the team's role policies:
  p, team:backend-devs, *, *, project, read
  p, team:backend-devs, *, *, run, read
  ...
```

Casbin's matcher with `g(r.sub, p.sub)` handles the group membership transitively.

---

## UI Surfaces

### Role Management (Admin → Roles)

**List view:**
- All roles displayed as cards
- System roles shown with "System" badge, not editable
- Custom roles show workspace/environment scope badges
- Edit and delete actions on custom roles

**Create/Edit form:**
- Name, slug (auto-generated from name), description
- Permission grid: checkboxes organized by object (rows) × action (columns)
- Workspace scope: toggle "All workspaces" or multi-select specific workspaces
- Environment scope: toggle "All environments" or multi-select specific environments

### Role Assignments (Admin → Roles → Assignments tab)

**List view:**
- Table: subject | role | effective scope (derived from role)
- Filter by role, by subject

**Create assignment:**
- Subject: multi-select users + teams (like the team member picker)
- Role: single-select from available roles

### Access indicators (CI pages)

- Gates: show who can approve (derived from roles with `gate:approve` + matching scope)
- Runs: trigger button only visible if user has `run:trigger` in scope
- Secrets: only visible if user has `secret:read` in scope

---

## Database Schema (Go/Bun)

```sql
-- Roles
CREATE TABLE roles (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  slug        TEXT NOT NULL UNIQUE,
  description TEXT,
  is_system   BOOLEAN NOT NULL DEFAULT false,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Role permissions (both admin and CI stored in one table)
-- The domain (admin vs CI) is determined by the object name at runtime.
CREATE TABLE role_permissions (
  role_id  TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  object   TEXT NOT NULL,
  action   TEXT NOT NULL,
  PRIMARY KEY (role_id, object, action)
);

-- Only explicit permissions are stored. Implied permissions (e.g., gate:approve
-- implies run:read and project:read) are computed at check time.

-- CI workspace scope (empty = all workspaces)
-- Only applies to CI permissions. Admin permissions are always platform-wide.
CREATE TABLE role_workspace_scope (
  role_id      TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  PRIMARY KEY (role_id, workspace_id)
);

-- CI environment scope (empty = all environments)
-- Only applies to CI permissions. Admin permissions are always platform-wide.
CREATE TABLE role_environment_scope (
  role_id        TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
  PRIMARY KEY (role_id, environment_id)
);

-- Assignments (subject → role)
-- No workspace or environment in the assignment — the role carries the scope.
CREATE TABLE role_assignments (
  subject    TEXT NOT NULL,  -- "alice@acme.dev" or "team:backend-devs"
  role_id    TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (subject, role_id)
);
```

### Notes

- `role_workspace_scope` and `role_environment_scope` being empty means the role's CI permissions apply to all workspaces/environments.
- These scope tables only affect CI permissions. Admin permissions ignore scope entirely.
- On role create/update, regenerate Casbin policies from the role + all its assignments.
- On assignment create/delete, regenerate Casbin policies for that subject.
- System roles are seeded in a database migration, with `is_system = true`.
- Only explicitly selected permissions are stored. The implication expansion (e.g., `gate:approve` → `run:read`, `project:read`) happens at policy generation time and access check time.

### Access check flow (pseudocode)

```
function canAccess(subject, object, action, workspace?, environment?):
  roles = getRolesForSubject(subject)  // includes team roles

  for role in roles:
    perms = expandImplications(role.permissions)

    if not perms.has(object, action):
      continue

    if isAdminResource(object):
      return ALLOWED  // admin perms are always platform-wide

    // CI resource — check scope
    if role.workspaces.length > 0 and workspace not in role.workspaces:
      continue
    if role.environments.length > 0 and environment not in role.environments:
      continue

    return ALLOWED

  return DENIED
```
