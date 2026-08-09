import { os } from '@orpc/server'
import { z } from 'zod'
import { RunStatus, type PipelineRun, type PipelineStep, type RunEvent, type PipelineDefinition, type Project, type Gate, type RunPlacement, type WorkflowRun, type WorkflowRunDetail, type StepLogLine, type RunAnnotation } from './types'
import { backendGet, backendPost, backendPut, backendPatch, backendDelete } from './backend'
import type { Paginated } from './router-shared'
import { demoAnnotations, isDemoMode } from './router-shared'

export const projects = {
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

export const runs = {
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

export const workflows = {
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

export const gates = {
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
