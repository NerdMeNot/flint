import { os } from '@orpc/server'
import { z } from 'zod'
import {
  RunStatus,
  type PipelineRun,
  type PipelineStep,
  type PipelineDefinition,
  type Project,
  type DashboardSummary,
  type Gate,
  type Workspace,
  type TagKey,
  type Environment,
  type EnvVariable,
  type EnvVariableValue,
  type Team,
  type TeamWithMembers,
  type User,
  type Role,
  type Assignment,
  type ApiKey,
  type PersonalToken,
  type RunnerPool,
  type ForgeConnection,
  type AuditEntry,
  type AuthUser,
  type Org,
  type Session,
  type MfaSetup,
  type AuthProviders,
  type WorkflowRun,
  type WorkflowRunDetail,
} from './types'
import {
  backendGet,
  backendPost,
  backendPut,
  backendPatch,
  backendDelete,
  BackendUnavailableError,
  isBackendUnreachable,
} from './backend'
import { apiMode } from './mode'
import * as mocks from './mocks'

// ---------------------------------------------------------------------------
// Shared types for backend responses
// ---------------------------------------------------------------------------

type Paginated<T> = { items: T[]; nextCursor?: string }

// withFallback / safe are mode-aware (see ./mode):
//   mock — skip the backend entirely and use the mock path.
//   live — call the backend and let errors (including unreachability) surface.
//   auto — try the backend, fall back to the mock path only when unreachable.
//
// withFallback is for reads; safe is for writes. safe's second arg accepts a
// plain value OR a thunk — pass a thunk to MUTATE the mock store so writes feel
// real in mock mode (the next read reflects them).
async function withFallback<T>(backendCall: () => Promise<T>, mockFallback: () => T): Promise<T> {
  if (apiMode() === 'mock') return mockFallback()
  try {
    return await backendCall()
  } catch (err) {
    if (apiMode() === 'auto' && err instanceof BackendUnavailableError) return mockFallback()
    throw err
  }
}

async function safe<T>(backendCall: () => Promise<T>, fallback: T | (() => T)): Promise<T> {
  const resolve = () => (typeof fallback === 'function' ? (fallback as () => T)() : fallback)
  if (apiMode() === 'mock') return resolve()
  try {
    return await backendCall()
  } catch (err) {
    if (apiMode() === 'auto' && err instanceof BackendUnavailableError) return resolve()
    throw err
  }
}

function paginateMock<T extends { id: string }>(
  items: T[],
  opts: { limit?: number; cursor?: string },
): { items: T[]; nextCursor?: string } {
  const limit = opts.limit ?? 20
  const startIdx = opts.cursor ? items.findIndex((i) => i.id === opts.cursor) + 1 : 0
  const page = items.slice(startIdx, startIdx + limit)
  const nextCursor = page.length === limit ? page[page.length - 1]?.id : undefined
  return { items: page, nextCursor }
}


// ---------------------------------------------------------------------------
// Stats
// ---------------------------------------------------------------------------

const stats = {
  get: os.handler(async () => {
    return withFallback(
      () => backendGet<DashboardSummary>('/stats'),
      () => mocks.getDashboardSummary(),
    )
  }),
}

// ---------------------------------------------------------------------------
// Capabilities — which product sections the unified UI should render.
// ---------------------------------------------------------------------------

type Capability = {
  id: string
  name: string
  enabled: boolean
  status: 'enabled' | 'coming_soon' | 'disabled'
}

// meta exposes the API mode so the client can warn when it is showing demo
// (mock) data instead of real backend data.
const meta = {
  get: os.handler(async () => {
    const mode = apiMode()
    return {
      mode,
      usingMockData: mode === 'mock' || (mode === 'auto' && isBackendUnreachable()),
    }
  }),
}

const capabilities = {
  get: os.handler(async () => {
    return withFallback(
      () => backendGet<{ products: Capability[] }>('/capabilities'),
      () => ({
        products: [
          { id: 'ci', name: 'CI', enabled: true, status: 'enabled' as const },
          { id: 'workflows', name: 'Workflows', enabled: true, status: 'enabled' as const },
          { id: 'loadtest', name: 'Load Testing', enabled: false, status: 'coming_soon' as const },
        ],
      }),
    )
  }),
}

// ---------------------------------------------------------------------------
// Projects
// ---------------------------------------------------------------------------

const projects = {
  list: os
    .input(
      z.object({
        workspace: z.optional(z.array(z.string())),
        tags: z.optional(z.array(z.string())),
        needsGrouping: z.optional(z.boolean()),
        limit: z.optional(z.number()),
        cursor: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<Paginated<Project>>('/projects', {
          workspace: input.workspace, tags: input.tags,
          needsGrouping: input.needsGrouping ? 'true' : undefined,
          limit: input.limit, cursor: input.cursor,
        }),
        () => {
          let items = mocks.getProjects()
          if (input.workspace && input.workspace.length > 0) {
            const set = new Set(input.workspace)
            items = items.filter((p) => set.has(p.workspace))
          }
          if (input.tags && input.tags.length > 0) {
            // Array-overlap semantics, matching the backend's `p.tags && $tags`.
            const want = new Set(input.tags)
            items = items.filter((p) => (p.tags ?? []).some((t) => want.has(t)))
          }
          if (input.needsGrouping) {
            // Inferred workspace (not declared) or no tags — matches the backend.
            items = items.filter((p) => p.inferred || (p.tags ?? []).length === 0)
          }
          return paginateMock(items, input)
        },
      )
    }),

  get: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<Project>(`/projects/${input.id}`),
        () => {
          const p = mocks.getProjects().find((p) => p.id === input.id)
          if (!p) throw new Error('Project not found')
          return p
        },
      )
    }),

  setTags: os
    .input(z.object({ id: z.string(), tags: z.array(z.string()) }))
    .handler(async ({ input }) => {
      return safe(
        () => backendPut(`/projects/${input.id}/tags`, { tags: input.tags }),
        () => mocks.setProjectTags(input.id, input.tags),
      )
    }),

  pipelines: os
    .input(z.object({ projectId: z.string() }))
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<PipelineDefinition[]>(`/projects/${input.projectId}/pipelines`),
        () => mocks.getProjectPipelines(input.projectId),
      )
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
      return withFallback(
        () => backendGet<Paginated<PipelineRun>>('/runs', {
          projectId: input.projectId, status: input.status, branch: input.branch,
          limit: input.limit, cursor: input.cursor,
        }),
        () => {
          let items = mocks.getRuns()
          if (input.projectId) items = items.filter((r) => r.projectId === input.projectId)
          if (input.status) items = items.filter((r) => r.status === input.status)
          return paginateMock(items, input)
        },
      )
    }),

  get: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<PipelineRun>(`/runs/${input.id}`),
        () => {
          const r = mocks.getRuns().find((r) => r.id === input.id)
          if (!r) throw new Error('Run not found')
          return r
        },
      )
    }),

  steps: os
    .input(z.object({ runId: z.string() }))
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<{ steps: PipelineStep[]; dagWaves: string[][] }>(`/runs/${input.runId}/steps`),
        () => ({ steps: mocks.getSteps(input.runId), dagWaves: [] }),
      )
    }),

  stepLogs: os
    .input(z.object({ runId: z.string(), stepName: z.string() }))
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<{ lines: string }>(`/runs/${input.runId}/steps/${encodeURIComponent(input.stepName)}/logs`),
        () => {
          const logs = mocks.getStepLogs()
          return { lines: logs[input.stepName] || '' }
        },
      )
    }),

  // All started steps' logs, for the contiguous "All output" view.
  logs: os
    .input(z.object({ runId: z.string() }))
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<{ logs: Record<string, string> }>(`/runs/${input.runId}/logs`),
        () => {
          const all = mocks.getStepLogs()
          const logs: Record<string, string> = {}
          for (const s of mocks.getSteps(input.runId)) {
            if (s.startedAt) logs[s.name] = all[s.name] ?? ''
          }
          return { logs }
        },
      )
    }),

  trigger: os
    .input(z.object({ projectId: z.string(), branch: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return safe(
        () => backendPost('/runs', {
          projectId: input.projectId,
          branch: input.branch || 'main',
        }),
        () => mocks.triggerRun({ projectId: input.projectId, branch: input.branch }),
      )
    }),

  cancel: os
    .input(z.object({ runId: z.string() }))
    .handler(async ({ input }) => {
      return safe(
        () => backendPost(`/runs/${input.runId}/cancel`),
        () => ({ success: true, run: mocks.cancelRun(input.runId) }) as any,
      )
    }),

  retry: os
    .input(z.object({ runId: z.string() }))
    .handler(async ({ input }) => {
      return safe(
        () => backendPost(`/runs/${input.runId}/retry`),
        () => mocks.retryRun(input.runId),
      )
    }),
}

// ---------------------------------------------------------------------------
// Workflows — generic engine runs (no forge/repo). Mock-first: the backend has
// POST /workflows/runs (trigger) and GET /workflows/runs/:id (detail) but not
// yet a list endpoint, so `list` falls back to mocks until that's reconciled.
// ---------------------------------------------------------------------------

const workflows = {
  list: os
    .input(
      z.object({
        status: z.optional(RunStatus),
        limit: z.optional(z.number()),
        cursor: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<Paginated<WorkflowRun>>('/workflows/runs', {
          status: input.status, limit: input.limit, cursor: input.cursor,
        }),
        () => {
          let items = mocks.getWorkflowRuns()
          if (input.status) items = items.filter((r) => r.status === input.status)
          return paginateMock(items, input)
        },
      )
    }),

  get: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<WorkflowRunDetail>(`/workflows/runs/${input.id}`),
        () => {
          const r = mocks.getWorkflowRun(input.id)
          if (!r) throw new Error('Workflow run not found')
          return r
        },
      )
    }),

  // trigger posts a workflow definition (YAML) and starts a run. Backend
  // reconciliation: triggerRun reads a raw YAML body — the real wiring needs to
  // send `definition` as the body rather than a JSON envelope.
  trigger: os
    .input(z.object({ definition: z.string() }))
    .handler(async ({ input }) => {
      return safe(
        () => backendPost<{ runId: string; name: string; status: string }>(
          '/workflows/runs',
          { definition: input.definition },
        ),
        { runId: `wf-mock-${Date.now()}`, name: 'workflow', status: 'pending' },
      )
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
      return withFallback(
        () => backendGet<Paginated<Gate>>('/gates', {
          status: input.status, limit: input.limit, cursor: input.cursor,
        }),
        () => {
          let items = mocks.getGates()
          if (input.status) items = items.filter((g) => g.status === input.status)
          return { items, nextCursor: undefined }
        },
      )
    }),

  approve: os
    .input(z.object({ runId: z.string(), stepName: z.string(), comment: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return safe(
        () => backendPost(`/runs/${input.runId}/gates/${encodeURIComponent(input.stepName)}/approve`, {
          stepName: input.stepName, comment: input.comment,
        }),
        () => ({ success: true, gate: mocks.approveGate(input.runId, input.stepName) }) as any,
      )
    }),

  reject: os
    .input(z.object({ runId: z.string(), stepName: z.string(), reason: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return safe(
        () => backendPost(`/runs/${input.runId}/gates/${encodeURIComponent(input.stepName)}/reject`, {
          stepName: input.stepName, reason: input.reason,
        }),
        () => ({ success: true, gate: mocks.rejectGate(input.runId, input.stepName) }) as any,
      )
    }),
}

// ---------------------------------------------------------------------------
// Workspaces
// ---------------------------------------------------------------------------

const workspaces = {
  list: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<Paginated<Workspace>>('/workspaces', { limit: input.limit, cursor: input.cursor }),
        () => paginateMock(mocks.getWorkspaces(), input),
      )
    }),

  create: os
    .input(z.object({ name: z.string(), slug: z.string(), description: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return safe(() => backendPost('/workspaces', input), () => mocks.createWorkspace(input))
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return safe(() => backendDelete(`/workspaces/${input.id}`), () => mocks.deleteWorkspace(input.id))
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
        return withFallback(
          () => backendGet<Paginated<TagKey>>('/tags', { limit: input.limit, cursor: input.cursor }),
          () => paginateMock(mocks.getTagKeys(), input),
        )
      }),

    create: os
      .input(z.object({
        key: z.string(),
        label: z.string(),
        allowedValues: z.optional(z.array(z.string())),
        color: z.optional(z.string()),
      }))
      .handler(async ({ input }) => {
        return safe(() => backendPost('/tags', input), () => mocks.createTagKey(input))
      }),

    update: os
      .input(z.object({
        id: z.string(),
        label: z.string(),
        allowedValues: z.optional(z.array(z.string())),
        color: z.optional(z.string()),
      }))
      .handler(async ({ input }) => {
        return safe(() => backendPut(`/tags/${input.id}`, input), () => mocks.updateTagKey(input))
      }),

    delete: os
      .input(z.object({ id: z.string() }))
      .handler(async ({ input }) => {
        return safe(() => backendDelete(`/tags/${input.id}`), () => mocks.deleteTagKey(input.id))
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
      return withFallback(
        () => backendGet<Paginated<Environment>>('/environments', { limit: input.limit, cursor: input.cursor }),
        () => paginateMock(mocks.getEnvironments(), input),
      )
    }),

  create: os
    .input(z.object({ name: z.string(), slug: z.string() }))
    .handler(async ({ input }) => {
      return safe(() => backendPost('/environments', input), () => mocks.createEnvironment(input))
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return safe(() => backendDelete(`/environments/${input.id}`), () => mocks.deleteEnvironment(input.id))
    }),
}

// ---------------------------------------------------------------------------
// Environment variables
// ---------------------------------------------------------------------------

const envVariables = {
  list: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<Paginated<EnvVariable>>('/env-variables', { limit: input.limit, cursor: input.cursor }),
        () => paginateMock(mocks.getEnvVariables(), input),
      )
    }),

  values: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<Paginated<EnvVariableValue>>('/env-variables/values', { limit: input.limit, cursor: input.cursor }),
        () => {
          const all = mocks.getEnvVariableValues()
          const limit = input.limit ?? 20
          const page = all.slice(0, limit)
          return { items: page, nextCursor: undefined }
        },
      )
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
      return safe(() => backendPost('/env-variables', input), () => mocks.createEnvVariable(input))
    }),

  setValue: os
    .input(z.object({ variableId: z.string(), environmentId: z.optional(z.string()), value: z.string() }))
    .handler(async ({ input }) => {
      return safe(() => backendPut(`/env-variables/${input.variableId}/values`, input), () => mocks.setEnvVariableValue(input))
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return safe(() => backendDelete(`/env-variables/${input.id}`), () => mocks.deleteEnvVariable(input.id))
    }),
}

// ---------------------------------------------------------------------------
// Teams
// ---------------------------------------------------------------------------

const teams = {
  list: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<Paginated<Team>>('/teams', { limit: input.limit, cursor: input.cursor }),
        () => paginateMock(mocks.getTeams(), input),
      )
    }),

  get: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<TeamWithMembers>(`/teams/${input.id}`),
        () => mocks.getTeamWithMembers(input.id) || { id: input.id, name: 'Unknown', slug: 'unknown', source: 'internal' as const, memberCount: 0, members: [] },
      )
    }),

  create: os
    .input(z.object({ name: z.string(), slug: z.string() }))
    .handler(async ({ input }) => {
      return safe(() => backendPost('/teams', input), () => mocks.createTeam(input))
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return safe(() => backendDelete(`/teams/${input.id}`), () => mocks.deleteTeam(input.id))
    }),

  addMembers: os
    .input(z.object({ teamId: z.string(), userIds: z.array(z.string()) }))
    .handler(async ({ input }) => {
      return safe(
        () => backendPost(`/teams/${input.teamId}/members`, { userIds: input.userIds }),
        () => mocks.addTeamMembers(input.teamId, input.userIds),
      )
    }),

  removeMember: os
    .input(z.object({ teamId: z.string(), userId: z.string() }))
    .handler(async ({ input }) => {
      return safe(
        () => backendDelete(`/teams/${input.teamId}/members/${input.userId}`),
        () => mocks.removeTeamMember(input.teamId, input.userId),
      )
    }),
}

// ---------------------------------------------------------------------------
// Users
// ---------------------------------------------------------------------------

const users = {
  list: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<Paginated<User>>('/users', { limit: input.limit, cursor: input.cursor }),
        () => paginateMock(mocks.getUsers(), input),
      )
    }),
}

// ---------------------------------------------------------------------------
// Roles
// ---------------------------------------------------------------------------

const roles = {
  list: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<Paginated<Role>>('/roles', { limit: input.limit, cursor: input.cursor }),
        () => paginateMock(mocks.getRoles(), input),
      )
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
      return safe(() => backendPost('/roles', input), () => mocks.createRole(input))
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
      const { id, ...body } = input
      return safe(() => backendPatch(`/roles/${id}`, body), () => mocks.updateRole(id, body))
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return safe(() => backendDelete(`/roles/${input.id}`), () => mocks.deleteRole(input.id))
    }),

  assignments: {
    list: os
      .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
      .handler(async ({ input }) => {
        return withFallback(
          () => backendGet<Paginated<Assignment>>('/roles/assignments', { limit: input.limit, cursor: input.cursor }),
          () => {
            const items = mocks.getAssignments()
            const limit = input.limit ?? 20
            return { items: items.slice(0, limit), nextCursor: undefined }
          },
        )
      }),

    create: os
      .input(z.object({ subjects: z.array(z.string()), role: z.string() }))
      .handler(async ({ input }) => {
        return safe(() => backendPost('/roles/assignments', input), () => mocks.createAssignments(input))
      }),

    delete: os
      .input(z.object({ subject: z.string(), role: z.string() }))
      .handler(async ({ input }) => {
        return safe(() => backendDelete(`/roles/assignments/${encodeURIComponent(input.subject)}?role=${input.role}`), () => mocks.deleteAssignment(input.subject, input.role))
      }),
  },
}

// ---------------------------------------------------------------------------
// API keys
// ---------------------------------------------------------------------------

const apiKeys = {
  list: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<Paginated<ApiKey>>('/api-keys', { limit: input.limit, cursor: input.cursor }),
        () => paginateMock(mocks.getApiKeys(), input),
      )
    }),

  create: os
    .input(z.object({
      name: z.string(),
      role: z.string(),
      workspaces: z.array(z.string()),
      environments: z.array(z.string()),
      expiresAt: z.optional(z.string()),
    }))
    .handler(async ({ input }) => {
      return safe(() => backendPost('/api-keys', input), () => mocks.createApiKey(input))
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return safe(() => backendDelete(`/api-keys/${input.id}`), () => mocks.deleteApiKey(input.id))
    }),
}

// ---------------------------------------------------------------------------
// Personal tokens
// ---------------------------------------------------------------------------

const personalTokens = {
  list: os
    .input(z.object({ userId: z.string(), limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<Paginated<PersonalToken>>('/personal-tokens', { limit: input.limit, cursor: input.cursor }),
        () => paginateMock(mocks.getPersonalTokens(input.userId), input),
      )
    }),

  create: os
    .input(z.object({ userId: z.string(), name: z.string(), expiresAt: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return safe(() => backendPost('/personal-tokens', input), () => mocks.createPersonalToken(input))
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return safe(() => backendDelete(`/personal-tokens/${input.id}`), () => mocks.deletePersonalToken(input.id))
    }),
}

// ---------------------------------------------------------------------------
// Runners
// ---------------------------------------------------------------------------

const runners = {
  list: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<Paginated<RunnerPool>>('/runners', { limit: input.limit, cursor: input.cursor }),
        () => paginateMock(mocks.getRunnerPools(), input),
      )
    }),
}

// ---------------------------------------------------------------------------
// Forge connections
// ---------------------------------------------------------------------------

const forgeConnections = {
  list: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<Paginated<ForgeConnection>>('/forge-connections', { limit: input.limit, cursor: input.cursor }),
        () => paginateMock(mocks.getForgeConnections(), input),
      )
    }),
}

// ---------------------------------------------------------------------------
// Audit entries
// ---------------------------------------------------------------------------

const auditEntries = {
  list: os
    .input(z.object({ action: z.optional(z.string()), limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<Paginated<AuditEntry>>('/audit-entries', {
          action: input.action, limit: input.limit, cursor: input.cursor,
        }),
        () => {
          let items = mocks.getAuditEntries()
          if (input.action) items = items.filter((e) => e.action === input.action)
          return paginateMock(items, { limit: input.limit ?? 50, cursor: input.cursor })
        },
      )
    }),
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

const org = {
  get: os.handler(async () => {
    return withFallback(() => backendGet<Org>('/org'), () => mocks.getOrg())
  }),
  setPolicy: os
    .input(z.object({ requireProjectWorkspace: z.boolean() }))
    .handler(async ({ input }) => {
      return safe(() => backendPut('/org/policy', input), () => mocks.setOrgPolicy(input))
    }),
}

const auth = {
  me: os.handler(async () => {
    return withFallback(
      () => backendGet<AuthUser>('/auth/me'),
      () => mocks.getAuthUser(),
    )
  }),

  updateProfile: os
    .input(z.object({
      name: z.optional(z.string()),
      avatarUrl: z.optional(z.string()),
      themeMode: z.optional(z.enum(['light', 'dark', 'auto'])),
      colorTheme: z.optional(z.string()),
    }))
    .handler(async ({ input }) => {
      return safe(() => backendPut('/auth/profile', input), () => mocks.updateAuthProfile(input))
    }),

  changePassword: os
    .input(z.object({ currentPassword: z.string(), newPassword: z.string() }))
    .handler(async ({ input }) => {
      return safe(() => backendPost('/auth/change-password', input), () => mocks.changePassword())
    }),

  sessions: {
    list: os.handler(async () => {
      return withFallback(
        () => backendGet<{ items: Session[] }>('/auth/sessions').then((r) => r.items),
        () => mocks.getSessions(),
      )
    }),
    revoke: os
      .input(z.object({ id: z.string() }))
      .handler(async ({ input }) => {
        return safe(() => backendDelete(`/auth/sessions/${input.id}`), () => mocks.revokeSession(input.id))
      }),
  },

  mfa: {
    setup: os.handler(async () => {
      return safe(() => backendPost<MfaSetup>('/auth/mfa/setup'), () => mocks.mfaSetup())
    }),
    verifySetup: os
      .input(z.object({ code: z.string() }))
      .handler(async ({ input }) => {
        return safe(() => backendPost('/auth/mfa/setup/verify', input), () => mocks.mfaVerifySetup())
      }),
    disable: os
      .input(z.object({ code: z.string() }))
      .handler(async () => {
        return safe(() => backendDelete('/auth/mfa'), () => mocks.mfaDisable())
      }),
  },

  providers: {
    list: os.handler(async () => {
      return withFallback(
        () => backendGet<AuthProviders>('/auth/providers'),
        () => mocks.getAuthProviders(),
      )
    }),
    save: os
      .input(z.object({
        providerType: z.enum(['oidc', 'saml']),
        displayName: z.optional(z.string()),
        config: z.object({
          issuerUrl: z.optional(z.string()),
          clientId: z.optional(z.string()),
          clientSecret: z.optional(z.string()),
          metadataUrl: z.optional(z.string()),
          entityId: z.optional(z.string()),
        }),
      }))
      .handler(async ({ input }) => {
        return safe(() => backendPut('/auth/provider', input), () => mocks.saveAuthProvider(input))
      }),
    delete: os
      .input(z.object({ providerType: z.string() }))
      .handler(async ({ input }) => {
        return safe(() => backendDelete(`/auth/provider/${input.providerType}`), () => mocks.deleteAuthProvider(input.providerType))
      }),
  },
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

const search = {
  query: os
    .input(z.object({ q: z.string(), limit: z.optional(z.number()) }))
    .handler(async ({ input }) => {
      return withFallback(
        () => backendGet<{ projects: Project[]; runs: PipelineRun[] }>('/search', { q: input.q, limit: input.limit }),
        () => {
          const q = input.q.toLowerCase()
          const limit = input.limit ?? 10
          return {
            projects: mocks.getProjects().filter((p) => p.name.toLowerCase().includes(q)).slice(0, limit),
            runs: mocks.getRuns().filter((r) => r.commitMessage.toLowerCase().includes(q)).slice(0, limit),
          }
        },
      )
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
  forgeConnections,
  auditEntries,
  org,
  auth,
  search,
})

export type AppRouter = typeof appRouter
