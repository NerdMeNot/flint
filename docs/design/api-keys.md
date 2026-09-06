# API Keys in Flint

## Overview

API keys provide programmatic access to Flint. They reuse the RBAC model — each key is assigned a role, optionally with a narrower scope restriction.

## Model

```
ApiKey {
  id:            string
  name:          string          // "CI Bot", "Terraform Automation"
  role:          string          // role slug — determines permissions
  workspaces:    string[]        // optional scope restriction (narrows role scope)
  environments:  string[]        // optional scope restriction (narrows role scope)
  expiresAt:     string?         // optional expiry
  lastUsedAt:    string?
  createdAt:     string
  createdBy:     string          // user who created it
}
```

## How permissions work

API keys do NOT have inline permissions. They inherit permissions from their assigned role.

The key can optionally **restrict** the role's scope further:

```
Role: "Developer"
  Permissions: [project:read, project:write, run:read, run:trigger, run:cancel]
  Workspaces: []           ← all
  Environments: []         ← all

API Key: "CI Bot"
  Role: developer
  Workspaces: [production]     ← narrows to production only
  Environments: [production]   ← narrows to production only

Effective: Developer permissions, but only in production workspace + production environment.
```

**Key restriction is an intersection, never a union.** If the role is scoped to `[production, staging]` and the key restricts to `[production]`, the effective scope is `[production]`. The key cannot widen beyond the role's scope.

If the key has no scope restriction (empty arrays), it inherits the role's scope as-is.

## Casbin integration

API keys are subjects in Casbin, prefixed with `apikey:`:

```
p, apikey:ci-bot, <org>, production, production, project, read
p, apikey:ci-bot, <org>, production, production, run, trigger
...
```

On API request:
1. Extract key from `X-API-Key` (or `Authorization: Bearer flint_...`)
2. Look it up by the SHA-256 digest of its raw value — one indexed lookup. The
   key is 32 bytes of CSPRNG output, so it needs a fast digest and a unique
   index, not a password KDF. This used to bcrypt-compare every live key on
   every request.
3. Set `Claims.Principal = "apikey:{key-id}"`
4. `enforcer.Enforce(principal, orgID, workspace, environment, object, action)`

Same matcher as users/teams. The principal is the part that has to be right:
enforcement once ran against `Claims.Email`, which an API key does not have, so
every key request was denied however privileged the key was.

## Lifecycle

- **Creation:** Admin picks a name, role, optional scope restriction, optional expiry. The token value (`flint_...`) is shown once and never again.
- **Usage:** Every API call with the key is logged in the audit trail with `apikey:{key-id}` as the actor.
- **Expiry:** Expired keys fail auth immediately. No automatic cleanup — they stay in the list as expired until manually deleted.
- **Deletion:** Removes the key and its Casbin policies.
- **Role changes:** If the underlying role is updated, the key's effective permissions change automatically (Casbin policies are regenerated).

## UI

### List view (`/settings/api-keys`)

Each key card shows:
- Name
- Assigned role (badge)
- Scope restriction (if narrowed from role, show both role scope and key restriction)
- Created date, last used, expiry status
- Delete action

### Create form (modal)

1. **Name** — text input
2. **Role** — FormSelect dropdown listing all available roles. On selection, show the role's permissions + scope as a preview.
3. **Scope restriction** — optional. Same pattern as role form: radio "Same as role" / "Restrict further", then workspace + environment chip selectors. Only show workspaces/environments that are within the role's scope (can't widen).
4. **Expiry** — optional date picker or preset durations (30 days, 90 days, 1 year, no expiry)
5. **Confirm** → show the generated token once

## Database schema

```sql
CREATE TABLE api_keys (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  token_hash    TEXT NOT NULL UNIQUE,   -- bcrypt/argon2 hash of the token
  role_id       TEXT NOT NULL REFERENCES roles(id),
  expires_at    TIMESTAMPTZ,
  last_used_at  TIMESTAMPTZ,
  created_by    TEXT NOT NULL,          -- user who created it
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Optional scope restriction (empty = inherit role scope)
CREATE TABLE api_key_workspace_scope (
  api_key_id   TEXT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  PRIMARY KEY (api_key_id, workspace_id)
);

CREATE TABLE api_key_environment_scope (
  api_key_id     TEXT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
  environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
  PRIMARY KEY (api_key_id, environment_id)
);
```

Token value is never stored — only the hash. The raw token is shown once at creation time.
