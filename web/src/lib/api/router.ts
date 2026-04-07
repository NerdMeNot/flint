import { os } from '@orpc/server'
import { z } from 'zod'
import {
  RunStatus,
} from './types'
import {
  getDashboardSummary,
  getRuns,
  getProjects,
  getSteps,
  getGates,
  getUsers,
  getStepLogs,
  getWorkspaces,
  getTeams,
  getTeamWithMembers,
  getRoles,
  getAssignments,
  getEnvironments,
  getEnvVariables,
  getEnvVariableValues,
  getApiKeys,
  getAuditEntries,
  getRunnerPools,
  getForgeConnections,
  getAuthUser,
  getProjectPipelines,
  getPersonalTokens,
} from './mocks'

// ---------------------------------------------------------------------------
// Pagination helper
// ---------------------------------------------------------------------------

function paginate<T extends { id: string }>(
  items: T[],
  opts: { limit?: number; cursor?: string },
  defaultLimit = 20,
): { items: T[]; nextCursor?: string } {
  const limit = opts.limit ?? defaultLimit
  const startIdx = opts.cursor ? items.findIndex((i) => i.id === opts.cursor) + 1 : 0
  const page = items.slice(startIdx, startIdx + limit)
  const nextCursor = page.length === limit ? page[page.length - 1]?.id : undefined
  return { items: page, nextCursor }
}

// ---------------------------------------------------------------------------
// Stats (replaces dashboard)
// ---------------------------------------------------------------------------

const stats = {
  get: os.handler(async () => {
    return getDashboardSummary()
  }),
}

// ---------------------------------------------------------------------------
// Projects
// ---------------------------------------------------------------------------

const projects = {
  list: os
    .input(
      z.object({
        workspace: z.optional(z.string()),
        limit: z.optional(z.number()),
        cursor: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      let items = getProjects()
      if (input.workspace) {
        items = items.filter((p) => p.workspace === input.workspace)
      }
      return paginate(items, input)
    }),

  get: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      const project = getProjects().find((p) => p.id === input.id)
      if (!project) throw new Error(`Project ${input.id} not found`)
      return project
    }),

  pipelines: os
    .input(z.object({ projectId: z.string() }))
    .handler(async ({ input }) => {
      return getProjectPipelines(input.projectId)
    }),
}

// ---------------------------------------------------------------------------
// Runs
// ---------------------------------------------------------------------------

const runs = {
  list: os
    .input(
      z.object({
        projectId: z.optional(z.string()),
        status: z.optional(RunStatus),
        branch: z.optional(z.string()),
        limit: z.optional(z.number()),
        cursor: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      let items = getRuns()

      if (input.projectId) {
        items = items.filter((r) => r.projectId === input.projectId)
      }
      if (input.status) {
        items = items.filter((r) => r.status === input.status)
      }
      if (input.branch) {
        items = items.filter((r) => r.branch === input.branch)
      }

      return paginate(items, input)
    }),

  get: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      const run = getRuns().find((r) => r.id === input.id)
      if (!run) throw new Error(`Run ${input.id} not found`)
      return run
    }),

  steps: os
    .input(z.object({ runId: z.string() }))
    .handler(async ({ input: _input }) => {
      // In a real app, steps would be fetched per run. Return the default pipeline.
      return getSteps()
    }),

  stepLogs: os
    .input(z.object({ runId: z.string(), stepName: z.string() }))
    .handler(async ({ input }) => {
      const logs = getStepLogs()
      const content = logs[input.stepName]
      if (content === undefined) {
        return { lines: '' }
      }
      return { lines: content }
    }),

  trigger: os
    .input(z.object({ projectId: z.string(), branch: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      console.log(`[mock] Triggering run for project=${input.projectId} branch=${input.branch ?? 'main'}`)
      return {
        id: `r-${Date.now()}`,
        status: 'pending' as const,
        message: 'Run triggered successfully',
      }
    }),

  cancel: os
    .input(z.object({ runId: z.string() }))
    .handler(async ({ input }) => {
      console.log(`[mock] Cancelling run=${input.runId}`)
      return { success: true, message: `Run ${input.runId} cancelled` }
    }),

  retry: os
    .input(z.object({ runId: z.string() }))
    .handler(async ({ input }) => {
      console.log(`[mock] Retrying run=${input.runId}`)
      return {
        id: `r-${Date.now()}`,
        status: 'pending' as const,
        message: `Retry of ${input.runId} queued`,
      }
    }),
}

// ---------------------------------------------------------------------------
// Gates
// ---------------------------------------------------------------------------

const gates = {
  list: os
    .input(
      z.object({
        status: z.optional(z.enum(['pending', 'approved', 'rejected'])),
        limit: z.optional(z.number()),
        cursor: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      let items = getGates()

      if (input.status) {
        items = items.filter((g) => g.status === input.status)
      }

      if (input.cursor) {
        const idx = items.findIndex((g) => `${g.runId}:${g.stepName}` === input.cursor)
        if (idx !== -1) items = items.slice(idx + 1)
      }

      const limit = input.limit ?? 20
      const page = items.slice(0, limit)
      const nextCursor = page.length === limit
        ? `${page[page.length - 1]!.runId}:${page[page.length - 1]!.stepName}`
        : undefined

      return { items: page, nextCursor }
    }),

  approve: os
    .input(z.object({ runId: z.string(), stepName: z.string(), comment: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      console.log(`[mock] Approving gate run=${input.runId} step=${input.stepName}`)
      return { success: true, message: `Gate ${input.stepName} approved` }
    }),

  reject: os
    .input(z.object({ runId: z.string(), stepName: z.string(), reason: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      console.log(`[mock] Rejecting gate run=${input.runId} step=${input.stepName}`)
      return { success: true, message: `Gate ${input.stepName} rejected` }
    }),
}

// ---------------------------------------------------------------------------
// Workspaces
// ---------------------------------------------------------------------------

const workspaces = {
  list: os
    .input(
      z.object({
        limit: z.optional(z.number()),
        cursor: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      return paginate(getWorkspaces(), input)
    }),

  create: os
    .input(z.object({ name: z.string(), slug: z.string(), description: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      console.log(`[mock] Creating workspace name=${input.name}`)
      return {
        id: `ws-${Date.now()}`,
        name: input.name,
        slug: input.slug,
        description: input.description,
        projectCount: 0,
        createdAt: new Date().toISOString(),
      }
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      console.log(`[mock] Deleting workspace id=${input.id}`)
      return { success: true, message: `Workspace ${input.id} deleted` }
    }),
}

// ---------------------------------------------------------------------------
// Environments
// ---------------------------------------------------------------------------

const environments = {
  list: os
    .input(
      z.object({
        limit: z.optional(z.number()),
        cursor: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      return paginate(getEnvironments(), input)
    }),

  create: os
    .input(z.object({ name: z.string(), slug: z.string() }))
    .handler(async ({ input }) => {
      console.log(`[mock] Creating environment name=${input.name}`)
      return { id: `env-${Date.now()}`, name: input.name, slug: input.slug, createdAt: new Date().toISOString() }
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      console.log(`[mock] Deleting environment id=${input.id}`)
      return { success: true }
    }),
}

// ---------------------------------------------------------------------------
// Environment variables
// ---------------------------------------------------------------------------

const envVariables = {
  list: os
    .input(
      z.object({
        limit: z.optional(z.number()),
        cursor: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      return paginate(getEnvVariables(), input)
    }),

  values: os
    .input(
      z.object({
        limit: z.optional(z.number()),
        cursor: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      const items = getEnvVariableValues()
      return paginate(items, input)
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
      console.log(`[mock] Creating env variable name=${input.name} scope=${input.scope} secret=${input.isSecret}`)
      return {
        id: `var-${Date.now()}`,
        name: input.name,
        description: input.description,
        scope: input.scope,
        isSecret: input.isSecret,
        value: input.scope === 'global' ? input.value : undefined,
        createdAt: new Date().toISOString(),
      }
    }),

  setValue: os
    .input(z.object({ variableId: z.string(), environmentId: z.optional(z.string()), value: z.string() }))
    .handler(async ({ input }) => {
      console.log(`[mock] Setting var=${input.variableId} env=${input.environmentId ?? 'global'}`)
      return { variableId: input.variableId, environmentId: input.environmentId, value: input.value, updatedAt: new Date().toISOString() }
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      console.log(`[mock] Deleting env variable id=${input.id}`)
      return { success: true }
    }),
}

// ---------------------------------------------------------------------------
// Teams
// ---------------------------------------------------------------------------

const teams = {
  list: os
    .input(
      z.object({
        limit: z.optional(z.number()),
        cursor: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      return paginate(getTeams(), input)
    }),

  get: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      const team = getTeamWithMembers(input.id)
      if (!team) throw new Error(`Team ${input.id} not found`)
      return team
    }),

  create: os
    .input(z.object({ name: z.string(), slug: z.string() }))
    .handler(async ({ input }) => {
      console.log(`[mock] Creating team name=${input.name}`)
      return {
        id: `t-${Date.now()}`,
        name: input.name,
        slug: input.slug,
        memberCount: 0,
      }
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      console.log(`[mock] Deleting team id=${input.id}`)
      return { success: true, message: `Team ${input.id} deleted` }
    }),
}

// ---------------------------------------------------------------------------
// Users
// ---------------------------------------------------------------------------

const users = {
  list: os
    .input(
      z.object({
        limit: z.optional(z.number()),
        cursor: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      return paginate(getUsers(), input)
    }),
}

// ---------------------------------------------------------------------------
// Roles
// ---------------------------------------------------------------------------

const roles = {
  list: os
    .input(
      z.object({
        limit: z.optional(z.number()),
        cursor: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      return paginate(getRoles(), input)
    }),

  create: os
    .input(
      z.object({
        name: z.string(),
        slug: z.string(),
        description: z.optional(z.string()),
        permissions: z.array(z.object({ object: z.string(), action: z.string() })),
        workspaces: z.array(z.string()),
        environments: z.array(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      console.log(`[mock] Creating role name=${input.name}`)
      return {
        id: `role-${Date.now()}`,
        name: input.name,
        slug: input.slug,
        description: input.description,
        isSystem: false,
        permissions: input.permissions,
        workspaces: input.workspaces,
        environments: input.environments,
      }
    }),

  update: os
    .input(
      z.object({
        id: z.string(),
        name: z.optional(z.string()),
        description: z.optional(z.string()),
        permissions: z.optional(z.array(z.object({ object: z.string(), action: z.string() }))),
        workspaces: z.optional(z.array(z.string())),
        environments: z.optional(z.array(z.string())),
      }),
    )
    .handler(async ({ input }) => {
      console.log(`[mock] Updating role id=${input.id}`)
      const role = getRoles().find((r) => r.id === input.id)
      if (!role) throw new Error(`Role ${input.id} not found`)
      return { ...role, ...Object.fromEntries(Object.entries(input).filter(([_, v]) => v !== undefined)) }
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      console.log(`[mock] Deleting role id=${input.id}`)
      return { success: true, message: `Role ${input.id} deleted` }
    }),

  assignments: {
    list: os
      .input(
        z.object({
          limit: z.optional(z.number()),
          cursor: z.optional(z.string()),
        }),
      )
      .handler(async ({ input }) => {
        const items = getAssignments()
        // Assignments may not have an `id` field, so return all with wrapper
        const limit = input.limit ?? 20
        const startIdx = input.cursor ? parseInt(input.cursor, 10) : 0
        const page = items.slice(startIdx, startIdx + limit)
        const nextCursor = page.length === limit ? String(startIdx + limit) : undefined
        return { items: page, nextCursor }
      }),

    create: os
      .input(z.object({ subjects: z.array(z.string()), role: z.string() }))
      .handler(async ({ input }) => {
        console.log(`[mock] Assigning role=${input.role} to ${input.subjects.length} subjects`)
        return { success: true, count: input.subjects.length }
      }),

    delete: os
      .input(z.object({ subject: z.string(), role: z.string() }))
      .handler(async ({ input }) => {
        console.log(`[mock] Removing role=${input.role} from subject=${input.subject}`)
        return { success: true, message: 'Assignment removed' }
      }),
  },
}

// ---------------------------------------------------------------------------
// API keys
// ---------------------------------------------------------------------------

const apiKeys = {
  list: os
    .input(
      z.object({
        limit: z.optional(z.number()),
        cursor: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      return paginate(getApiKeys(), input)
    }),

  create: os
    .input(
      z.object({
        name: z.string(),
        role: z.string(),
        workspaces: z.array(z.string()),
        environments: z.array(z.string()),
        expiresAt: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      console.log(`[mock] Creating API key name=${input.name} role=${input.role}`)
      return {
        id: `ak-${Date.now()}`,
        name: input.name,
        role: input.role,
        workspaces: input.workspaces,
        environments: input.environments,
        expiresAt: input.expiresAt,
        createdBy: 'alice@acme.dev',
        createdAt: new Date().toISOString(),
        token: `flint_${Array.from({ length: 32 }, () => Math.random().toString(36)[2]).join('')}`,
      }
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      console.log(`[mock] Deleting API key id=${input.id}`)
      return { success: true, message: `API key ${input.id} deleted` }
    }),
}

// ---------------------------------------------------------------------------
// Personal tokens
// ---------------------------------------------------------------------------

const personalTokens = {
  list: os
    .input(
      z.object({
        userId: z.string(),
        limit: z.optional(z.number()),
        cursor: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      const items = getPersonalTokens(input.userId)
      return paginate(items, input)
    }),

  create: os
    .input(z.object({ userId: z.string(), name: z.string(), expiresAt: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      console.log(`[mock] Creating personal token name=${input.name} for user=${input.userId}`)
      return {
        id: `pt-${Date.now()}`,
        name: input.name,
        expiresAt: input.expiresAt,
        createdAt: new Date().toISOString(),
        token: `flint_pat_${Array.from({ length: 32 }, () => Math.random().toString(36)[2]).join('')}`,
      }
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      console.log(`[mock] Deleting personal token id=${input.id}`)
      return { success: true }
    }),
}

// ---------------------------------------------------------------------------
// Runners
// ---------------------------------------------------------------------------

const runners = {
  list: os
    .input(
      z.object({
        limit: z.optional(z.number()),
        cursor: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      return paginate(getRunnerPools(), input)
    }),
}

// ---------------------------------------------------------------------------
// Forge connections
// ---------------------------------------------------------------------------

const forgeConnections = {
  list: os
    .input(
      z.object({
        limit: z.optional(z.number()),
        cursor: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      return paginate(getForgeConnections(), input)
    }),
}

// ---------------------------------------------------------------------------
// Audit entries (renamed from auditLog)
// ---------------------------------------------------------------------------

const auditEntries = {
  list: os
    .input(
      z.object({
        action: z.optional(z.string()),
        limit: z.optional(z.number()),
        cursor: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      let items = getAuditEntries()

      if (input.action) {
        items = items.filter((e) => e.action === input.action)
      }

      return paginate(items, input, 50)
    }),
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

const auth = {
  me: os.handler(async () => {
    return getAuthUser()
  }),
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

const search = {
  query: os
    .input(
      z.object({
        q: z.string(),
        limit: z.optional(z.number()),
      }),
    )
    .handler(async ({ input }) => {
      const query = input.q.toLowerCase().trim()
      const limit = input.limit ?? 10
      if (!query) return { projects: [], runs: [] }

      const matchedProjects = getProjects()
        .filter(
          (p) =>
            p.name.toLowerCase().includes(query) ||
            p.repo.toLowerCase().includes(query) ||
            p.tags.some((t) => t.toLowerCase().includes(query)),
        )
        .slice(0, limit)

      const matchedRuns = getRuns()
        .filter(
          (r) =>
            r.projectName.toLowerCase().includes(query) ||
            r.branch.toLowerCase().includes(query) ||
            r.commitMessage.toLowerCase().includes(query),
        )
        .slice(0, limit)

      return { projects: matchedProjects, runs: matchedRuns }
    }),
}

// ---------------------------------------------------------------------------
// App router
// ---------------------------------------------------------------------------

export const appRouter = os.router({
  stats,
  projects,
  runs,
  gates,
  workspaces,
  environments,
  envVariables,
  teams,
  users,
  roles,
  apiKeys,
  personalTokens,
  runners,
  forgeConnections,
  auditEntries,
  auth,
  search,
})

export type AppRouter = typeof appRouter
