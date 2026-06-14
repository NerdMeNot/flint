import { useState, useRef } from 'react'
import { RefreshCw, ChevronDown } from 'lucide-react'
import { useClickOutside } from '#/hooks/use-click-outside'

const OPTIONS: { label: string; ms: number | null }[] = [
  { label: 'Off', ms: null },
  { label: '5s', ms: 5_000 },
  { label: '10s', ms: 10_000 },
  { label: '30s', ms: 30_000 },
  { label: '1m', ms: 60_000 },
  { label: '5m', ms: 300_000 },
]

// Manual refresh + auto-refresh cadence, the way observability tools pair it
// with a time range. `value` is the interval in ms (null = off).
export function RefreshControl({ value, onChange, onRefresh }: {
  value: number | null
  onChange: (ms: number | null) => void
  onRefresh: () => void
}) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  useClickOutside(ref, () => setOpen(false), open)

  const current = OPTIONS.find((o) => o.ms === value) ?? OPTIONS[0]!

  return (
    <div ref={ref} className="relative inline-flex items-center rounded-lg border border-border text-xs font-medium">
      <button
        type="button"
        onClick={onRefresh}
        title="Refresh now"
        className="flex items-center px-2 py-1.5 text-muted-foreground hover:text-foreground transition-colors"
      >
        <RefreshCw size={13} className={value ? 'text-primary' : ''} />
      </button>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-1 pl-1 pr-2 py-1.5 border-l border-border text-muted-foreground hover:text-foreground transition-colors"
        title="Auto-refresh interval"
      >
        <span className={value ? 'text-primary' : ''}>{current.label}</span>
        <ChevronDown size={10} className={`opacity-50 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>

      {open && (
        <div className="absolute right-0 top-full mt-1 z-50 w-24 max-w-[calc(100vw-1rem)] rounded-lg border border-border shadow-lg overflow-hidden" style={{ background: 'var(--surface-strong)' }}>
          {OPTIONS.map((o) => (
            <button
              key={o.label}
              type="button"
              onClick={() => { onChange(o.ms); setOpen(false) }}
              className={`w-full text-left px-3 py-1.5 text-xs transition-colors ${
                o.ms === value ? 'text-primary bg-primary/5' : 'text-muted-foreground hover:text-foreground hover:bg-accent'
              }`}
            >
              {o.label}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
