import { useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { orpc } from '#/lib/orpc'
import { getAccessToken } from '#/lib/auth-token'
import type { StepLogLine } from '#/lib/api/types'

/**
 * Streams a running step's log lines over SSE (via the /api/sse proxy →
 * GET /runs/:id/steps/:step/logs/stream) into the runs.stepLogs query cache,
 * so the log view fills in live without polling. The server sends the full
 * history as the first `log` event and increments after; on `done` the query
 * is invalidated once for the authoritative final read. Returns whether the
 * stream is connected; the caller uses that to suspend its polling fallback.
 */
export function useStepLogStream(runId: string, stepName: string | null): boolean {
  const qc = useQueryClient()
  const [connected, setConnected] = useState(false)

  // The first `log` frame replaces the cache (it carries full history);
  // later frames append.
  const gotHistory = useRef(false)

  useEffect(() => {
    if (!stepName || typeof window === 'undefined' || typeof EventSource === 'undefined') return
    gotHistory.current = false

    const token = getAccessToken()
    const qs = token ? `?access_token=${encodeURIComponent(token)}` : ''
    const es = new EventSource(
      `/api/sse/runs/${encodeURIComponent(runId)}/steps/${encodeURIComponent(stepName)}/logs/stream${qs}`,
    )

    const logsKey = orpc.runs.stepLogs.queryOptions({ input: { runId, stepName } }).queryKey

    es.onopen = () => setConnected(true)
    es.addEventListener('log', (ev) => {
      try {
        const lines = JSON.parse((ev as MessageEvent).data) as StepLogLine[]
        if (!Array.isArray(lines)) return
        const replace = !gotHistory.current
        gotHistory.current = true
        qc.setQueryData(logsKey, (prev: { lines: StepLogLine[]; complete: boolean } | undefined) => ({
          lines: replace || !prev ? lines : [...prev.lines, ...lines],
          complete: false,
        }))
      } catch {
        /* ignore malformed frame */
      }
    })
    es.addEventListener('done', () => {
      setConnected(false)
      es.close()
      void qc.invalidateQueries({ queryKey: logsKey })
    })
    es.onerror = () => {
      // The browser auto-reconnects; the server replays history on reconnect,
      // so the next `log` frame must replace rather than append.
      setConnected(false)
      gotHistory.current = false
    }

    return () => {
      es.close()
      setConnected(false)
    }
  }, [runId, stepName, qc])

  return connected
}
