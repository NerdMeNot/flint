import { type RunAnnotation } from './types'
import { backendGet } from './backend'

// Cached for the page's lifetime: /meta is immutable per deployment.
let cachedMode: 'demo' | 'live' | null = null

export async function isDemoMode(): Promise<boolean> {
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
export async function demoAnnotations(runId: string): Promise<RunAnnotation[]> {
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

export type Paginated<T> = { items: T[]; nextCursor?: string }

export type Capability = {
  id: string
  name: string
  enabled: boolean
  status: 'enabled' | 'coming_soon' | 'disabled'
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
