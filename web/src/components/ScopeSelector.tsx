import { useState, useRef, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Boxes, Server, Check, ChevronDown } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { useScope } from '#/lib/scope-context'

export function ScopeSelector() {
  const {
    workspaces, toggleWorkspace,
    environments, toggleEnvironment,
  } = useScope()
  const { data: wsData } = useQuery(orpc.workspaces.list.queryOptions({ input: {} }))
  const { data: envData } = useQuery(orpc.environments.list.queryOptions({ input: {} }))
  const wsItems = (wsData as any)?.items ?? []
  const envItems = (envData as any)?.items ?? []

  return (
    <div className="flex items-center gap-1.5 min-w-0">
      <ScopeSegment
        icon={<Boxes size={13} />}
        label="Workspaces"
        items={wsItems.map((w: any) => ({ key: w.slug, label: w.name }))}
        selected={workspaces}
        onToggle={toggleWorkspace}
      />
      <ScopeSegment
        icon={<Server size={13} />}
        label="Environments"
        items={envItems.map((e: any) => ({ key: e.name, label: e.name }))}
        selected={environments}
        onToggle={toggleEnvironment}
      />
    </div>
  )
}

// ---------------------------------------------------------------------------
// Compact trigger: icon + label + count badge. Selection is rendered as
// chips by ScopeChips below the header, so this never grows with the choice.
// ---------------------------------------------------------------------------

interface ScopeSegmentProps {
  icon: React.ReactNode
  label: string
  items: { key: string; label: string }[]
  selected: string[]
  onToggle: (key: string) => void
}

function ScopeSegment({ icon, label, items, selected, onToggle }: ScopeSegmentProps) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [])

  const active = selected.length > 0
  const selectedSet = new Set(selected)

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className={`flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs font-medium transition-colors ${
          active
            ? 'border-primary/30 bg-primary/5 text-primary'
            : 'border-transparent text-muted-foreground/60 hover:text-muted-foreground hover:border-border'
        }`}
      >
        <span className={`shrink-0 ${active ? 'opacity-90' : 'opacity-70'}`}>{icon}</span>
        <span>{label}</span>
        {active && (
          <span
            className="flex items-center justify-center rounded-full bg-primary text-primary-foreground text-[10px] font-bold leading-none min-w-[16px] h-[16px] px-1"
          >
            {selected.length}
          </span>
        )}
        <ChevronDown size={11} className={`opacity-50 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>

      {open && (
        <div
          className="absolute top-full left-0 mt-1.5 min-w-[220px] w-max rounded-lg border border-border shadow-xl overflow-hidden z-50"
          style={{ background: 'var(--surface-strong)' }}
        >
          {items.length === 0 && (
            <div className="px-3 py-2 text-xs text-muted-foreground italic">No items</div>
          )}
          {items.map((item) => {
            const isSelected = selectedSet.has(item.key)
            return (
              <button
                key={item.key}
                type="button"
                onClick={() => onToggle(item.key)}
                className={`w-full flex items-center gap-2 px-3 py-2 text-xs font-medium whitespace-nowrap transition-colors ${
                  isSelected
                    ? 'text-primary bg-primary/5'
                    : 'text-muted-foreground hover:text-foreground hover:bg-accent'
                }`}
              >
                <span
                  className={`flex h-4 w-4 shrink-0 items-center justify-center rounded border transition-colors ${
                    isSelected
                      ? 'border-primary bg-primary text-primary-foreground'
                      : 'border-border bg-transparent'
                  }`}
                >
                  {isSelected && <Check size={11} strokeWidth={3} />}
                </span>
                <span className="flex-1 text-left">{item.label}</span>
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}
