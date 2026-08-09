# Security Policy

Flint is a CI platform. It holds repository credentials, encrypted secrets, SSO
configuration, and it executes untrusted code on machines it provisions — so a
vulnerability here tends to be a vulnerability in whatever it builds. We take
reports seriously and would rather hear about something uncertain than not hear
about it.

## Reporting a vulnerability

**Please do not open a public issue for a security problem.**

Report privately via [GitHub Security
Advisories](https://github.com/NerdMeNot/flint/security/advisories/new), which
opens a channel visible only to you and the maintainers.

Useful things to include, as far as you have them:

- The version, commit, or deployment you tested.
- What an attacker gains, and what access they need to start.
- Steps to reproduce — a failing test, a curl sequence, or a pipeline YAML is
  ideal.
- Anything you already know about the fix.

### What to expect

| | |
|---|---|
| First response | within 3 working days |
| Assessment and severity | within 7 working days |
| Fix for high/critical | prioritised over feature work |
| Credit | offered in the advisory unless you'd rather stay anonymous |

We'll tell you what we think the severity is and why. If we disagree with your
assessment we'll explain our reasoning rather than just closing it — and if
you push back with something we missed, we'd like to know.

## Scope

Flint is pre-1.0 and self-hosted; there is no Flint-operated production service
to test against. Please test against your own deployment.

**In scope**

- The control plane (`flint server`): API, authentication (OIDC/SAML/local/device
  flow), RBAC, webhook handling.
- The machine agent (`flint-agent`): registration, token handling, step
  execution, workspace isolation.
- Secret storage and the envelope-encryption scheme.
- Pipeline evaluation: expression injection, module resolution, anything that
  turns pipeline YAML into unintended execution on the control plane.
- The compute-provider layer: credential handling, instance lifecycle.

**Out of scope**

- Anything requiring an already-compromised control-plane host or database.
- Vulnerabilities in a dependency with no reachable call path from Flint —
  though please still tell us; we run `govulncheck` in CI and may be wrong
  about reachability.
- Missing hardening that is documented as deliberate. In particular: steps run
  as containers with pid/mount isolation and a **shared host network**, which is
  suitable for trusted tenants only. Multi-tenant isolation (user-namespace
  remap, per-step netns, `isolation: dedicated`) is tracked in
  `docs/design/fleet/security.md` and is known-incomplete, not an oversight.
- Denial of service through ordinary resource exhaustion (a pipeline that
  requests a lot of machines). Bypassing a configured spend or machine **cap** is
  in scope.
- Reports from automated scanners with no demonstrated exploit path.

## Supported versions

Pre-1.0, fixes land on `main` and in the next release. There are no backports to
older tags yet; that changes at 1.0.

## Security practices in this repository

So you know what's already enforced, and where the gaps are:

- `govulncheck` runs on every PR against the full module graph.
- `golangci-lint` runs on every PR, reporting untruncated.
- Secrets are envelope-encrypted (AES-256-GCM) with a versioned master key.
- Every machine state change flows through a single audited transition
  chokepoint with an append-only event log.
- Every provisioning and termination decision is recorded in a decision ledger
  with its inputs and rejected alternatives.
