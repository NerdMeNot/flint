import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { Server, Wind, Gauge } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { BackLink } from '#/components/BackLink'
import { PanelHeader } from '#/components/pipeline/run-gantt'
import { MachineStatusBadge, fmtCpuMem, fmtAgo, fmtDuration } from '#/components/fleet/machine-bits'
import type { MachineEvent } from '#/lib/api/types'

export const Route = createFileRoute('/fleet/$id')({
  component: MachineDetailPage,
})

function MachineDetailPage() {
  const { id } = Route.useParams()
  const { data } = useSuspenseQuery({
    ...orpc.machines.get.queryOptions({ input: { id } }),
    refetchInterval: 5000,
  })
  const m = data.machine
  const events = data.events

  const drain = useAction(() => client.machines.drain({ id }), {
    invalidate: [orpc.machines.get.key({ input: { id } }), orpc.machines.list.key()],
  })
  const canDrain = m.status === 'idle' || m.status === 'busy'

  // Lifetime economics: what this machine cost and what it did with it.
  const lifeHours = hoursBetween(m.requestedAt)
  const idlePct = m.idleSince && m.status === 'idle' ? Math.min(100, (hoursBetween(m.idleSince) / Math.max(lifeHours, 0.01)) * 100) : null

  return (
    <div className="space-y-5">
      <BackLink fallbackTo="/fleet" label="Fleet" />

      <div className="flex items-start justify-between gap-3 flex-wrap">
        <div className="flex items-center gap-3">
          <span className="flex h-11 w-11 items-center justify-center rounded-xl bg-muted text-muted-foreground">
            <Server size={20} />
          </span>
          <div>
            <h1 className="text-lg font-semibold text-foreground font-mono flex items-center gap-2">
              {m.hostname || m.id.slice(0, 8)}
              <MachineStatusBadge status={m.status} />
            </h1>
            <p className="text-xs text-muted-foreground font-mono">{m.id}</p>
          </div>
        </div>
        {canDrain && (
          <button
            onClick={() => drain.mutate(undefined)}
            disabled={drain.isPending}
            className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-foreground hover:bg-accent transition-colors"
          >
            <Wind size={13} /> {drain.isPending ? 'Draining…' : 'Drain'}
          </button>
        )}
      </div>

      {m.drainReason && (
        <div className="rounded-lg border border-warning bg-warning-subtle px-3 py-2 text-xs text-muted-foreground">
          Draining: {m.drainReason}
        </div>
      )}

      {/* ── Facts + economics ── */}
      <div className="island-shell !p-0 overflow-hidden">
        <PanelHeader icon={<Gauge size={14} />} title="Machine" subtitle={`${m.provider}${m.region ? ` · ${m.region}` : ''}`} />
        <div className="p-4 sm:p-5 grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-4">
          <Stat label="Shape" value={fmtCpuMem(m.cpuMillis, m.memoryMb)} mono />
          <Stat label="Arch" value={m.arch} mono />
          {m.instanceType && <Stat label="Instance" value={`${m.instanceType}${m.capacityType === 'spot' ? ' (spot)' : ''}`} mono />}
          {m.providerRef && <Stat label="Provider ref" value={m.providerRef} mono />}
          <Stat label="Steps completed" value={String(m.stepsCompleted)} />
          <Stat label="Age" value={fmtDuration(m.requestedAt)} />
          {m.lastHeartbeatAt && <Stat label="Last heartbeat" value={fmtAgo(m.lastHeartbeatAt)} />}
          {m.agentVersion && <Stat label="Agent" value={m.agentVersion} mono />}
          {m.pricePerHourUsd != null && <Stat label="Price" value={`$${m.pricePerHourUsd.toFixed(3)}/hr`} />}
          {m.costToDateUsd != null && <Stat label="Cost to date" value={`$${m.costToDateUsd.toFixed(2)}`} accent />}
          {m.costToDateUsd != null && m.stepsCompleted > 0 && (
            <Stat label="Cost per step" value={`$${(m.costToDateUsd / m.stepsCompleted).toFixed(3)}`} />
          )}
          {idlePct != null && <Stat label="Currently idle for" value={fmtDuration(m.idleSince!)} />}
        </div>
      </div>

      {/* ── Lifecycle events ── */}
      <div className="island-shell !p-0 overflow-hidden">
        <PanelHeader icon={<Server size={14} />} title="Events" subtitle={`${events.length} recorded`} />
        <div className="divide-y divide-border">
          {events.length === 0 && (
            <p className="px-4 py-6 text-center text-xs text-muted-foreground">No events recorded.</p>
          )}
          {events.map((e, i) => (
            <EventRow key={i} event={e} />
          ))}
        </div>
      </div>
    </div>
  )
}

function EventRow({ event: e }: { event: MachineEvent }) {
  return (
    <div className="flex items-center gap-3 px-4 py-2.5 text-xs">
      <span className="font-medium text-foreground w-28 shrink-0">{e.type}</span>
      <span className="text-muted-foreground font-mono shrink-0">
        {e.from && e.to ? `${e.from} → ${e.to}` : e.to ?? ''}
      </span>
      <span className="text-muted-foreground/70 truncate flex-1">{e.reason ?? ''}</span>
      <span className="text-muted-foreground/70 shrink-0">{e.actor}</span>
      <span className="text-muted-foreground/50 shrink-0" title={e.createdAt}>{fmtAgo(e.createdAt)}</span>
    </div>
  )
}

function Stat({ label, value, mono, accent }: { label: string; value: string; mono?: boolean; accent?: boolean }) {
  return (
    <div className="space-y-0.5">
      <p className="text-[11px] text-muted-foreground uppercase tracking-wider">{label}</p>
      <p className={`text-sm ${mono ? 'font-mono' : ''} ${accent ? 'text-primary font-semibold' : 'text-foreground'}`}>{value}</p>
    </div>
  )
}

function hoursBetween(fromIso: string, toIso?: string): number {
  const from = new Date(fromIso).getTime()
  const to = toIso ? new Date(toIso).getTime() : Date.now()
  return Math.max(0, (to - from) / 3.6e6)
}
