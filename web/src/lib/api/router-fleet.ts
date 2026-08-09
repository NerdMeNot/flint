import { os } from '@orpc/server'
import { z } from 'zod'
import { type RunnerPool, type Machine, type MachineEvent, type FleetDecision, type ComputeProvider, PolicyOverrideSchema } from './types'
import { backendGet, backendPost, backendPatch, backendDelete } from './backend'
import type { Paginated, PoolInsights } from './router-shared'

export const runnerInput = z.object({
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

export const runners = {
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

export const machines = {
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

export const decisions = {
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

export const providerInput = z.object({
  name: z.string(),
  type: z.string(),
  config: z.optional(z.record(z.string(), z.unknown())),
  credentials: z.optional(z.record(z.string(), z.unknown())),
})

export const computeProviders = {
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
