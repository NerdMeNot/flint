import { useState, useRef } from 'react'
import { Clock, X } from 'lucide-react'
import { useClickOutside } from '#/hooks/use-click-outside'
import { FormSelect } from '#/components/FormSelect'
import { DateTimePicker } from '#/components/DateTimePicker'
import {
  QUICK_RANGES,
  UNIT_LABELS,
  parseRelative,
  relExpr,
  resolveTime,
  formatRange,
  isActiveRange,
  type TimeRange,
  type RelUnit,
} from '#/lib/time-range'

type Tab = 'quick' | 'relative' | 'absolute'

const UNIT_OPTIONS = (Object.keys(UNIT_LABELS) as RelUnit[]).map((u) => ({ key: u, label: UNIT_LABELS[u] }))

// Kibana-style time picker: quick ranges, a relative builder, and an absolute
// from/to range. Emits TimeRange tokens (relative stay live; absolute = epoch).
export function TimeRangeFilter({ value, onChange }: { value: TimeRange; onChange: (r: TimeRange) => void }) {
  const [open, setOpen] = useState(false)
  const [tab, setTab] = useState<Tab>('quick')
  const ref = useRef<HTMLDivElement>(null)
  useClickOutside(ref, () => setOpen(false), open)

  const active = isActiveRange(value)
  const label = formatRange(value)

  // Relative tab state, seeded from the current value when it's relative.
  const initRel = parseRelative(value.from)
  const [relN, setRelN] = useState(String(initRel?.n ?? 24))
  const [relU, setRelU] = useState<RelUnit>(initRel?.unit ?? 'h')

  // Absolute tab state (epoch ms), seeded from the resolved current range.
  const now = Date.now()
  const [absFrom, setAbsFrom] = useState<number | undefined>(resolveTime(value.from, now) ?? now - 3_600_000)
  const [absTo, setAbsTo] = useState<number | undefined>(value.to && value.to !== 'now' ? resolveTime(value.to, now) : now)

  const apply = (r: TimeRange) => { onChange(r); setOpen(false) }
  const applyRelative = () => {
    const n = Math.max(1, parseInt(relN, 10) || 1)
    apply({ from: relExpr(n, relU), to: 'now' })
  }
  const applyAbsolute = () => {
    apply({ from: absFrom ? String(absFrom) : undefined, to: absTo ? String(absTo) : undefined })
  }

  return (
    <div ref={ref} className="relative">
      <div className={`flex items-center rounded-lg border text-xs font-medium whitespace-nowrap transition-colors ${
        active ? 'border-primary/30 bg-primary/5 text-primary' : 'border-border text-muted-foreground'
      }`}>
        <button type="button" onClick={() => setOpen((v) => !v)} className="flex items-center gap-1.5 px-2.5 py-1.5 hover:text-foreground transition-colors">
          <Clock size={12} />
          <span>{label}</span>
        </button>
        {active && (
          <button
            type="button"
            onClick={() => onChange({})}
            title="Clear time range"
            className="flex items-center px-1.5 py-1.5 border-l border-primary/20 text-primary/60 hover:text-primary transition-colors"
          >
            <X size={11} />
          </button>
        )}
      </div>

      {open && (
        <div className="absolute left-0 top-full mt-1 z-50 w-[300px] rounded-lg border border-border shadow-xl" style={{ background: 'var(--surface-strong)' }}>
          {/* Tabs */}
          <div className="flex items-center gap-1 p-1.5 border-b border-border">
            {(['quick', 'relative', 'absolute'] as Tab[]).map((t) => (
              <button
                key={t}
                type="button"
                onClick={() => setTab(t)}
                className={`flex-1 rounded-md px-2 py-1 text-xs font-medium capitalize transition-colors ${
                  tab === t ? 'bg-accent text-foreground' : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                {t}
              </button>
            ))}
          </div>

          {tab === 'quick' && (
            <div className="grid grid-cols-2 gap-1 p-2 max-h-72 overflow-y-auto">
              {QUICK_RANGES.map((q) => {
                const on = value.from === q.from && value.to === q.to
                return (
                  <button
                    key={q.label}
                    type="button"
                    onClick={() => apply({ from: q.from, to: q.to })}
                    className={`text-left rounded-md px-2.5 py-1.5 text-xs transition-colors ${
                      on ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:text-foreground hover:bg-accent'
                    }`}
                  >
                    {q.label}
                  </button>
                )
              })}
            </div>
          )}

          {tab === 'relative' && (
            <div className="p-3 space-y-3">
              <p className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">Last…</p>
              <div className="flex items-center gap-2">
                <input
                  type="text" inputMode="numeric"
                  value={relN}
                  onChange={(e) => setRelN(e.target.value.replace(/\D/g, ''))}
                  onKeyDown={(e) => { if (e.key === 'Enter') applyRelative() }}
                  className="w-16 rounded-lg border border-border bg-transparent px-2.5 py-2 text-sm text-center text-foreground focus:outline-none focus:ring-2 focus:ring-ring/40"
                />
                <div className="flex-1">
                  <FormSelect value={relU} onChange={(v) => setRelU(v as RelUnit)} options={UNIT_OPTIONS} />
                </div>
              </div>
              <button type="button" onClick={applyRelative} className="w-full rounded-md bg-primary px-3 py-1.5 text-xs font-semibold text-primary-foreground hover:bg-primary/90 transition-colors">
                Apply
              </button>
            </div>
          )}

          {tab === 'absolute' && (
            <div className="p-3 space-y-3">
              <div className="space-y-1">
                <span className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">From</span>
                <DateTimePicker value={absFrom} onChange={setAbsFrom} />
              </div>
              <div className="space-y-1">
                <span className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">To</span>
                <DateTimePicker value={absTo} onChange={setAbsTo} />
              </div>
              <button type="button" onClick={applyAbsolute} className="w-full rounded-lg bg-primary px-3 py-1.5 text-xs font-semibold text-primary-foreground hover:bg-primary/90 transition-colors">
                Apply
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
