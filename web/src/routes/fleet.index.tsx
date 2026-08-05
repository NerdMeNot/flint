import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Server, ChevronRight, Cpu, DollarSign, Activity } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { PageHeader } from '#/components/PageHeader'
import { EmptyState } from '#/components/EmptyState'
import type { Machine } from '#/lib/api/types'
import { MachineStatusBadge, fmtAgo, fmtCpuMem } from '#/components/fleet/machine-bits'

export const Route = createFileRoute('/fleet/')({
  component: FleetPage,
})

const STATUS_FILTERS = ['all', 'idle', 'busy', 'provisioning', 'draining', 'failed', 'lost'] as const

function FleetPage() {
  const [status, setStatus] = useState<(typeof STATUS_FILTERS)[number]>('all')
  const [pool, setPool] = useState('all')

  const { data: poolsData } = useQuery(orpc.runners.list.queryOptions({ input: {} }))
  const poolsById = new Map((poolsData?.items ?? []).map((p) => [p.id, p.name]))

  // Live view: the fleet changes under you (boots, heartbeats, drains).
  const { data } = useSuspenseQuery({
    ...orpc.machines.list.queryOptions({
      input: {
        status: status === 'all' ? undefined : status,
        pool: pool === 'all' ? undefined : pool,
        limit: 100,
      },
    }),
    refetchInterval: 5000,
  })
  const machines = data.items

  const active = machines.filter((m) => m.status === 'idle' || m.status === 'busy')
  const burnPerHour = active.reduce((sum, m) => sum + (m.pricePerHourUsd ?? 0), 0)

  return (
    <div className="space-y-6">
      <PageHeader
        title="Fleet"
        subtitle={
          machines.length === 0
            ? 'No machines'
            : `${machines.length} machines · ${active.length} active${burnPerHour > 0 ? ` · $${burnPerHour.toFixed(2)}/hr` : ''}`
        }
      />

      <div className="flex items-center gap-2 flex-wrap">
        {STATUS_FILTERS.map((s) => (
          <button
            key={s}
            onClick={() => setStatus(s)}
            className={`rounded-full border px-2.5 py-1 text-[11px] font-medium transition-colors ${
              status === s
                ? 'border-primary bg-accent text-primary'
                : 'border-border text-muted-foreground hover:text-foreground hover:bg-accent'
            }`}
          >
            {s}
          </button>
        ))}
        {(poolsData?.items ?? []).length > 1 && (
          <select
            value={pool}
            onChange={(e) => setPool(e.target.value)}
            className="ml-auto rounded-lg border border-border bg-transparent px-2 py-1 text-xs text-foreground focus:outline-none"
          >
            <option value="all">all pools</option>
            {(poolsData?.items ?? []).map((p) => (
              <option key={p.id} value={p.id}>{p.name}</option>
            ))}
          </select>
        )}
      </div>

      {machines.length === 0 ? (
        <EmptyState
          icon={Server}
          message={
            status === 'all'
              ? 'No machines yet. Join one with a pool join token, or let an elastic pool boot on demand.'
              : `No ${status} machines.`
          }
        />
      ) : (
        <div className="island-shell !p-0 overflow-hidden divide-y divide-border">
          {machines.map((m, i) => (
            <MachineRow key={m.id} machine={m} poolName={poolsById.get(m.poolId)} index={i} />
          ))}
        </div>
      )}
    </div>
  )
}

function MachineRow({ machine: m, poolName, index }: { machine: Machine; poolName?: string; index: number }) {
  return (
    <Link
      to="/fleet/$id"
      params={{ id: m.id }}
      className="flex items-center gap-3.5 px-4 py-3 group hover:bg-accent transition-colors rise-in"
      style={{ animationDelay: `${index * 25 + 20}ms` }}
    >
      <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-muted text-muted-foreground shrink-0">
        <Server size={16} />
      </span>
      <div className="flex-1 min-w-0 space-y-1">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="text-sm font-medium text-foreground font-mono group-hover:text-primary transition-colors">
            {m.hostname || m.id.slice(0, 8)}
          </span>
          <MachineStatusBadge status={m.status} />
          {poolName && <span className="text-[11px] text-muted-foreground font-mono">{poolName}</span>}
        </div>
        <div className="flex items-center gap-3 text-[11px] text-muted-foreground flex-wrap">
          <span className="inline-flex items-center gap-1"><Cpu size={11} />{fmtCpuMem(m.cpuMillis, m.memoryMb)}</span>
          {m.instanceType && <span className="font-mono">{m.instanceType}{m.capacityType === 'spot' ? ' · spot' : ''}</span>}
          {m.pricePerHourUsd != null && (
            <span className="inline-flex items-center gap-1"><DollarSign size={11} />{m.pricePerHourUsd.toFixed(3)}/hr</span>
          )}
          <span className="inline-flex items-center gap-1"><Activity size={11} />{m.stepsCompleted} steps</span>
          {m.lastHeartbeatAt && <span>hb {fmtAgo(m.lastHeartbeatAt)}</span>}
        </div>
      </div>
      <ChevronRight size={15} className="text-muted-foreground/40 group-hover:text-foreground transition-colors shrink-0" />
    </Link>
  )
}
