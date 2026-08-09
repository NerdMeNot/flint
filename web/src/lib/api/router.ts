import { os } from '@orpc/server'
import { z } from 'zod'
import { type PipelineRun, type Project, type DashboardSummary, type Workspace, type TagKey, type Environment, type EnvVariable, type EnvVariableValue, type ForgeConnection, type SavedView } from './types'
import { backendGet, backendPost, backendPut, backendDelete } from './backend'
import type { Capability, Paginated } from './router-shared'
import { computeProviders, decisions, machines, runners } from './router-fleet'
import { gates, projects, runs, workflows } from './router-ci'
import { apiKeys, auditEntries, org, personalTokens, roles, teams, users } from './router-admin'
import { auth } from './router-auth'

// ---------------------------------------------------------------------------
// Shared types for backend responses
// ---------------------------------------------------------------------------


// ---------------------------------------------------------------------------
// Stats
// ---------------------------------------------------------------------------

const stats = {
  get: os.handler(async () => {
    return backendGet<DashboardSummary>('/stats')
  }),
}

// ---------------------------------------------------------------------------
// Capabilities — which product sections the unified UI should render.
// ---------------------------------------------------------------------------


// meta reports the backend's data mode so the client can warn when the server is
// a local demo deployment (seeded data + simulated step execution) rather than
// production.
const meta = {
  get: os.handler(async () => {
    const m = await backendGet<{ mode: 'demo' | 'live' }>('/meta')
    return { mode: m.mode, isDemo: m.mode === 'demo' }
  }),
}

// Cached backend mode, used to gate mock fallbacks so they never surface fake
// data against a live deployment. Sticky after the first successful check.


const capabilities = {
  get: os.handler(async () => {
    return backendGet<{ products: Capability[] }>('/capabilities')
  }),
}

// ---------------------------------------------------------------------------
// Projects
// ---------------------------------------------------------------------------


// ---------------------------------------------------------------------------
// Runs
// ---------------------------------------------------------------------------


// ---------------------------------------------------------------------------
// Workflows — generic engine runs (no forge/repo). Mock-first: the backend has
// POST /workflows/runs (trigger) and GET /workflows/runs/:id (detail) but not
// yet a list endpoint, so `list` falls back to mocks until that's reconciled.
// ---------------------------------------------------------------------------


// ---------------------------------------------------------------------------
// Gates
// ---------------------------------------------------------------------------


// ---------------------------------------------------------------------------
// Workspaces
// ---------------------------------------------------------------------------

const workspaces = {
  list: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendGet<Paginated<Workspace>>('/workspaces', { limit: input.limit, cursor: input.cursor })
    }),

  create: os
    .input(z.object({ name: z.string(), slug: z.string(), description: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendPost('/workspaces', input)
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return backendDelete(`/workspaces/${input.id}`)
    }),
}

// ---------------------------------------------------------------------------
// Tag registry (curated namespaced keys)
// ---------------------------------------------------------------------------

const tags = {
  registry: {
    list: os
      .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
      .handler(async ({ input }) => {
        return backendGet<Paginated<TagKey>>('/tags', { limit: input.limit, cursor: input.cursor })
      }),

    create: os
      .input(z.object({
        key: z.string(),
        label: z.string(),
        allowedValues: z.optional(z.array(z.string())),
        color: z.optional(z.string()),
      }))
      .handler(async ({ input }) => {
        return backendPost('/tags', input)
      }),

    update: os
      .input(z.object({
        id: z.string(),
        label: z.string(),
        allowedValues: z.optional(z.array(z.string())),
        color: z.optional(z.string()),
      }))
      .handler(async ({ input }) => {
        return backendPut(`/tags/${input.id}`, input)
      }),

    delete: os
      .input(z.object({ id: z.string() }))
      .handler(async ({ input }) => {
        return backendDelete(`/tags/${input.id}`)
      }),
  },
}

// ---------------------------------------------------------------------------
// Environments
// ---------------------------------------------------------------------------

const environments = {
  list: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendGet<Paginated<Environment>>('/environments', { limit: input.limit, cursor: input.cursor })
    }),

  create: os
    .input(z.object({ name: z.string(), slug: z.string() }))
    .handler(async ({ input }) => {
      return backendPost('/environments', input)
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return backendDelete(`/environments/${input.id}`)
    }),
}

// ---------------------------------------------------------------------------
// Environment variables
// ---------------------------------------------------------------------------

const envVariables = {
  list: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendGet<Paginated<EnvVariable>>('/env-variables', { limit: input.limit, cursor: input.cursor })
    }),

  values: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendGet<Paginated<EnvVariableValue>>('/env-variables/values', { limit: input.limit, cursor: input.cursor })
    }),

  create: os
    .input(z.object({
      name: z.string(),
      description: z.optional(z.string()),
      scope: z.enum(['global', 'environment']),
      isSecret: z.boolean(),
      value: z.optional(z.string()),
    }))
    .handler(async ({ input }) => {
      return backendPost('/env-variables', input)
    }),

  setValue: os
    .input(z.object({ variableId: z.string(), environmentId: z.optional(z.string()), value: z.string() }))
    .handler(async ({ input }) => {
      return backendPut(`/env-variables/${input.variableId}/values`, input)
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return backendDelete(`/env-variables/${input.id}`)
    }),
}

// ---------------------------------------------------------------------------
// Teams
// ---------------------------------------------------------------------------


// ---------------------------------------------------------------------------
// Users
// ---------------------------------------------------------------------------


// ---------------------------------------------------------------------------
// Roles
// ---------------------------------------------------------------------------


// ---------------------------------------------------------------------------
// API keys
// ---------------------------------------------------------------------------


// ---------------------------------------------------------------------------
// Personal tokens
// ---------------------------------------------------------------------------


// ---------------------------------------------------------------------------
// Runners
// ---------------------------------------------------------------------------




// ---------------------------------------------------------------------------
// Fleet — machines, decisions, placement
// ---------------------------------------------------------------------------



// ---------------------------------------------------------------------------
// Compute providers (the machine sources pools draw from)
// ---------------------------------------------------------------------------



// ---------------------------------------------------------------------------
// Forge connections
// ---------------------------------------------------------------------------

const forgeConnections = {
  list: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendGet<Paginated<ForgeConnection>>('/forge-connections', { limit: input.limit, cursor: input.cursor })
    }),
}

// ---------------------------------------------------------------------------
// Audit entries
// ---------------------------------------------------------------------------


// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------



// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

const search = {
  query: os
    .input(z.object({ q: z.string(), limit: z.optional(z.number()) }))
    .handler(async ({ input }) => {
      return backendGet<{ projects: Project[]; runs: PipelineRun[] }>('/search', { q: input.q, limit: input.limit })
    }),
}

const views = {
  list: os.handler(async () => {
    return backendGet<{ items: SavedView[] }>('/views')
  }),

  create: os
    .input(z.object({
      name: z.string(),
      route: z.string(),
      search: z.record(z.string(), z.unknown()),
    }))
    .handler(async ({ input }) => {
      return backendPost('/views', input)
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return backendDelete(`/views/${input.id}`)
    }),
}

// ---------------------------------------------------------------------------
// App router
// ---------------------------------------------------------------------------

export const appRouter = os.router({
  meta,
  stats,
  capabilities,
  projects,
  runs,
  workflows,
  gates,
  workspaces,
  tags,
  environments,
  envVariables,
  teams,
  users,
  roles,
  apiKeys,
  personalTokens,
  runners,
  machines,
  decisions,
  computeProviders,
  forgeConnections,
  auditEntries,
  org,
  auth,
  search,
  views,
})

export type AppRouter = typeof appRouter
