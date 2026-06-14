import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { orpc } from '#/lib/orpc'
import { getAccessToken } from '#/lib/auth-token'
import type { PipelineRun, PipelineStep } from '#/lib/api/types'

interface RunStateEvent {
  status: PipelineRun['status']
  steps: PipelineStep[]
  dagWaves: string[][]
}

/**
 * Subscribes to a run's live state over SSE (via the /api/sse streaming proxy)
 * and writes each snapshot into the React Query cache for runs.get (status) and
 * runs.steps (steps + dagWaves) — so the UI updates without polling. Returns
 * whether the stream is currently connected; the caller uses that to suspend its
 * polling fallback. Reconnection is handled by the browser's EventSource.
 */
export function useRunStream(runId: string, enabled: boolean): boolean {
  const qc = useQueryClient()
  const [connected, setConnected] = useState(false)

  useEffect(() => {
    if (!enabled || typeof window === 'undefined' || typeof EventSource === 'undefined') return

    const token = getAccessToken()
    const qs = token ? `?access_token=${encodeURIComponent(token)}` : ''
    const es = new EventSource(`/api/sse/runs/${encodeURIComponent(runId)}/stream${qs}`)

    const stepsKey = orpc.runs.steps.queryOptions({ input: { runId } }).queryKey
    const getKey = orpc.runs.get.queryOptions({ input: { id: runId } }).queryKey

    es.onopen = () => setConnected(true)
    es.addEventListener('state', (ev) => {
      try {
        const data = JSON.parse((ev as MessageEvent).data) as RunStateEvent
        qc.setQueryData(stepsKey, { steps: data.steps, dagWaves: data.dagWaves })
        qc.setQueryData(getKey, (prev) => (prev ? { ...prev, status: data.status } : prev))
      } catch {
        /* ignore malformed frame */
      }
    })
    es.addEventListener('done', () => {
      setConnected(false)
      es.close()
    })
    es.onerror = () => setConnected(false) // browser auto-reconnects

    return () => {
      es.close()
      setConnected(false)
    }
  }, [runId, enabled, qc])

  return connected
}
