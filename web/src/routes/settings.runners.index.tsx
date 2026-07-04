import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { Server, Cloud, Cpu, Plus, Zap, ChevronRight, Scale, Flame } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { PageHeader } from '#/components/PageHeader'
import { Badge } from '#/components/Badge'
import { EmptyState } from '#/components/EmptyState'
import type { RunnerPool } from '#/lib/api/types'

export const Route = createFileRoute('/settings/runners/')({
  component: RunnersPage,
})

function RunnersPage() {
  const { data } = useSuspenseQuery(orpc.runners.list.queryOptions({ input: {} }))
  const pools = data.items

  const staticPools = pools.filter((p) => p.provider === 'static')
  const elastic = pools.filter((p) => p.provider !== 'static')

  const subtitle = pools.length === 0
    ? 'No pools yet'
    : `${pools.length} ${pools.length === 1 ? 'pool' : 'pools'}${elastic.length ? ` · ${elastic.length} elastic` : ''}`

  return (
    <div className="space-y-6">
      <PageHeader
        title="Machine Pools"
        subtitle={subtitle}
        action={
          <Link
            to="/settings/runners/new"
            className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium text-white transition-colors"
            style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
          >
            <Plus size={14} /> New pool
          </Link>
        }
      />

      {pools.length === 0 ? (
        <EmptyState
          icon={Server}
          message="No machine pools yet — create one, mint a join token, and point flint-agent at it."
          action={
            <Link
              to="/settings/runners/new"
              className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-foreground hover:bg-accent transition-colors"
            >
              <Plus size={13} /> Create the first pool
            </Link>
          }
        />
      ) : (
        <div className="space-y-6">
          {staticPools.length > 0 && (
            <PoolGroup title="Static" caption="Bring-your-own machines joined with a token" pools={staticPools} />
          )}
          {elastic.length > 0 && (
            <PoolGroup title="Elastic" caption="Provisioned on demand from a compute provider" pools={elastic} />
          )}
        </div>
      )}
    </div>
  )
}

function PoolGroup({ title, caption, pools }: { title: string; caption: string; pools: RunnerPool[] }) {
  return (
    <section className="space-y-2">
      <div className="flex items-baseline gap-2">
        <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">{title}</h3>
        <span className="text-[11px] text-muted-foreground opacity-50">{pools.length}</span>
        <span className="text-[11px] text-muted-foreground/70">· {caption}</span>
      </div>
      <div className="island-shell !p-0 overflow-hidden divide-y divide-border">
        {pools.map((pool, i) => (
          <PoolRow key={pool.id} pool={pool} index={i} />
        ))}
      </div>
    </section>
  )
}

function PoolRow({ pool, index }: { pool: RunnerPool; index: number }) {
  const isStatic = pool.provider === 'static'

  return (
    <div className="flex items-stretch group rise-in" style={{ animationDelay: `${index * 35 + 20}ms` }}>
      <Link
        to="/settings/runners/$name"
        params={{ name: pool.name }}
        className="flex items-center gap-3.5 px-4 py-3.5 flex-1 min-w-0 hover:bg-accent/30 transition-colors"
      >
        <span className={`flex h-9 w-9 items-center justify-center rounded-lg shrink-0 ${isStatic ? 'bg-muted text-muted-foreground' : 'bg-primary/10 text-primary'}`}>
          {isStatic ? <Server size={16} /> : <Cloud size={16} />}
        </span>
        <div className="flex-1 min-w-0 space-y-1.5">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="text-sm font-semibold text-foreground group-hover:text-primary transition-colors font-mono">{pool.name}</span>
            {pool.isDefault && <Badge variant="primary">Default</Badge>}
            {!pool.ready && <Badge variant="danger">Not ready</Badge>}
            {pool.description && <span className="text-xs text-muted-foreground truncate hidden sm:inline">— {pool.description}</span>}
          </div>
          <div className="flex items-center gap-1.5 flex-wrap">
            {(pool.cpu || pool.memory) && (
              <SpecChip icon={<Cpu size={11} />}>
                {[pool.cpu && `${pool.cpu} vCPU`, pool.memory && `${memGB(pool.memory)} GB`].filter(Boolean).join(' · ')}
              </SpecChip>
            )}
            <SpecChip mono>{pool.arch || 'any'}</SpecChip>
            <SpecChip icon={<Zap size={11} />}>{capacityLabel(pool.capacityType)}</SpecChip>
            <SpecChip icon={<Scale size={11} />}>{pool.objective}</SpecChip>
            {pool.minWarm > 0 ? (
              <SpecChip icon={<Flame size={11} />} accent>{pool.minWarm} warm</SpecChip>
            ) : (
              <SpecChip>scale to zero</SpecChip>
            )}
            <SpecChip>max {pool.maxMachines}</SpecChip>
            {!isStatic && <SpecChip mono>{pool.provider}</SpecChip>}
          </div>
        </div>
        <ChevronRight size={15} className="text-muted-foreground/40 group-hover:text-foreground transition-colors shrink-0" />
      </Link>
    </div>
  )
}

function SpecChip({ children, icon, mono, accent }: {
  children: React.ReactNode
  icon?: React.ReactNode
  mono?: boolean
  accent?: boolean
}) {
  return (
    <span className={`inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-[11px] ${mono ? 'font-mono' : ''} ${
      accent ? 'border-primary/25 bg-primary/5 text-primary' : 'border-border bg-muted/40 text-muted-foreground'
    }`}>
      {icon}{children}
    </span>
  )
}

function capacityLabel(t: string): string {
  switch (t) {
    case 'spot': return 'spot'
    case 'any': return 'spot or on-demand'
    default: return 'on-demand'
  }
}

function memGB(q: string): string {
  if (q.endsWith('Gi')) return q.slice(0, -2)
  if (q.endsWith('Mi')) return String(Number(q.slice(0, -2)) / 1024)
  return q
}
