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
// with a time range. `value` is the interval in ms (null = off). `onRefresh` may
// return a promise (e.g. React Query's refetch()) — the icon spins until it
// settles, with a short floor so instant (cached) refetches still register.
export function RefreshControl({ value, onChange, onRefresh }: {
  value: number | null
  onChange: (ms: number | null) => void
  onRefresh: () => void | Promise<unknown>
}) {
  const [open, setOpen] = useState(false)
  const [spinning, setSpinning] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  useClickOutside(ref, () => setOpen(false), open)

  const current = OPTIONS.find((o) => o.ms === value) ?? OPTIONS[0]!

  async function handleRefresh() {
    if (spinning) return
    setSpinning(true)
    const started = Date.now()
    try {
      await onRefresh()
    } finally {
      const wait = Math.max(0, 500 - (Date.now() - started))
      if (wait) await new Promise((r) => setTimeout(r, wait))
      setSpinning(false)
    }
  }

  return (
    <div ref={ref} className="relative inline-flex items-center rounded-lg border border-border text-xs font-medium">
      <button
        type="button"
        onClick={handleRefresh}
        disabled={spinning}
        title="Refresh now"
        className="flex items-center px-2 py-1.5 text-muted-foreground hover:text-foreground transition-colors"
      >
        <RefreshCw size={13} className={`${value ? 'text-primary' : ''} ${spinning ? 'animate-spin' : ''}`} />
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
        <div className="absolute right-0 top-full mt-1 z-50 w-24 max-w-[calc(100vw-1rem)] rounded-lg border border-border overlay-edge overflow-hidden" style={{ background: 'var(--surface-strong)' }}>
          {OPTIONS.map((o) => (
            <button
              key={o.label}
              type="button"
              onClick={() => { onChange(o.ms); setOpen(false) }}
              className={`w-full text-left px-3 py-1.5 text-xs transition-colors ${
                o.ms === value ? 'text-primary bg-accent' : 'text-muted-foreground hover:text-foreground hover:bg-accent'
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
