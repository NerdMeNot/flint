# Environments in Flint

## Overview

Environments are runtime contexts for pipeline execution. They are simple named entities — protection rules come from RBAC (role scoping), not from the environment itself.

## Environment Model

```
Environment {
  id:        string
  name:      string    // "production"
  slug:      string    // "production"
  createdAt: string
}
```

That's it. No deploy windows, no approvers, no min_role, no branch restrictions on the environment. Those concerns are handled by:
- **RBAC**: Roles scoped to environments (e.g., "Prod Release Manager" scoped to production)
- **Pipeline config**: `only: [production]` on gate steps, branch restrictions in the pipeline YAML
- **Environment variables**: Configuration that differs per environment

## Environment Variables

Variables are defined once and given values per environment. This prevents the "forgot to set it in staging" problem.

### Model

```
EnvVariable {
  id:          string
  name:        string          // "CLUSTER_URL"
  description: string?         // "Base URL for the deployment cluster"
  isSecret:    boolean         // true = write-only, values never returned
  createdAt:   string
}

EnvVariableValue {
  variableId:    string
  environmentId: string?       // null = global default
  value:         string        // encrypted if isSecret
  updatedAt:     string
}
```

### Global defaults

A variable can have a **global default** value (`environmentId = null`). This value is used when no environment-specific override exists.

```
APP_NAME:
  global: "flint"           ← used in all environments
  (no per-env overrides)

LOG_LEVEL:
  global: "info"            ← default
  production: "warn"        ← overrides global in production
  dev: "debug"              ← overrides global in dev
  staging: (inherited)      ← uses global "info"
```

Resolution order:
1. Environment-specific value if set → use it
2. Global default if set → use it
3. Neither → variable is unset for that environment (flagged as missing)
```

### How it works

1. Admin creates a variable: `CLUSTER_URL` (not secret), `STRIPE_KEY` (secret)
2. For each environment, they set the value:
   - `CLUSTER_URL` in dev = `localhost`, staging = `staging.acme.com`, production = `api.acme.com`
   - `STRIPE_KEY` in staging = `sk_test_...`, production = `sk_live_...`
3. When a pipeline runs in an environment, all variables for that environment are injected
4. Pipelines reference them as `${{ env.CLUSTER_URL }}`

### Matrix view (UI)

The primary UI is a matrix: variables as rows, environments as columns.

```
                  dev            staging              production
CLUSTER_URL      localhost       staging.acme.com     api.acme.com
REPLICAS         1               2                    5
DB_HOST          localhost       staging-db           prod-db
STRIPE_KEY       ●●●●●●          ●●●●●●               ●●●●●●
LOG_LEVEL        debug           info                 warn
```

- Empty cells are visually flagged — shows which env is missing a value
- Secret values show as dots (never returned from API)
- Click a cell to edit the value
- "Add variable" creates a row — then fill in per-environment values
- "Add environment" creates a column — then fill in values for existing variables

### Secrets vs Variables

Both use the same model. The only difference:
- **Variable** (`isSecret: false`): value is stored plaintext and returned in API responses
- **Secret** (`isSecret: true`): value is encrypted at rest, never returned in API responses (only `●●●●●●` placeholder), write-only after creation

This replaces the old separate "Secrets" page — secrets are now just secret-flagged environment variables.

## Database Schema

```sql
-- Environments (simplified — just name + slug)
CREATE TABLE environments (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id     UUID NOT NULL REFERENCES orgs(id),
  name       TEXT NOT NULL,
  slug       TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (org_id, slug)
);

-- Environment variables (defined once, valued per env)
CREATE TABLE env_variables (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id      UUID NOT NULL REFERENCES orgs(id),
  name        TEXT NOT NULL,
  description TEXT,
  is_secret   BOOLEAN NOT NULL DEFAULT false,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (org_id, name)
);

-- Per-environment values
CREATE TABLE env_variable_values (
  variable_id    UUID NOT NULL REFERENCES env_variables(id) ON DELETE CASCADE,
  environment_id UUID NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
  value          TEXT NOT NULL,      -- encrypted if is_secret
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (variable_id, environment_id)
);
```

## Pipeline Usage

```yaml
steps:
  deploy:
    environment: ${{ target_env }}
    run: |
      helm upgrade myapp ./charts \
        --set cluster=${{ env.CLUSTER_URL }} \
        --set replicas=${{ env.REPLICAS }}
```

`${{ env.VARIABLE_NAME }}` resolves at runtime from the run's target environment.

## UI Pages

### Admin > Environments

List of environments — simple cards with name, slug, variable count. Create/delete actions.

### Admin > Environment Variables (new page, replaces Secrets)

The matrix view. Primary interaction surface for managing configuration across environments.

- Left column: variable names with type badge (variable vs secret)
- Column headers: environment names
- Cells: editable values (click to edit). Empty cells highlighted.
- Footer: coverage count per environment ("5/7 set")
- Actions: add variable, delete variable
