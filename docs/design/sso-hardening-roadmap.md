# SSO Hardening & "Ahead of the Curve" Roadmap

## Context

Flint shipped a full SSO overhaul (OIDC + SAML wizard, configurable mapping, group→role,
SCIM 2.0). A deep audit — benchmarking against WorkOS, Okta, Auth0, Entra, Stytch, Cloudflare
Access, plus the OSS field (Keycloak, Zitadel, Authentik, Ory Polis, GitLab, Grafana) — found we
are **already ahead of every OSS peer on breadth**: no OSS competitor ships free SAML + free SCIM
(users *and* groups) + group/team sync + a restart-free admin UI. We ship all four.

The goal of this roadmap: close the real security/correctness gaps, then claim the **cutting-edge
UX moves almost nobody (OSS or commercial) ships** — so Flint's SSO is unambiguously
ahead-of-the-curve for an OSS project, while staying simple and easy to understand.

Reassurance from the audit: the SAML-library catastrophes (XSW, multi-assertion bypass
CVE-2022-41912, comment-injection, ACS XSS) are **not our exposure** — we're on
`crewjam/saml v0.5.1` and `go-oidc v3.17.0`, both current and patched.

## Locked product decisions

- **Single global IdP** — one OIDC + one SAML per instance (current model). **Out of scope,
  permanently:** multiple connections per org, per-email-domain home-realm routing, DNS-TXT
  domain verification, and self-service "setup links" (all multi-tenant patterns; the wizard is
  our setup experience).
- **Manual / local users via admin** — already implemented (`handleCreateUser` + `CreateLocalUser`
  + `settings.users` UI + password auth). This is also our **break-glass** path. Constraint: any
  future "enforce SSO" toggle must never lock out local/manual accounts.

## Phase A — Security correctness (P0, do first; small + non-controversial)

Each is concrete and verified in the current code.

1. **PKCE (S256) on the OIDC code flow.** `oidc.go:79` uses `AuthCodeURL(state, Nonce)` with no
   `code_challenge`. Add S256 PKCE: generate a verifier per login, store it alongside the device
   code / oauth state, send `code_challenge`, pass `code_verifier` on `Exchange`. RFC 9700 requires
   PKCE for all client types.
2. **Encrypt the IdP refresh token at rest.** `auth_handlers.go:650` / `idpsync.go:154` write
   `json.Marshal(idpToken)` straight into `sessions.idp_token_enc` (the column name lies; the error
   is swallowed). Route it through the existing `secretstore`/`pkg/secret` envelope encryption used
   for provider configs. Decrypt in `idpsync` before use.
3. **Rate limiting on `/auth/oidc/callback`, `/auth/saml/acs`, and `/scim/v2/*`.** Password login is
   throttled; these are not. Add IP/identifier-based throttling (reuse the password-login limiter).
4. **SCIM org-scoping + filter safety.** `GetUserByID`/`GetTeam` in SCIM handlers look up by raw
   UUID — harmless under single-org but an IDOR if it ever isn't; add the org predicate defensively.
   `scimFilterEq` silently returns ALL users on an unsupported compound filter — reject unsupported
   filters with a SCIM error instead of over-returning.
5. **Audit coverage.** Log failed SSO logins (missing/invalid provider, validation failure) and SCIM
   user/group mutations to the audit table (currently only token generate/revoke + config changes).

Ships as one focused PR.

## Phase B — The "ahead of the curve" UX moves (the crown)

These are the cutting-edge patterns the research found almost no product — and **zero OSS** —
combines. Each is independently shippable.

1. **Decoded "Test sign-in".** Today's test does discovery only. Add a real login round-trip that
   returns and **displays the exact claims/assertion the IdP emitted** (Cloudflare's killer feature;
   "no OSS product has it"). Feeds directly into the mapping step. Highest leverage.
2. **Bundled mock IdP** (à la SSOReady DummyIDP / Zitadel MockSAML) so the first login is never
   production — supports SP-initiated, IdP-initiated, and error scenarios for dev + onboarding.
3. **Visual claim→role mapper.** Replace free-text claim/attribute names with point-and-click from
   the *real incoming payload* captured by the test sign-in. "Every OSS product forces code or raw
   strings here — second-biggest opening." Builds on #1.
4. **Certificate-expiry warnings + metadata-URL auto-refresh.** The silent SSO outage GitHub,
   Linear, Figma, Slack all ignore. Check IdP/SP cert validity, surface "expires in N days", and
   refresh IdP metadata on a schedule when a metadata URL is configured.
5. **Field-level failed-login diagnostics + a per-connection sign-in log.** Name the exact mismatched
   field ("Audience X ≠ expected Y", "clock skew N min") with decoded request/response, backed by a
   recent sign-in list (WorkOS/Scalekit model).

Note: our **declarative deny-by-default (strict mode)** is *already* a cutting-edge differentiator
(only Cloudflare/Entra ship it cleanly) — keep and surface it prominently.

## Phase C — Completeness to match commercial leaders (P1)

1. **Single Logout** — SAML SLO + OIDC RP-initiated/back-channel logout, so logout ends the IdP
   session, not just ours.
2. **SAML deprovisioning / periodic re-sync.** The sync daemon is OIDC-only; a SAML user removed
   from a group in the IdP keeps access until session expiry. Covered by SCIM *if* the IdP does SCIM;
   otherwise a real hole. Add periodic re-evaluation (and lean on SCIM as the authoritative channel).
3. **Optional "Require SSO" toggle** — disable password login for SSO-eligible users **while keeping
   local/manual admin accounts as break-glass** (the locked decision). Lighter now that break-glass
   is solved by manual users.
4. **Azure group GUID→name resolution + overage handling** — optional Graph lookup so admins map by
   name, and detect the `_claim_sources`/overage case so large-group users aren't silently
   under-privileged.

## Phase D — Polish

Recovery codes for local admins; OIDC issuer autodiscovery from an email domain (WebFinger);
auth success/latency metrics; integration tests for `oidc.go`/`saml.go` (currently untested);
"test-before-enforce" structural guardrail on the Require-SSO toggle.

## Sequencing

**Phase A → highest-leverage Phase B (1–3: decoded test, mock IdP, visual mapper) → Phase C → Phase B (4–5) → Phase D.**
Phase A is non-negotiable and ships first. Phase B 1–3 is what makes the UX "unheard of." Each phase
is an independent PR, verified live (build, unit tests, real browser/stack run) as with the original
overhaul.

## Verification

Per phase: `go build`/`vet`/`gofmt`, unit tests (mapping/sync/SCIM + new oidc/saml tests), `tsc`,
`bun run build`, and a live run against Postgres + server + web — including a real OIDC discovery
(Google) and the bundled mock IdP for end-to-end SAML/SCIM once it exists.
