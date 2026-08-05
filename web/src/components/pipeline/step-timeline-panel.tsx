import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Loader2, Server, Hourglass } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { EVENT_LABELS, EVENT_TONE } from '#/lib/run-events'
import { formatDateTime } from '#/lib/format-time'
import type { RunPlacement } from '#/lib/api/types'

// StepTimelinePanel — the dispatch lifecycle of one step, from the engine's
// durable event log plus the fleet placement record: queued → dispatched →
// started → finished, each with the delta from the previous transition, and
// which machine took the assignment at what price. This is the per-step
// answer to "where did the time go, and why this machine?".

function fmtDelta(ms: number): string {
  if (ms < 1000) return `+${ms}ms`
  if (ms < 60_000) return `+${(ms / 1000).toFixed(ms < 10_000 ? 1 : 0)}s`
  const m = Math.floor(ms / 60_000)
  return `+${m}m ${Math.round((ms % 60_000) / 1000)}s`
}

function fmtClock(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toTimeString().slice(0, 8)
}

export function StepTimelinePanel({ runId, stepName, live }: { runId: string; stepName: string; live: boolean }) {
  const { data: eventsData, isLoading } = useQuery({
    ...orpc.runs.events.queryOptions({ input: { runId } }),
    refetchInterval: live ? 4000 : false,
  })
  const { data: placementData } = useQuery({
    ...orpc.runs.placement.queryOptions({ input: { runId } }),
    refetchInterval: live ? 5000 : false,
  })

  const events = (eventsData?.events ?? []).filter((e) => e.stepName === stepName)
  const placements = (placementData?.placements ?? []).filter((p) => p.stepName === stepName)

  if (isLoading) {
    return (
      <div className="p-8 flex items-center justify-center text-muted-foreground text-sm">
        <Loader2 size={14} className="animate-spin mr-2" /> Loading timeline…
      </div>
    )
  }
  if (events.length === 0 && placements.length === 0) {
    return <div className="p-8 text-center text-muted-foreground text-sm">No events recorded for this step yet.</div>
  }

  return (
    <div className="p-4 sm:p-5 space-y-5">
      {events.length > 0 && (
        <ol className="relative">
          {events.map((e, i) => {
            const label = EVENT_LABELS[e.event] ?? e.event
            const tone = EVENT_TONE[e.event] ?? 'text-foreground'
            const prev = i > 0 ? Date.parse(events[i - 1]!.at) : NaN
            const cur = Date.parse(e.at)
            const delta = i > 0 && !Number.isNaN(prev) && !Number.isNaN(cur) ? Math.max(0, cur - prev) : null
            return (
              <li key={i} className="flex items-start gap-3 py-1.5">
                <span className="relative flex flex-col items-center self-stretch">
                  <span className={`mt-1.5 h-2 w-2 shrink-0 rounded-full bg-current ${tone} opacity-80`} />
                  {i < events.length - 1 && <span className="w-px flex-1 bg-border mt-1 -mb-2.5" />}
                </span>
                <div className="min-w-0 flex-1 pb-1">
                  <div className="flex flex-wrap items-baseline gap-x-2">
                    <span className={`text-sm font-medium ${tone}`}>{label}</span>
                    {typeof e.attempt === 'number' && e.attempt > 0 && (
                      <span className="text-[10px] text-muted-foreground">attempt {e.attempt + 1}</span>
                    )}
                    {e.actor !== 'engine' && <span className="text-xs text-muted-foreground">{e.actor}</span>}
                  </div>
                  {e.reason && <div className="mt-0.5 text-xs text-muted-foreground">{e.reason}</div>}
                </div>
                <span className="shrink-0 flex items-baseline gap-2 text-[11px] tabular-nums">
                  {delta != null && <span className="text-muted-foreground/70 font-mono">{fmtDelta(delta)}</span>}
                  <time className="text-muted-foreground" title={formatDateTime(cur)}>
                    {fmtClock(e.at)}
                  </time>
                </span>
              </li>
            )
          })}
        </ol>
      )}

      {placements.length > 0 && (
        <div className="space-y-2 pt-1 border-t border-border">
          <p className="text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground/60 pt-2">
            Placement
          </p>
          {placements.map((p) => (
            <PlacementRow key={`${p.stepName}-${p.attempt}`} placement={p} />
          ))}
        </div>
      )}
    </div>
  )
}

function PlacementRow({ placement: p }: { placement: RunPlacement }) {
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px]">
      <Server size={12} className="text-muted-foreground/60 shrink-0" />
      {p.machineId ? (
        <Link
          to="/fleet/$id"
          params={{ id: p.machineId }}
          className="font-mono text-foreground/90 hover:text-primary transition-colors"
        >
          {p.machineId.slice(0, 8)}
        </Link>
      ) : (
        <span className="text-muted-foreground">{p.status === 'pending' ? 'waiting for a machine…' : p.status}</span>
      )}
      {p.instanceType && <span className="font-mono text-muted-foreground">{p.instanceType}</span>}
      {p.capacityType && (
        <span
          className={`rounded-full border px-1.5 py-px text-[10px] font-medium ${
            p.capacityType === 'spot'
              ? 'border-purple-400/30 text-purple-400'
              : 'border-border text-muted-foreground'
          }`}
        >
          {p.capacityType}
        </span>
      )}
      {p.pricePerHourUsd != null && (
        <span className="font-mono tabular-nums text-muted-foreground">${p.pricePerHourUsd.toFixed(3)}/hr</span>
      )}
      {p.queueWaitMs != null && (
        <span className="flex items-center gap-1 text-muted-foreground" title="Time from queued to assigned">
          <Hourglass size={11} />
          <span className="font-mono tabular-nums">{fmtDelta(p.queueWaitMs)} queued</span>
        </span>
      )}
      {p.attempt > 0 && <span className="text-[10px] text-muted-foreground">attempt {p.attempt + 1}</span>}
    </div>
  )
}
