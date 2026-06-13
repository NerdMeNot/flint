# Grouping & Classification Taxonomy

> Status: **Proposed** (decision recorded; implementation not started).
> Supersedes the "environment as a global scope dimension" framing in
> [environments.md](environments.md) — environments keep all their CI/governance
> roles but stop being a project-grouping axis (see "Environment relocation").

## Problem

How should Flint classify and filter projects and workflows so users can find
and focus on what they care about? Today the UI exposes two peer filters in a
global scope selector — **Workspace** and **Environment** — as if they are the
same kind of choice. They are not, and that mismatch is the source of recurring
confusion.

## The core insight

Every project/workflow lives on **three distinct axes** that behave differently:

| Axis | Question | Cardinality | Changes | Carries permissions? |
|------|----------|-------------|---------|----------------------|
| **Ownership** | Whose is this? Who can touch it? | exactly **one** | rarely | yes |
| **Classification** | What is this; what is it like? | **many** | fluid | no |
| **Runtime / lifecycle** | Where/how is it running *now*? | **one per run**, many per project | per execution | scopes actions, not projects |

- **Workspace** is the *ownership* axis.
- **Tags** are the *classification* axis (Flint barely has these today).
- **Environment** is the *runtime* axis — a property of a **run/deployment**, not
  of a project. A project doesn't live *in* production; it *deploys to* it.

Putting Workspace and Environment side-by-side as peer filters is a category
error: ownership is a strict 1-of partition, while environment-on-a-project is
1-of-many and only collapses to a single value on a *run*.

This model also has to work for **Workflows** (no environment, no repo) and
future **Load Testing** — which means the universal axes are ownership + tags;
environment is a CI-deployment-specific concern that cannot be a core grouping
axis.

## Decision

Adopt the **proven three-axis model**, optimized for **findability**, with
**progressive disclosure** (flat for small teams, scales to enterprise without
re-architecting):

1. **Workspace — ownership spine.** Exactly one per project/workflow. The
   permission, quota, and billing boundary. Kept shallow (flat, or ≤1 level of
   nesting). Mostly invisible until a second workspace exists.
2. **Tags — classification.** Many per project. A small set of **curated,
   namespaced** tags (`domain:`, `tier:`, `lang:`, `compliance:`) with
   autocomplete + optional allowed-values, governed by admins, that power
   filtering and rollups — plus free-form tags for the long tail. The curated
   registry is the single detail that prevents `prod`/`Prod`/`prd` tag sprawl.
3. **Views — navigation.** Named, shareable saved selectors over
   `workspace + tags + status + product`. Overlapping and virtual ("folders you
   actually wanted", no single-parent tyranny). Personal, team, or org scope.
   This is the daily driver.

### Progressive-disclosure ladder

The model is identical at every scale; only the surface area shown grows.

- **Tiny (1 team, <~20 projects):** no Workspace UI (one implicit default), no
  tags required. Flat project list + search + **system smart-views** ("Owned by
  me", "Failing now", "Needs approval"). Zero taxonomy homework.
- **Growing (a few teams):** Workspace picker appears the moment a second
  workspace exists. Tags appear when someone adds one. Views become shareable.
- **Large/enterprise:** curated tag registry, ≤1 level of workspace nesting,
  cross-workspace org-wide views, health rollups by `domain`/`tier`, RBAC
  scoping. Present but only surfaces when the data justifies it.

Principle: **features surface when the data justifies them, not when an admin
configures them.**

### Findability-first navigation

Lean on search + views so the visible hierarchy stays minimal.

1. **Facet-aware search (⌘K).** `tier:1 status:failed` filters; `payments`
   matches names, repos, tags. Absorbs most "get me there fast" load.
2. **Sidebar is Views, not a folder tree:**

```
┌─ FLINT ─────────────────┐
│ ⌕ Search ⌘K             │  ← facet-aware
│ VIEWS                   │  ← daily driver
│  ★ Owned by me          │     (system smart-views, zero setup)
│  ★ Failing now          │
│  ★ Needs approval       │
│  ☆ Payments · prod      │     (saved views)
│  ☆ Tier-1 services      │
│  + New view             │
│ WORKSPACES              │  ← ownership spine (hidden when only 1)
│  ▸ Payments             │
│  ▸ Platform             │
│ Deployments             │  ← environment lives here now
│ Settings                │
└─────────────────────────┘
```

3. **Facet filter-bar** atop any list; "Save as view" promotes a slice to the
   sidebar. Environment is **not** in this bar — it's not a project facet.

```
[ Workspace ▾ ]  [ domain ▾ ]  [ tier ▾ ]  [ status ▾ ]      + Save as view
```

## Environment relocation

Environment is **kept** — it is correct almost everywhere it appears. The work
is to stop *presenting* it as a global grouping peer of Workspace, and surface it
only where it functionally applies. Grounded in the current code:

- `projects` has **no** environment column — environment was never a project
  attribute. ✓
- `pipeline_runs.environment` exists (indexed) — environment-on-a-run (1-of) is
  already modeled correctly. ✓
- In the web, `environmentMatches` only filters **runs and gates**, never the
  projects list — so the bug is information architecture, not data: environment
  sits in the always-on global `ScopeSelector` as if it were a top-level "place".

### KEEP (legitimate CI/governance roles — untouched)

- Pipeline language: `environments:` narrowing funnel (pipeline → trigger →
  job), `promotion` triggers — `pkg/pipeline/*`, `internal/products/ci/*`.
- Run targeting: `StartWorkflowInput.Environment`, `pipeline_runs.environment`,
  pod injection — `internal/core/engine/*`, `dispatch.go`.
- Governance: protected environments + gates, env-scoped secrets and variables —
  `protected_environments`, `env_resolve`, `secretstore`, `agent/secrets`,
  `settings.environments/*`, `settings.variables`.
- The trigger modal's "deploy to which environment" picker (selects a run's
  target).

### CHANGE (web IA — small, surgical)

- Remove environment from the global `ScopeSelector` / `ScopeChips` /
  `scope-context` (`environments[]`, the global `environmentMatches`).
- Re-home it as a **local filter** on the surfaces it applies to: a filter pill
  on the Runs list and Gates list (next to the existing Status pill), and the new
  Deployments view. Same logic, page-scoped instead of app-global.
- Dashboard recent-runs env filter follows (local, or dropped).

### VERIFY (likely already correct)

- RBAC env-scope (`casbin.go`, `role_scopes`, `api_key_scopes`, `middleware.go`)
  scopes **actions** (deploy/approve to an env), not project reads. Projects have
  no environment, so this is almost certainly already true — confirm and document.
- `internal/products/workflows/workflow.go` env reference — confirm it's
  incidental (shared type), not Workflows carrying a deployment environment.

### New surface: Deployments view

The high-value thing environments unlock and that no folder/filter expresses:
*"Checkout v2.8.1 is live in prod, v2.9.0 in staging, v2.9.1 in canary."*
Per-environment, what-version-is-where. Replaces the urge to treat environment as
a browsing axis.

## Data model (target)

```
Project/Workflow:  workspaceId (1)  ·  tags: {key?, value}[]   (no environment)
Workspace:         ownership boundary · members · quota · parentId? (≤1 level)
Tag registry:      key → { label, allowedValues?, color }      (curated subset)
View:              name · selector(workspace, tags, status, product)
                        · scope(personal|team|org) · owner
Run/Deployment:    environment (1)                              (env lives here)
ProtectedEnvironment: gates · approvers · scoped-secrets        (governance)
```

## Implementation phasing (high level)

1. **Data model & migrations** — `tags` on projects/workflows + curated tag
   registry; `views` table; confirm workspace as the sole project-grouping FK.
   No environment schema change.
2. **Backend APIs** — tag CRUD + registry; view CRUD; list endpoints accept
   `workspace`, `tags`, `status` server-side filters (replacing client-side
   filtering); facet-aware search.
3. **Web IA** — sidebar-of-views + system smart-views; facet filter-bar; ⌘K
   facet search; **demote environment** from the global scope selector to a local
   run/gate filter.
4. **Deployments view** — what-version-is-where per environment.
5. **RBAC reconciliation** — confirm/document env-scope as action-scope; project
   scope by workspace only.
6. **Migration** — backfill: existing per-project workspace stays; seed a couple
   of default views; no environment data migration needed.

Ship findability value early: even before tags exist, system smart-views +
facet search + the environment demotion are independently valuable.

## Open questions

- **Naming:** keep **"Workspace"** (neutral, ownership connotation) or move to
  **"Team"** (Linear-style, if ownership == team)?
- **Workspace nesting:** flat in v1, or allow ≤1 level immediately?
- **Tag governance:** who curates the registry — org admins only, or per-workspace
  owners too?
