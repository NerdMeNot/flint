import { os } from '@orpc/server'
import { z } from 'zod'
import {
  RunStatus,
  type PipelineRun,
  type PipelineStep,
  type RunEvent,
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
  type Machine,
  type MachineEvent,
  type FleetDecision,
  type ComputeProvider,
  type RunPlacement,
  PolicyOverrideSchema,
  type ForgeConnection,
  type AuditEntry,
  type AuthUser,
  type Org,
  type Session,
  type MfaSetup,
  type AuthProviders,
  type ProviderTestResult,
  type TestLoginStart,
  type TestLoginResult,
  type GroupMappings,
  type ScimStatus,
  type SignInLog,
  providerConfigSchema,
  type WorkflowRun,
  type WorkflowRunDetail,
  type SavedView,
  type StepLogLine,
  type RunAnnotation,
} from './types'
import {
  backendGet,
  backendPost,
  backendPut,
  backendPatch,
  backendDelete,
  backendGetRoot,
  backendPostRoot,
  backendPutRoot,
  backendDeleteRoot,
} from './backend'

// ---------------------------------------------------------------------------
// Shared types for backend responses
// ---------------------------------------------------------------------------

type Paginated<T> = { items: T[]; nextCursor?: string }

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

type Capability = {
  id: string
  name: string
  enabled: boolean
  status: 'enabled' | 'coming_soon' | 'disabled'
}

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
let cachedMode: 'demo' | 'live' | null = null
async function isDemoMode(): Promise<boolean> {
  if (cachedMode === null) {
    try {
      cachedMode = (await backendGet<{ mode: 'demo' | 'live' }>('/meta')).mode
    } catch {
      return false
    }
  }
  return cachedMode === 'demo'
}

// Representative annotations for demo mode — what a test-summary step and a
// coverage step would publish. The tone follows the run's actual outcome so
// the demo page reads coherently (no "tests failed" panel on a green run).
async function demoAnnotations(runId: string): Promise<RunAnnotation[]> {
  let failing = false
  try {
    failing = (await backendGet<{ status?: string }>(`/runs/${runId}`)).status === 'failed'
  } catch {
    /* run lookup failed → default to the passing variant */
  }
  const now = new Date().toISOString()
  const annotations: RunAnnotation[] = [
    {
      id: `${runId}-tests`,
      style: failing ? 'error' : 'success',
      context: 'test-summary',
      body: failing
        ? '**3 tests failed** across 2 suites\n\n| Suite | Failed | Total |\n| --- | --- | --- |\n| `engine` | 2 | 412 |\n| `fleet` | 1 | 188 |\n\nFirst failure: `TestAdvanceRetriesLostAgent` — `expected requeue, got terminal failure`'
        : '**All 600 tests passed** across 14 suites in 2m 41s\n\nSlowest: `TestSchedulerBinPacksWarmMachines` (8.2s)',
      createdAt: now,
    },
    {
      id: `${runId}-coverage`,
      style: 'info',
      context: 'coverage',
      body: 'Coverage: **81.4%** (+0.3% vs main) · [full report](#)',
      createdAt: now,
    },
  ]
  return annotations
}

const capabilities = {
  get: os.handler(async () => {
    return backendGet<{ products: Capability[] }>('/capabilities')
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
        attention: z.optional(z.boolean()),
        limit: z.optional(z.number()),
        cursor: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      return backendGet<Paginated<Project>>('/projects', {
          workspace: input.workspace, tags: input.tags,
          needsGrouping: input.needsGrouping ? 'true' : undefined,
          attention: input.attention ? 'true' : undefined,
          limit: input.limit, cursor: input.cursor,
        })
    }),

  get: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return backendGet<Project>(`/projects/${input.id}`)
    }),

  // Register a project (the canonical create path; the Project CRD is an optional
  // adapter onto the same row). repo + forgeRef required.
  create: os
    .input(z.object({
      repo: z.string(),
      forgeRef: z.string(),
      displayName: z.optional(z.string()),
      description: z.optional(z.string()),
      colour: z.optional(z.string()),
      workspace: z.optional(z.string()),
      defaultBranch: z.optional(z.string()),
    }))
    .handler(async ({ input }) => {
      return backendPost<{ id: string; repo: string; webhookProvisioned: boolean }>('/projects', input)
    }),

  update: os
    .input(z.object({
      id: z.string(),
      displayName: z.optional(z.string()),
      description: z.optional(z.string()),
      colour: z.optional(z.string()),
      workspace: z.optional(z.string()),
      defaultBranch: z.optional(z.string()),
    }))
    .handler(async ({ input }) => {
      const { id, ...body } = input
      return backendPatch(`/projects/${id}`, body)
    }),

  archive: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return backendDelete(`/projects/${input.id}`)
    }),

  restore: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return backendPost(`/projects/${input.id}/restore`)
    }),

  // Archived projects (admin) — a flat list for restore.
  archived: os.handler(async () => {
    return backendGet<{ items: Array<{ id: string; name: string; repo: string; workspace: string; colour: string; createdAt: string }> }>(
      '/projects', { archived: 'true' },
    )
  }),

  setTags: os
    .input(z.object({ id: z.string(), tags: z.array(z.string()) }))
    .handler(async ({ input }) => {
      return backendPut(`/projects/${input.id}/tags`, { tags: input.tags })
    }),

  pipelines: os
    .input(z.object({ projectId: z.string() }))
    .handler(async ({ input }) => {
      return backendGet<PipelineDefinition[]>(`/projects/${input.projectId}/pipelines`)
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
        from: z.optional(z.number()),
        to: z.optional(z.number()),
        limit: z.optional(z.number()),
        cursor: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      return backendGet<Paginated<PipelineRun>>('/runs', {
          projectId: input.projectId, status: input.status, branch: input.branch,
          from: input.from, to: input.to,
          limit: input.limit, cursor: input.cursor,
        })
    }),

  get: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return backendGet<PipelineRun>(`/runs/${input.id}`)
    }),

  steps: os
    .input(z.object({ runId: z.string() }))
    .handler(async ({ input }) => {
      return backendGet<{ steps: PipelineStep[]; dagWaves: string[][] }>(`/runs/${input.runId}/steps`)
    }),

  stepLogs: os
    .input(z.object({ runId: z.string(), stepName: z.string() }))
    .handler(async ({ input }) => {
      // Structured lines pass through as-is: the log view needs timestamps
      // (group durations, timestamp gutter) and the stream tag (stderr tint).
      const res = await backendGet<{ lines: StepLogLine[] | null; complete?: boolean }>(
        `/runs/${input.runId}/steps/${encodeURIComponent(input.stepName)}/logs`,
      )
      return { lines: Array.isArray(res.lines) ? res.lines : [], complete: res.complete ?? true }
    }),

  // Annotations — markdown panels steps publish onto the run page. Mock-first:
  // the backend endpoint doesn't exist yet, so demo deployments fall back to
  // representative samples (live deployments fall back to none) until it's
  // reconciled (GET /runs/:id/annotations + an agent `annotate` emit).
  annotations: os
    .input(z.object({ runId: z.string() }))
    .handler(async ({ input }) => {
      try {
        const res = await backendGet<{ annotations: RunAnnotation[] }>(`/runs/${input.runId}/annotations`)
        return { annotations: res.annotations ?? [] }
      } catch {
        return { annotations: (await isDemoMode()) ? await demoAnnotations(input.runId) : [] }
      }
    }),

  // All started steps' logs, for the contiguous "All output" view.
  logs: os
    .input(z.object({ runId: z.string() }))
    .handler(async ({ input }) => {
      return backendGet<{ logs: Record<string, string> }>(`/runs/${input.runId}/logs`)
    }),

  // Cost-per-run: requested compute × wall-clock, per step, with $ estimate.
  cost: os
    .input(z.object({ runId: z.string() }))
    .handler(async ({ input }) => {
      return backendGet<{
        steps: Array<{
          name: string
          durationSecs: number
          cpuCores: number
          memoryGb: number
          coreSecs: number
          gbSecs: number
          estimatedUsd: number
        }>
        totalCoreSecs: number
        totalGbSecs: number
        estimatedUsd: number
        rates: { cpuCoreHourUsd: number; memoryGbHrUsd: number }
      }>(`/runs/${input.runId}/cost`)
    }),

  // Placement transparency: which machine ran each step, at what price, and
  // how long it waited — the per-run half of the fleet decision ledger.
  placement: os
    .input(z.object({ runId: z.string() }))
    .handler(async ({ input }) => {
      return backendGet<{ placements: RunPlacement[] }>(`/runs/${input.runId}/placement`)
    }),

  trigger: os
    .input(z.object({ projectId: z.string(), branch: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendPost('/runs', {
          projectId: input.projectId,
          branch: input.branch || 'main',
        })
    }),

  cancel: os
    .input(z.object({ runId: z.string() }))
    .handler(async ({ input }) => {
      return backendPost(`/runs/${input.runId}/cancel`)
    }),

  retry: os
    .input(z.object({ runId: z.string() }))
    .handler(async ({ input }) => {
      return backendPost(`/runs/${input.runId}/retry`)
    }),

  pause: os
    .input(z.object({ runId: z.string() }))
    .handler(async ({ input }) => {
      return backendPost(`/runs/${input.runId}/pause`)
    }),

  resume: os
    .input(z.object({ runId: z.string() }))
    .handler(async ({ input }) => {
      return backendPost(`/runs/${input.runId}/resume`)
    }),

  rerunFailed: os
    .input(z.object({ runId: z.string() }))
    .handler(async ({ input }) => {
      return backendPost(`/runs/${input.runId}/rerun-failed`)
    }),

  retryFromStep: os
    .input(z.object({ runId: z.string(), stepName: z.string() }))
    .handler(async ({ input }) => {
      return backendPost(`/runs/${input.runId}/retry-from/${encodeURIComponent(input.stepName)}`)
    }),

  resolveStep: os
    .input(
      z.object({
        runId: z.string(),
        stepName: z.string(),
        outcome: z.enum(['succeeded', 'failed', 'skipped']),
        reason: z.optional(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      return backendPost(`/runs/${input.runId}/steps/${encodeURIComponent(input.stepName)}/resolve`, {
          outcome: input.outcome,
          reason: input.reason ?? '',
        })
    }),

  events: os
    .input(z.object({ runId: z.string() }))
    .handler(async ({ input }) => {
      return backendGet<{ runId: string; events: RunEvent[] }>(`/runs/${input.runId}/events`)
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
      return backendGet<Paginated<WorkflowRun>>('/workflows/runs', {
          status: input.status, limit: input.limit, cursor: input.cursor,
        })
    }),

  get: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return backendGet<WorkflowRunDetail>(`/workflows/runs/${input.id}`)
    }),

  // trigger posts a workflow definition (YAML) and starts a run. Backend
  // reconciliation: triggerRun reads a raw YAML body — the real wiring needs to
  // send `definition` as the body rather than a JSON envelope.
  trigger: os
    .input(z.object({ definition: z.string() }))
    .handler(async ({ input }) => {
      return backendPost<{ runId: string; name: string; status: string }>(
          '/workflows/runs',
          { definition: input.definition },
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
      return backendGet<Paginated<Gate>>('/gates', {
          status: input.status, limit: input.limit, cursor: input.cursor,
        })
    }),

  approve: os
    .input(z.object({ runId: z.string(), stepName: z.string(), comment: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendPost(`/runs/${input.runId}/gates/${encodeURIComponent(input.stepName)}/approve`, {
          stepName: input.stepName, comment: input.comment,
        })
    }),

  reject: os
    .input(z.object({ runId: z.string(), stepName: z.string(), reason: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendPost(`/runs/${input.runId}/gates/${encodeURIComponent(input.stepName)}/reject`, {
          stepName: input.stepName, reason: input.reason,
        })
    }),
}

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

const teams = {
  list: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendGet<Paginated<Team>>('/teams', { limit: input.limit, cursor: input.cursor })
    }),

  get: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return backendGet<TeamWithMembers>(`/teams/${input.id}`)
    }),

  create: os
    .input(z.object({ name: z.string(), slug: z.string() }))
    .handler(async ({ input }) => {
      return backendPost('/teams', input)
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return backendDelete(`/teams/${input.id}`)
    }),

  addMembers: os
    .input(z.object({ teamId: z.string(), userIds: z.array(z.string()) }))
    .handler(async ({ input }) => {
      return backendPost(`/teams/${input.teamId}/members`, { userIds: input.userIds })
    }),

  removeMember: os
    .input(z.object({ teamId: z.string(), userId: z.string() }))
    .handler(async ({ input }) => {
      return backendDelete(`/teams/${input.teamId}/members/${input.userId}`)
    }),
}

// ---------------------------------------------------------------------------
// Users
// ---------------------------------------------------------------------------

const users = {
  list: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendGet<Paginated<User>>('/users', { limit: input.limit, cursor: input.cursor })
    }),

  // Provision a local (email + password) user and assign a role. Returns the
  // generated password when one wasn't supplied (shown once).
  create: os
    .input(z.object({
      email: z.string(),
      name: z.optional(z.string()),
      role: z.optional(z.string()),
      password: z.optional(z.string()),
    }))
    .handler(async ({ input }) => {
      return backendPost<{ id: string; email: string; role: string; generatedPassword?: string }>(
        '/users',
        input,
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
      return backendGet<Paginated<Role>>('/roles', { limit: input.limit, cursor: input.cursor })
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
      return backendPost('/roles', input)
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
      return backendPatch(`/roles/${id}`, body)
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return backendDelete(`/roles/${input.id}`)
    }),

  assignments: {
    list: os
      .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
      .handler(async ({ input }) => {
        return backendGet<Paginated<Assignment>>('/roles/assignments', { limit: input.limit, cursor: input.cursor })
      }),

    create: os
      .input(z.object({ subjects: z.array(z.string()), role: z.string() }))
      .handler(async ({ input }) => {
        return backendPost('/roles/assignments', input)
      }),

    delete: os
      .input(z.object({ subject: z.string(), role: z.string() }))
      .handler(async ({ input }) => {
        return backendDelete(`/roles/assignments/${encodeURIComponent(input.subject)}?role=${input.role}`)
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
      return backendGet<Paginated<ApiKey>>('/api-keys', { limit: input.limit, cursor: input.cursor })
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
      return backendPost('/api-keys', input)
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return backendDelete(`/api-keys/${input.id}`)
    }),
}

// ---------------------------------------------------------------------------
// Personal tokens
// ---------------------------------------------------------------------------

const personalTokens = {
  list: os
    .input(z.object({ userId: z.string(), limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendGet<Paginated<PersonalToken>>('/personal-tokens', { limit: input.limit, cursor: input.cursor })
    }),

  create: os
    .input(z.object({ userId: z.string(), name: z.string(), expiresAt: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendPost('/personal-tokens', input)
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return backendDelete(`/personal-tokens/${input.id}`)
    }),
}

// ---------------------------------------------------------------------------
// Runners
// ---------------------------------------------------------------------------

const runnerInput = z.object({
  name: z.string(),
  description: z.optional(z.string()),
  // compute_providers.name the pool draws machines from; default "static" (BYO).
  provider: z.optional(z.string()),
  // Optional pool defaults — blank means the pool stamps no request, so each job
  // sizes itself.
  cpu: z.optional(z.string()),
  memory: z.optional(z.string()),
  disk: z.optional(z.string()),
  arch: z.optional(z.string()),
  gpu: z.optional(z.object({ vendor: z.string(), model: z.optional(z.string()), count: z.optional(z.number()) })),
  // Provider allow-lists narrowing what Quote may offer (elastic pools).
  instanceTypes: z.optional(z.array(z.string())),
  regions: z.optional(z.array(z.string())),
  // Economics policy.
  capacityType: z.optional(z.enum(['spot', 'on_demand', 'any'])),
  objective: z.optional(z.enum(['cost', 'latency', 'balanced'])),
  minWarm: z.optional(z.number()),
  maxMachines: z.optional(z.number()),
  idleTtlSeconds: z.optional(z.number()),
  overrides: z.optional(z.array(PolicyOverrideSchema)),
  hourlyCost: z.optional(z.number()),
})

const runners = {
  list: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendGet<Paginated<RunnerPool>>('/runners', { limit: input.limit, cursor: input.cursor })
    }),

  create: os.input(runnerInput).handler(async ({ input }) => {
    return backendPost<{ name: string }>('/runners', input)
  }),

  update: os.input(runnerInput).handler(async ({ input }) => {
    const { name, ...body } = input
    return backendPatch(`/runners/${name}`, body)
  }),

  delete: os.input(z.object({ name: z.string() })).handler(async ({ input }) => {
    return backendDelete(`/runners/${input.name}`)
  }),

  setDefault: os.input(z.object({ name: z.string() })).handler(async ({ input }) => {
    return backendPost(`/runners/${input.name}/default`, {})
  }),

  // Mint (or rotate) the pool's agent join token. Plaintext returned exactly once.
  mintToken: os.input(z.object({ name: z.string() })).handler(async ({ input }) => {
    return backendPost<{ pool: string; token: string; note: string }>(`/runners/${input.name}/token`, {})
  }),

  // 7-day observed economics + the minWarm=1 what-if, computed from the
  // decision ledger and machine lifecycles.
  insights: os.input(z.object({ name: z.string() })).handler(async ({ input }) => {
    return backendGet<PoolInsights>(`/runners/${input.name}/insights`)
  }),
}

export type PoolInsights = {
  pool: string
  windowDays: number
  spendUsd: number
  machineHours: number
  boots: number
  bootP50Secs: number
  priceP50Usd: number
  interruptions: number
  assignments: {
    total: number
    warmHits: number
    warmHitRate: number
    queueP50Secs: number
    queueP95Secs: number
    warmQueueP50Secs: number
    coldQueueP50Secs: number
  }
  whatIf?: {
    minWarmOne: {
      costPerMonthUsd: number
      coldWaitP50Secs: number
      warmWaitP50Secs: number
    }
  }
}

// ---------------------------------------------------------------------------
// Fleet — machines, decisions, placement
// ---------------------------------------------------------------------------

const machines = {
  list: os
    .input(z.object({
      pool: z.optional(z.string()),
      status: z.optional(z.string()),
      limit: z.optional(z.number()),
      cursor: z.optional(z.string()),
    }))
    .handler(async ({ input }) => {
      return backendGet<Paginated<Machine>>('/machines', {
        pool: input.pool, status: input.status, limit: input.limit, cursor: input.cursor,
      })
    }),

  get: os.input(z.object({ id: z.string() })).handler(async ({ input }) => {
    return backendGet<{ machine: Machine; events: MachineEvent[] }>(`/machines/${input.id}`)
  }),

  drain: os.input(z.object({ id: z.string() })).handler(async ({ input }) => {
    return backendPost<{ id: string; status: string }>(`/machines/${input.id}/drain`, {})
  }),
}

const decisions = {
  list: os
    .input(z.object({
      pool: z.optional(z.string()),
      type: z.optional(z.string()),
      limit: z.optional(z.number()),
      cursor: z.optional(z.string()),
    }))
    .handler(async ({ input }) => {
      return backendGet<Paginated<FleetDecision>>('/decisions', {
        pool: input.pool, type: input.type, limit: input.limit, cursor: input.cursor,
      })
    }),
}

// ---------------------------------------------------------------------------
// Compute providers (the machine sources pools draw from)
// ---------------------------------------------------------------------------

const providerInput = z.object({
  name: z.string(),
  type: z.string(),
  config: z.optional(z.record(z.string(), z.unknown())),
  credentials: z.optional(z.record(z.string(), z.unknown())),
})

const computeProviders = {
  list: os.handler(async () => {
    return backendGet<{ providers: ComputeProvider[] }>('/providers')
  }),

  create: os.input(providerInput).handler(async ({ input }) => {
    return backendPost<{ id: string; name: string }>('/providers', input)
  }),

  update: os.input(providerInput).handler(async ({ input }) => {
    const { name, ...body } = input
    return backendPatch(`/providers/${name}`, body)
  }),

  delete: os.input(z.object({ name: z.string() })).handler(async ({ input }) => {
    return backendDelete(`/providers/${input.name}`)
  }),

  // Dry-run a Quote against the stored provider to verify config/credentials.
  test: os.input(z.object({ name: z.string() })).handler(async ({ input }) => {
    return backendPost<{ ok: boolean; error?: string; elastic?: boolean; offers?: unknown[] }>(
      `/providers/${input.name}/test`, {},
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
      return backendGet<Paginated<ForgeConnection>>('/forge-connections', { limit: input.limit, cursor: input.cursor })
    }),
}

// ---------------------------------------------------------------------------
// Audit entries
// ---------------------------------------------------------------------------

const auditEntries = {
  list: os
    .input(z.object({ action: z.optional(z.string()), limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendGet<Paginated<AuditEntry>>('/audit-entries', {
          action: input.action, limit: input.limit, cursor: input.cursor,
        })
    }),
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

const org = {
  get: os.handler(async () => {
    return backendGet<Org>('/org')
  }),
  setPolicy: os
    .input(z.object({ requireProjectWorkspace: z.boolean() }))
    .handler(async ({ input }) => {
      return backendPut('/org/policy', input)
    }),
}

const auth = {
  // The session/auth endpoints live at the server root (not /api/v1), so they use
  // the *Root backend helpers.
  me: os.handler(async () => {
    return backendGetRoot<AuthUser>('/auth/me')
  }),

  updateProfile: os
    .input(z.object({
      name: z.optional(z.string()),
      avatarUrl: z.optional(z.string()),
      themeMode: z.optional(z.enum(['light', 'dark', 'auto'])),
      colorTheme: z.optional(z.string()),
    }))
    .handler(async ({ input }) => {
      return backendPutRoot('/auth/profile', input)
    }),

  changePassword: os
    .input(z.object({ currentPassword: z.string(), newPassword: z.string() }))
    .handler(async ({ input }) => {
      return backendPostRoot('/auth/change-password', input)
    }),

  sessions: {
    list: os.handler(async () => {
      return backendGetRoot<{ items: Session[] }>('/auth/sessions').then((r) => r.items)
    }),
    revoke: os
      .input(z.object({ id: z.string() }))
      .handler(async ({ input }) => {
        return backendDeleteRoot(`/auth/sessions/${input.id}`)
      }),
  },

  mfa: {
    setup: os.handler(async () => {
      return backendPostRoot<MfaSetup>('/auth/mfa/setup')
    }),
    verifySetup: os
      .input(z.object({ code: z.string() }))
      .handler(async ({ input }) => {
        return backendPostRoot('/auth/mfa/setup/verify', input)
      }),
    disable: os
      .input(z.object({ code: z.string() }))
      .handler(async () => {
        return backendDeleteRoot('/auth/mfa')
      }),
    regenerateRecoveryCodes: os
      .input(z.object({ code: z.optional(z.string()), recoveryCode: z.optional(z.string()) }))
      .handler(async ({ input }) => {
        return backendPostRoot<{ recoveryCodes: string[] }>('/auth/mfa/recovery-codes', input)
      }),
  },

  providers: {
    list: os.handler(async () => {
      return backendGet<AuthProviders>('/auth/providers')
    }),
    setRequireSso: os
      .input(z.object({ enabled: z.boolean() }))
      .handler(async ({ input }) => {
        return backendPut<{ requireSso: boolean }>('/auth/require-sso', input)
      }),
    save: os
      .input(z.object({
        providerType: z.enum(['oidc', 'saml']),
        displayName: z.optional(z.string()),
        config: providerConfigSchema,
      }))
      .handler(async ({ input }) => {
        return backendPut('/auth/provider', input)
      }),
    test: os
      .input(z.object({
        providerType: z.enum(['oidc', 'saml']),
        config: providerConfigSchema,
      }))
      .handler(async ({ input }) => {
        return backendPost<ProviderTestResult>('/auth/provider/test', input)
      }),
    testLoginStart: os
      .input(z.object({
        providerType: z.enum(['oidc', 'saml']),
        config: providerConfigSchema,
      }))
      .handler(async ({ input }) => {
        return backendPost<TestLoginStart>('/auth/provider/test-login', input)
      }),
    testLoginResult: os
      .input(z.object({ id: z.string() }))
      .handler(async ({ input }) => {
        return backendGet<TestLoginResult>(`/auth/provider/test-login/${input.id}`)
      }),
    delete: os
      .input(z.object({ providerType: z.string() }))
      .handler(async ({ input }) => {
        return backendDelete(`/auth/provider/${input.providerType}`)
      }),
    groupMappings: {
      get: os.handler(async () => {
        return backendGet<GroupMappings>('/auth/group-mappings')
      }),
      save: os
        .input(z.object({
          strict: z.boolean(),
          mappings: z.array(z.object({ groupName: z.string(), roleId: z.string() })),
        }))
        .handler(async ({ input }) => {
          return backendPut('/auth/group-mappings', input)
        }),
    },
    signInLog: os.handler(async () => {
      return backendGet<SignInLog>('/auth/sign-in-log')
    }),
    scim: {
      get: os.handler(async () => {
        return backendGet<ScimStatus>('/auth/scim')
      }),
      generate: os.handler(async () => {
        return backendPost<{ token: string; baseUrl: string }>('/auth/scim/token')
      }),
      revoke: os.handler(async () => {
        return backendDelete('/auth/scim/token')
      }),
    },
  },
}

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
