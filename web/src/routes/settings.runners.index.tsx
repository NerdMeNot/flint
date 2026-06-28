import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { Server, Boxes, Cpu, Microchip, Plus, Zap, Cloud, ChevronRight, Crosshair } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { PageHeader } from '#/components/PageHeader'
import { Badge } from '#/components/Badge'
import type { RunnerPool } from '#/lib/api/types'

export const Route = createFileRoute('/settings/runners/')({
  component: RunnersPage,
})

function RunnersPage() {
  const { data } = useSuspenseQuery(orpc.runners.list.queryOptions({ input: {} }))
  const runners = data.items

  const managed = runners.filter((r) => r.mode === 'managed')
  const reference = runners.filter((r) => r.mode !== 'managed')

  const subtitle = runners.length === 0
    ? 'No pools yet'
    : `${runners.length} ${runners.length === 1 ? 'pool' : 'pools'}${managed.length ? ` · ${managed.length} managed` : ''}`

  return (
    <div className="space-y-6">
      <PageHeader
        title="Runner Pools"
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

      {runners.length === 0 ? (
        <div className="island-shell p-12 flex flex-col items-center gap-3 text-muted-foreground">
          <Server size={32} strokeWidth={1.2} />
          <span className="text-sm">No runner pools configured yet.</span>
          <Link
            to="/settings/runners/new"
            className="mt-1 flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-foreground hover:bg-accent transition-colors"
          >
            <Plus size={13} /> Create the first pool
          </Link>
        </div>
      ) : (
        <div className="space-y-6">
          {managed.length > 0 && (
            <PoolGroup title="Managed" caption="Flint renders the Karpenter NodePool" pools={managed} />
          )}
          {reference.length > 0 && (
            <PoolGroup title="Reference" caption="Targets nodes you manage" pools={reference} />
          )}
        </div>
      )}
    </div>
  )
}

function PoolGroup({ title, caption, pools }: {
  title: string
  caption: string
  pools: RunnerPool[]
}) {
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
  const isManaged = pool.mode === 'managed'
  // Reference pools don't control capacity-type; show it only if the selector
  // carries a known capacity-type label.
  const capacity = isManaged ? capacityLabel(pool.managed?.capacityType) : referenceCapacity(pool.nodeSelector)
  const target = isManaged ? undefined : primaryTarget(pool.nodeSelector)

  return (
    <div className="flex items-stretch group rise-in" style={{ animationDelay: `${index * 35 + 20}ms` }}>
      <Link
        to="/settings/runners/$name"
        params={{ name: pool.name }}
        className="flex items-center gap-3.5 px-4 py-3.5 flex-1 min-w-0 hover:bg-accent/30 transition-colors"
      >
        <span className={`flex h-9 w-9 items-center justify-center rounded-lg shrink-0 ${isManaged ? 'bg-primary/10 text-primary' : 'bg-muted text-muted-foreground'}`}>
          {isManaged ? <Boxes size={16} /> : <Server size={16} />}
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
              <SpecChip icon={<Cpu size={11} />}>{[pool.cpu && `${pool.cpu} vCPU`, pool.memory && `${memGB(pool.memory)} GB`].filter(Boolean).join(' · ')}</SpecChip>
            )}
            <SpecChip mono>{pool.arch || 'any'}</SpecChip>
            {pool.gpuVendor && (
              <SpecChip icon={<Microchip size={11} />} accent>
                {pool.gpuCount && pool.gpuCount > 1 ? `${pool.gpuCount}× ` : ''}{pool.gpuVendor}{pool.gpuModel ? ` ${pool.gpuModel}` : ''}
              </SpecChip>
            )}
            {capacity && <SpecChip icon={isManaged ? <Cloud size={11} /> : <Zap size={11} />}>{capacity}</SpecChip>}
            {target && <SpecChip icon={<Crosshair size={11} />} mono accent>{target}</SpecChip>}
            {isManaged && pool.managed?.scaleToZero !== false && <SpecChip>scale-to-zero</SpecChip>}
            {isManaged && pool.managed?.instanceFamilies?.length ? (
              <SpecChip mono>{pool.managed.instanceFamilies.join('/')}</SpecChip>
            ) : null}
          </div>
        </div>

        <ChevronRight size={15} className="text-muted-foreground/40 group-hover:text-foreground transition-colors shrink-0" />
      </Link>
    </div>
  )
}

function SpecChip({ children, icon, mono, accent }: { children: React.ReactNode; icon?: React.ReactNode; mono?: boolean; accent?: boolean }) {
  return (
    <span className={`inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-[11px] ${mono ? 'font-mono' : ''} ${
      accent ? 'border-primary/25 bg-primary/5 text-primary' : 'border-border bg-muted/40 text-muted-foreground'
    }`}>
      {icon}{children}
    </span>
  )
}

function capacityLabel(t?: string) {
  if (t === 'on-demand') return 'On-demand'
  if (t === 'spot') return 'Spot only'
  return 'Spot + fallback'
}

// referenceCapacity reads spot/on-demand from a reference pool's node selector
// if it carries a known cloud capacity-type label; otherwise it's unknown to Flint.
function referenceCapacity(ns?: Record<string, string>): string | undefined {
  if (!ns) return undefined
  const lower: Record<string, string> = {}
  for (const [k, v] of Object.entries(ns)) lower[k.toLowerCase()] = v.toLowerCase()
  const ct = lower['karpenter.sh/capacity-type'] ?? lower['eks.amazonaws.com/capacitytype']
  if (ct === 'spot') return 'Spot'
  if (ct === 'on-demand' || ct === 'on_demand') return 'On-demand'
  if (lower['cloud.google.com/gke-spot'] === 'true') return 'Spot'
  if (lower['kubernetes.azure.com/scalesetpriority'] === 'spot') return 'Spot'
  return undefined
}

// primaryTarget picks the most meaningful nodeSelector entry to show as the
// pool's "points at" chip — the node-group key if present, else the first label
// that isn't the always-pinned arch.
function primaryTarget(ns?: Record<string, string>): string | undefined {
  if (!ns) return undefined
  const preferred = ['karpenter.sh/nodepool', 'eks.amazonaws.com/nodegroup', 'cloud.google.com/gke-nodepool', 'agentpool']
  for (const k of preferred) if (ns[k]) return `${shortKey(k)}=${ns[k]}`
  const entry = Object.entries(ns).find(([k]) => k !== 'kubernetes.io/arch')
  return entry ? `${shortKey(entry[0])}=${entry[1]}` : undefined
}

function shortKey(k: string) {
  const slash = k.lastIndexOf('/')
  return slash >= 0 ? k.slice(slash + 1) : k
}

// memGB renders a stored memory quantity (e.g. "8Gi") as a GB number for display.
function memGB(s: string): string {
  const m = s.trim().match(/^([0-9.]+)\s*([A-Za-z]*)$/)
  if (!m) return s
  const n = parseFloat(m[1])
  if (Number.isNaN(n)) return s
  const gb = m[2] === 'Mi' ? n / 1024 : m[2] === 'M' ? n / 1000 : m[2] === 'Ti' || m[2] === 'T' ? n * 1024 : n
  return String(Math.round(gb * 100) / 100)
}
