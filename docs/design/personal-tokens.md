# Personal Tokens in Flint

## Overview

Personal tokens allow users to authenticate the Flint CLI/TUI and API without a browser session. Unlike admin API keys, personal tokens inherit the user's own permissions — no role assignment needed.

## Distinction from Admin API Keys

| | Admin API Keys | Personal Tokens |
|---|---|---|
| Who creates | Platform admins | Any authenticated user |
| Where in UI | Admin > API Keys | User profile page |
| Permissions | Assigned a specific role + optional scope restriction | Inherits all of the user's roles (direct + team) |
| Casbin subject | `apikey:<id>` (separate policies) | User's email (same as session) |
| Managed by | Admins only | The user themselves (admins can also revoke) |

## Model

```
PersonalToken {
  id:          string
  userId:      string          // owner
  name:        string          // "MacBook Pro", "CI workstation"
  expiresAt:   string?         // optional expiry
  lastUsedAt:  string?
  createdAt:   string
}
```

No role, no scope — the token acts as the user. When the Flint server receives a request with a personal token, it resolves the user's email and uses that as the Casbin subject. The user's existing role assignments (direct + team-inherited) apply automatically.

## Auth flow

1. User generates a personal token in the UI → gets `flint_pat_<random>` shown once
2. CLI stores the token in `~/.config/flint/credentials`
3. On API request: `Authorization: Bearer flint_pat_...`
4. Server looks up token hash → resolves to user email
5. Casbin check uses the user's email as subject — same as a browser session

## Database

```sql
CREATE TABLE personal_tokens (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  token_hash   TEXT NOT NULL UNIQUE,
  expires_at   TIMESTAMPTZ,
  last_used_at TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

No Casbin policy generation needed — the token resolves to the user, and the user's policies already exist.

## UI

Personal Tokens section on the user detail page (`/settings/users/$id`):
- List of tokens: name, created date, last used, expiry status
- "Generate token" button → modal with name + expiry → shows token once
- Revoke button per token
