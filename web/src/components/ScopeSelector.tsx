import { useState, useRef, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Boxes, Server, X } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { useScope } from '#/lib/scope-context'

export function ScopeSelector() {
  const { workspace, setWorkspace, environment, setEnvironment } = useScope()
  const { data: wsData } = useQuery(orpc.workspaces.list.queryOptions({ input: {} }))
  const { data: envData } = useQuery(orpc.environments.list.queryOptions({ input: {} }))
  const workspaces = (wsData as any)?.items ?? []
  const environments = (envData as any)?.items ?? []

  const wsLabel = workspace
    ? workspaces.find((w: any) => w.slug === workspace)?.name ?? workspace
    : 'All workspaces'
  const envLabel = environment
    ? environments.find((e: any) => e.name === environment)?.name ?? environment
    : 'All environments'

  return (
    <div className="flex items-center gap-1.5 min-w-0">
      <ScopeSegment
        icon={<Boxes size={12} />}
        label={wsLabel}
        active={!!workspace}
        items={workspaces.map((w: any) => ({ key: w.slug, label: w.name }))}
        selected={workspace}
        onSelect={(key) => setWorkspace(key === workspace ? undefined : key)}
        onClear={() => setWorkspace(undefined)}
      />
      <ScopeSegment
        icon={<Server size={12} />}
        label={envLabel}
        active={!!environment}
        items={environments.map((e: any) => ({ key: e.name, label: e.name }))}
        selected={environment}
        onSelect={(key) => setEnvironment(key === environment ? undefined : key)}
        onClear={() => setEnvironment(undefined)}
      />
    </div>
  )
}

// ---------------------------------------------------------------------------
// Individual breadcrumb segment with dropdown
// ---------------------------------------------------------------------------

interface ScopeSegmentProps {
  icon: React.ReactNode
  label: string
  active: boolean
  items: { key: string; label: string }[]
  selected: string | undefined
  onSelect: (key: string) => void
  onClear: () => void
}

function ScopeSegment({ icon, label, active, items, selected, onSelect, onClear }: ScopeSegmentProps) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [])

  return (
    <div ref={ref} className="relative min-w-0">
      <div className={`flex items-center rounded-md border transition-colors ${
        active
          ? 'border-primary/25 bg-primary/5'
          : 'border-transparent hover:border-border'
      }`}>
        <button
          type="button"
          onClick={() => setOpen(!open)}
          className={`flex items-center gap-1.5 min-w-0 px-2 py-1 text-xs font-medium transition-colors ${
            active
              ? 'text-primary'
              : 'text-muted-foreground/50 hover:text-muted-foreground'
          }`}
        >
          <span className="shrink-0 opacity-60">{icon}</span>
          <span className="truncate max-w-[130px]">{label}</span>
        </button>
        {active && (
          <button
            type="button"
            onClick={() => { onClear(); setOpen(false) }}
            className="shrink-0 flex items-center px-1.5 py-1 border-l border-primary/20 text-primary/40 hover:text-primary transition-colors"
          >
            <X size={10} />
          </button>
        )}
      </div>

      {open && (
        <div
          className="absolute top-full left-0 mt-1.5 min-w-[180px] w-max rounded-lg border border-border shadow-xl overflow-hidden z-50"
          style={{ background: 'var(--surface-strong)' }}
        >
          {items.map((item) => (
            <button
              key={item.key}
              type="button"
              onClick={() => { onSelect(item.key); setOpen(false) }}
              className={`w-full flex items-center px-3 py-2 text-xs font-medium whitespace-nowrap transition-colors ${
                item.key === selected
                  ? 'text-primary bg-primary/5'
                  : 'text-muted-foreground hover:text-foreground hover:bg-accent'
              }`}
            >
              {item.label}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
