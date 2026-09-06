# Flint Authentication & Authorization

## Overview

Flint supports three authentication methods:

1. **Local auth** — Email + password with Argon2id hashing and optional TOTP MFA
2. **OIDC SSO** — OpenID Connect (Okta, Azure AD, Google, Keycloak, etc.)
3. **SAML SSO** — SAML 2.0 (enterprise IdPs)

Plus two programmatic access methods:

4. **API keys** — For CI/CD integrations and automation
5. **Personal access tokens** — For CLI/TUI access (`flint_pat_` prefix)

All methods issue the same session tokens: a short-lived JWT (15 min) + a rotating refresh token (30 day absolute, 7 day idle). Authorization is handled by Casbin RBAC, evaluated per-request — roles are never embedded in the JWT.

---

## Bootstrap: First Admin Setup

### Automatic Bootstrap

On first server start, if no users exist, Flint auto-creates an admin account:

```yaml
# config.yaml
bootstrap:
  email: "admin@flint.local"   # default
  password: ""                  # auto-generated if empty
```

The generated password is logged once at startup:

```
⚡ BOOTSTRAP: Initial admin created — save this password, it will not be shown again
  email=admin@flint.local password=a3f8c21b9e4d...
```

For Helm deployments, the chart can store this in a K8s Secret:
```bash
kubectl get secret flint-admin-bootstrap -o jsonpath='{.data.password}' | base64 -d
```

### CLI Bootstrap

Alternatively, create users via CLI:

```bash
# Create admin user (password auto-generated, printed once)
flint admin create-user --email admin@acme.dev --role admin --config config.yaml

# With explicit password
flint admin create-user --email admin@acme.dev --role admin --password "..."

# Reset a password
flint admin reset-password --email admin@acme.dev --config config.yaml
```

---

## Onboarding Flow

### Day 0: Initial Setup

```
1. Install Flint (Helm/Docker/binary)
   └─ Server starts, auto-creates admin with random password

2. Admin logs in with bootstrap credentials
   POST /auth/login {"email": "admin@flint.local", "password": "..."}
   └─ Response includes forcePasswordChange: true

3. Change password (required before other actions)
   POST /auth/change-password {"currentPassword": "...", "newPassword": "..."}

4. Set up MFA (admin role requires it by default)
   POST /auth/mfa/setup
   └─ Returns: secret, qrCodeURL, recoveryCodes (8 single-use codes)
   POST /auth/mfa/setup/verify {"code": "123456"}
   └─ MFA now active

5. Configure SSO (optional but recommended)
   PUT /api/v1/auth/provider {
     "providerType": "oidc",
     "displayName": "Okta",
     "config": {
       "issuerUrl": "https://acme.okta.com/oauth2/default",
       "clientId": "...",
       "clientSecret": "..."
     }
   }
   └─ OIDC provider hot-reloaded, SSO immediately available
```

### Day 1+: Team Members

```
1. Team member visits Flint UI
   └─ Redirected to SSO provider (Okta/Azure AD/Google)

2. SSO callback
   └─ User auto-created from IdP claims (email, name, groups)
   └─ Teams auto-created from IdP groups
   └─ Default role (viewer) assigned

3. Admin assigns roles via UI/API
   └─ developer, platform-manager, or custom roles
```

---

## Authentication Methods

### Local Auth (Email + Password)

**Login:**
```
POST /auth/login
{"email": "user@acme.dev", "password": "..."}

→ Without MFA:
  {"accessToken": "eyJ...", "refreshToken": "flint_rt_...", "expiresIn": 900}

→ With MFA required:
  {"mfaRequired": true, "mfaToken": "temp_..."}

→ Then:
  POST /auth/mfa/verify {"mfaToken": "temp_...", "code": "123456"}
  {"accessToken": "eyJ...", "refreshToken": "flint_rt_...", "expiresIn": 900}
```

**Password hashing:** Argon2id (64MB memory, 3 iterations, 4 threads, 32-byte key). PHC format: `$argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>`.

**Rate limiting:** three layers, because one is the wrong shape on its own.

| Layer | Budget | Catches |
|---|---|---|
| Per-email failures (DB, 15 min) | 5 | Guessing one account's password |
| Per-IP failures (DB, 15 min) | 25 | Spraying one guess across many accounts |
| Per-IP token bucket (in-process) | 5/s, burst 10 | Bursts against any local-auth endpoint |

The email counter alone missed spraying entirely and let anyone lock a known
user out with five deliberate failures. The IP budget is the looser of the two
because an office NAT legitimately carries many sign-ins.

**Account enumeration:** an unknown email is verified against a dummy Argon2id
hash, so a miss costs the same ~100ms as a hit. Returning early on the miss path
made response time a reliable answer to "does this account exist?".

### OIDC SSO

**Device flow** (CLI/TUI):
```
1. POST /auth/device/code → {deviceCode, userCode, verificationURL}
2. User opens browser, visits /auth/login?code=FLNT-A3F8C21B
3. Redirected to OIDC provider → authenticates → callback
4. CLI polls: POST /auth/device/token {deviceCode: "..."}
   → {accessToken, refreshToken, expiresIn: 900}
```

**Web flow** (browser):
```
1. GET /auth/login → redirect to OIDC provider
2. User authenticates at IdP
3. Callback: GET /auth/oidc/callback?code=...&state=...
4. Server validates, creates session, returns tokens
```

**IdP requirements:**
- OIDC discovery endpoint (`.well-known/openid-configuration`)
- Scopes: `openid profile email groups`
- The `groups` claim is used for automatic team sync

### SAML SSO

```
1. GET /auth/login → redirect to SAML IdP
2. User authenticates at IdP
3. POST /auth/saml/acs (Assertion Consumer Service)
4. Server validates XML signature, extracts claims
```

**Required SAML attributes:** email (or NameID with email format). Optional: name, groups/memberOf.

### API Keys

Created via UI or API by authenticated users:

```
POST /api/v1/api-keys {
  "name": "CI Pipeline",
  "role": "developer",
  "workspaces": ["payments"],
  "environments": ["staging"]
}
→ {"id": "...", "token": "flint_ak_..."}
```

Used in requests: `X-API-Key: flint_ak_...`

API keys can optionally narrow their scope (workspace + environment restrictions) beyond what the assigned role allows.

### Personal Access Tokens

For CLI/TUI access:

```
POST /api/v1/personal-tokens {"name": "My Laptop"}
→ {"id": "...", "token": "flint_pat_..."}
```

Used as: `Authorization: Bearer flint_pat_...`

Personal tokens inherit the user's roles — no separate role assignment.

---

## Session Management

### Token Model

| Token | Lifetime | Storage | Purpose |
|-------|----------|---------|---------|
| Access JWT | 15 min | Client only (not in DB) | API authentication |
| Refresh token | 30d absolute, 7d idle | SHA-256 hash in `sessions` table | Token rotation |

### Token Rotation

Refresh tokens rotate on every use:
1. Client sends refresh token to `POST /auth/refresh`
2. Server validates: not revoked, not expired (absolute + idle)
3. Server creates new refresh token, invalidates old hash
4. Returns new access JWT + new refresh token

A stolen refresh token can only be used once — the next legitimate refresh will fail, alerting the user.

### Session Endpoints

```
POST /auth/refresh       — Rotate tokens
POST /auth/logout        — Revoke session
GET  /auth/sessions      — List active sessions (auth required)
DELETE /auth/sessions/:id — Revoke a specific session
```

### Revocation

Every access JWT carries a `sid` claim naming its `sessions` row, and the auth
middleware checks that row on each request. Without that binding a JWT is
self-validating and therefore unrevokable: logout, OIDC back-channel logout and
the IdP sync daemon all set `revoked_at`, and the token kept working regardless
until it expired.

- **Logout** revokes the session; the next request with that token is rejected
- **Deactivating a user** takes effect on their next request, not at expiry
- **IdP sync daemon** revokes sessions for deprovisioned users
- A token issued before `sid` existed is rejected rather than trusted — "no sid"
  is not evidence of liveness, so such sessions have to sign in once more

---

## MFA (Multi-Factor Authentication)

### TOTP

Time-based one-time passwords compatible with Google Authenticator, Authy, 1Password, etc.

The secret is envelope-encrypted with the server master key before storage, so
MFA cannot be enabled unless `encryption.masterKey` is configured — refusing is
better than storing the credential that defends a compromised password in a form
any database reader can lift. If a secret can no longer be decrypted (rotated or
lost master key) TOTP stops working for that user by design; recovery codes are
hashed separately and remain the way back in.

Verification is bounded: five wrong codes against a single pending token destroy
it, and the user must start again from the password — which is itself rate
limited per email and per IP. A 6-digit code is only a second factor if guessing
is bounded.

**Setup:**
```
POST /auth/mfa/setup     → {secret, qrCodeURL, recoveryCodes}
POST /auth/mfa/setup/verify {"code": "123456"} → MFA enabled
```

**Parameters:** SHA-1, 6 digits, 30-second period.

**Recovery codes:** 8 single-use codes generated during setup. Bcrypt-hashed in DB. Each code can only be used once (removed from the list after use).

**Disable:**
```
DELETE /auth/mfa {"code": "123456"}  — requires current TOTP code
DELETE /auth/mfa {"recoveryCode": "A3F8-C21B"} — or a recovery code
```

### Per-Role Enforcement

Roles have a `require_mfa` flag. The `admin` role requires MFA by default.

When MFA is required by a user's role but not set up:
- Login returns `403 MFA_SETUP_REQUIRED` with a clear message
- User must set up MFA before they can log in

---

## Authorization (RBAC)

### Model

6-field Casbin model: `(subject, org, workspace, environment, object, action)`

- **Subject:** the request's *principal* — user email, `team:<slug>`, or
  `apikey:<id>`. Enforcement uses `Claims.Principal`, never `Claims.Email`: an
  API key has no email, and checking one against key policies matches nothing.
- **Org:** the org id the grant belongs to, or `*`. Roles are per-org rows while
  subjects are bare emails, so without this dimension a grant made in one org
  authorized the same address in every other one.
- **Workspace:** specific slug or `*` (all)
- **Environment:** specific name or `*` (all)

On the request side two values are special:

- `*` means the request is genuinely global (an admin object). Only a
  platform-wide policy satisfies it — a workspace-scoped grant must not.
- `~any` (`auth.ScopeAny`) asks "does this subject hold the permission
  *anywhere*?" It is used by collection routes, which act on no single resource
  and then filter their rows to `auth.PermittedWorkspaces`. Checking a list
  against `*` instead made every scope-restricted user 403 on `GET /projects`.

Collection routes (`/projects`, `/runs`, `/gates`, `/search`) are registered
with `requireAnyScope`, which hands the handler a `WorkspaceScope` **as an
argument**. That is deliberate: a collection route's authorization is weak by
design — it establishes only that the caller holds the permission somewhere —
and the narrowing that makes it safe happens inside the handler. When the
restriction was an ambient context value it was easy to omit, and omitting it is
silent: `GET /projects?archived=true` shared the route, skipped the filter, and
served every workspace's rows. As a parameter it appears in the signature of
every handler that needs one. Use `scope.Filter(requested)` for the query and
`scope.Empty(requested)` to short-circuit.

An explicit `?workspace=` intersects with the permitted set — it can narrow,
never widen. Asking only for a workspace you do not hold returns an empty page
rather than 403, the same answer any filter gives when it excludes everything.

Admin objects (secret, runner, connection, role, …) are always emitted
platform-wide within their org, because the resources behind them carry no
workspace column. A role that combines them with a workspace or environment
scope is therefore refused at definition time rather than silently granting more
than it reads as — see `auth.ValidateRoleScope`.

### System Roles

| Role | Description | Key Permissions |
|------|-------------|-----------------|
| `admin` | Full platform access | `*:*` (wildcard) |
| `developer` | Build + deploy | project read/write, run trigger/cancel |
| `viewer` | Read-only | project read, run read |
| `platform-manager` | Admin without CI writes | workspace/team/env manage, audit read |

No system role requires MFA out of the box. Any role can be marked
`"requireMfa": true` via the roles API; it applies to **local (password) sign-in
only**. An SSO session is established by the IdP, which owns the second factor
there — Flint never sees whether one was presented, so it does not claim to
enforce it. Require MFA for SSO users on the IdP.

A user who holds an MFA-requiring role but has not enrolled gets an *enrolment
token* from `/auth/login` (`mfaSetupRequired: true`) rather than a refusal. The
token is passed as `X-MFA-Enrolment-Token` to `/auth/mfa/setup` and
`/auth/mfa/setup/verify`, is good for five minutes, and cannot be redeemed for a
session. Without it the requirement was a dead end: enrolling needs a session,
and login is the only place one is issued.

The sign-in page runs the enrolment wizard inline when it sees
`mfaSetupRequired`. Enrolment deliberately does not end in a session — the token
proved a password, not a second factor — so the user signs in again afterwards
and is challenged for the code they just set up.

```
POST /auth/login            → { mfaSetupRequired: true, enrolmentToken }
POST /auth/mfa/setup        (X-MFA-Enrolment-Token) → { secret, qrCodeURL, recoveryCodes }
POST /auth/mfa/setup/verify (X-MFA-Enrolment-Token) → token consumed, MFA enabled
POST /auth/login            → { mfaRequired: true, mfaToken }   ← normal challenge
```

One wrinkle worth knowing: TOTP replay protection advances the accepted period on
enrolment, so the *same* code cannot immediately be reused to sign in. In
practice the wizard's recovery-code step covers the wait; if a user is quick,
they see "invalid code" until the next 30-second window.

### Custom Roles

Admins can create custom roles with specific permission combinations:

```
POST /api/v1/roles {
  "name": "Release Manager",
  "slug": "release-manager",
  "permissions": [
    {"object": "run", "action": "trigger"},
    {"object": "gate", "action": "approve"},
    {"object": "project", "action": "read"}
  ],
  "workspaces": ["payments"],
  "environments": ["production"]
}
```

### Permission Objects

**Admin** (platform-wide): workspace, team, environment, runner, connection, apikey, secret, role, audit

**CI** (workspace + environment scoped): project, run, gate

### Permission Actions

- Admin: `read`, `manage`
- CI: `read`, `write`, `trigger`, `cancel`, `approve`, `reject`

### Implications

Some permissions imply others:
- `gate:approve` → `run:read`, `project:read`
- `run:trigger` → `project:read`
- `manage` → `read` (for all admin objects)

---

## IdP Sync Daemon (`flint-syncd`)

A separate binary that runs alongside the server. Periodically validates active sessions against the IdP.

### What It Does

Every cycle (default 15 min):

1. **Validates sessions** — Uses stored OIDC refresh tokens to call UserInfo
2. **Syncs groups** — Updates team memberships from IdP `groups` claim (additive — never removes manual team assignments)
3. **Revokes deprovisioned users** — If the IdP token fails, all sessions for that user are revoked
4. **Cleans up** — Deletes expired/revoked sessions older than 7 days

### Running

```bash
flint-syncd --config config.yaml
```

### Configuration

```yaml
sync:
  enabled: true
  interval: "15m"   # sync cycle interval
```

### Team Sync Behavior

Team sync is **additive**, not clean-slate:
- Teams with `source='idp'` are managed by sync — members are added/removed based on IdP groups
- Teams with `source='internal'` (manually created) are never touched
- A user can belong to both IdP-synced and manually-assigned teams

---

## Security Properties

| Property | Implementation |
|----------|---------------|
| Password storage | Argon2id (64MB memory-hard) |
| Token storage | SHA-256 hash in DB (never stored raw) |
| JWT signing | HMAC-SHA256 (configurable key, min 32 chars) |
| Rate limiting | 5/email + 25/IP per 15min (DB, cross-replica) + per-IP token bucket |
| TOTP | SHA-1, 6 digits, 30s period, 1-step window |
| Recovery codes | 64-bit, bcrypt-hashed, single-use |
| TOTP secrets | Envelope-encrypted with the server master key |
| MFA attempts | 5 per pending token, then the token is destroyed |
| API keys / personal tokens | SHA-256 digest + unique index (high-entropy tokens, so no KDF) |
| Session revocation | Immediate — the `sid` claim is checked against `sessions` per request |
| IdP token refresh | Automatic rotation via oauth2.TokenSource |
| SAML validation | XML signature verification via crewjam/saml |
| OIDC validation | ID token verification + nonce check via go-oidc |
| Audit trail | Login, logout, password change, MFA enable/disable, SSO config |

---

## API Reference

### Public Endpoints (no auth required)

| Method | Path | Description |
|--------|------|-------------|
| POST | `/auth/login` | Email + password login |
| POST | `/auth/mfa/verify` | Complete MFA challenge |
| POST | `/auth/device/code` | Start device flow |
| POST | `/auth/device/token` | Poll device flow completion |
| POST | `/auth/refresh` | Rotate tokens |
| POST | `/auth/logout` | Revoke session |
| GET | `/auth/login` | SSO redirect (with `?code=` for device flow) |
| GET | `/auth/oidc/callback` | OIDC callback |
| POST | `/auth/saml/acs` | SAML callback |

### Authenticated Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/auth/me` | Current user info + permissions |
| POST | `/auth/change-password` | Change password |
| POST | `/auth/mfa/setup` | Generate TOTP secret + QR |
| POST | `/auth/mfa/setup/verify` | Confirm TOTP setup |
| DELETE | `/auth/mfa` | Disable MFA |
| GET | `/auth/sessions` | List active sessions |
| DELETE | `/auth/sessions/:id` | Revoke specific session |

### Admin Endpoints

| Method | Path | Permission | Description |
|--------|------|------------|-------------|
| GET | `/api/v1/auth/providers` | role:manage | List SSO providers |
| PUT | `/api/v1/auth/provider` | role:manage | Configure OIDC/SAML |
