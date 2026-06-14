import { useState, useRef } from 'react'
import { CalendarDays, ChevronLeft, ChevronRight, Clock } from 'lucide-react'
import { useClickOutside } from '#/hooks/use-click-outside'
import { formatDateTime } from '#/lib/format-time'

const WEEKDAYS = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa']
const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December']

const pad2 = (n: number) => String(n).padStart(2, '0')
const clamp = (n: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, n))

/**
 * Reusable themed date + time picker. A popover field with a month calendar and
 * an HH:MM time row — fully on-theme (no native datetime-local). Value/onChange
 * are epoch ms.
 */
export function DateTimePicker({ value, onChange, placeholder = 'Pick date & time' }: {
  value?: number
  onChange: (ms: number) => void
  placeholder?: string
}) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  useClickOutside(ref, () => setOpen(false), open)

  const sel = value ? new Date(value) : null
  const [view, setView] = useState(() => {
    const d = sel ?? new Date()
    return { y: d.getFullYear(), m: d.getMonth() }
  })

  const hour = sel ? sel.getHours() : 0
  const minute = sel ? sel.getMinutes() : 0

  const pickDay = (day: number) => {
    const base = sel ?? new Date()
    onChange(new Date(view.y, view.m, day, base.getHours(), base.getMinutes(), 0, 0).getTime())
  }
  const setHM = (h: number, mi: number) => {
    const base = sel ?? new Date(view.y, view.m, new Date().getDate())
    onChange(new Date(base.getFullYear(), base.getMonth(), base.getDate(), clamp(h, 0, 23), clamp(mi, 0, 59), 0, 0).getTime())
  }

  const stepMonth = (delta: number) => setView((v) => {
    const m = v.m + delta
    if (m < 0) return { y: v.y - 1, m: 11 }
    if (m > 11) return { y: v.y + 1, m: 0 }
    return { y: v.y, m }
  })

  const startWeekday = new Date(view.y, view.m, 1).getDay()
  const daysInMonth = new Date(view.y, view.m + 1, 0).getDate()
  const cells: (number | null)[] = [
    ...Array.from({ length: startWeekday }, () => null),
    ...Array.from({ length: daysInMonth }, (_, i) => i + 1),
  ]
  const today = new Date()
  const isToday = (d: number) => today.getFullYear() === view.y && today.getMonth() === view.m && today.getDate() === d
  const isSel = (d: number) => !!sel && sel.getFullYear() === view.y && sel.getMonth() === view.m && sel.getDate() === d

  const iconBtn = 'flex items-center justify-center w-6 h-6 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent transition-colors'

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center gap-2 rounded-lg border border-border px-3 py-2 text-sm text-left transition-colors hover:border-ring/40 focus:outline-none focus:ring-2 focus:ring-ring/40"
      >
        <CalendarDays size={13} className="text-muted-foreground shrink-0" />
        <span className={value ? 'text-foreground' : 'text-muted-foreground/50'}>
          {value ? formatDateTime(value) : placeholder}
        </span>
      </button>

      {open && (
        <div className="absolute left-0 top-full mt-1 z-50 w-[256px] rounded-lg border border-border p-2.5 shadow-xl" style={{ background: 'var(--surface-strong)' }}>
          <div className="flex items-center justify-between mb-2">
            <button type="button" onClick={() => stepMonth(-1)} className={iconBtn}><ChevronLeft size={15} /></button>
            <span className="text-sm font-semibold text-foreground">{MONTHS[view.m]} {view.y}</span>
            <button type="button" onClick={() => stepMonth(1)} className={iconBtn}><ChevronRight size={15} /></button>
          </div>

          <div className="grid grid-cols-7 gap-0.5 mb-1">
            {WEEKDAYS.map((w) => (
              <span key={w} className="text-center text-[10px] font-medium text-muted-foreground/50 py-0.5">{w}</span>
            ))}
          </div>

          <div className="grid grid-cols-7 gap-0.5">
            {cells.map((d, i) => d === null ? <span key={i} /> : (
              <button
                key={i}
                type="button"
                onClick={() => pickDay(d)}
                className={`h-7 rounded-md text-xs transition-colors ${
                  isSel(d)
                    ? 'bg-primary text-primary-foreground font-semibold'
                    : isToday(d)
                      ? 'text-primary ring-1 ring-primary/30'
                      : 'text-foreground hover:bg-accent'
                }`}
              >
                {d}
              </button>
            ))}
          </div>

          <div className="flex items-center gap-1.5 mt-2.5 pt-2.5 border-t border-border">
            <Clock size={13} className="text-muted-foreground shrink-0" />
            <input
              type="text" inputMode="numeric" aria-label="Hour"
              value={pad2(hour)}
              onChange={(e) => setHM(parseInt(e.target.value.replace(/\D/g, '') || '0', 10), minute)}
              className="w-10 rounded-md border border-border bg-transparent px-1.5 py-1 text-sm text-center text-foreground focus:outline-none focus:ring-2 focus:ring-ring/40"
            />
            <span className="text-muted-foreground">:</span>
            <input
              type="text" inputMode="numeric" aria-label="Minute"
              value={pad2(minute)}
              onChange={(e) => setHM(hour, parseInt(e.target.value.replace(/\D/g, '') || '0', 10))}
              className="w-10 rounded-md border border-border bg-transparent px-1.5 py-1 text-sm text-center text-foreground focus:outline-none focus:ring-2 focus:ring-ring/40"
            />
            <span className="ml-auto text-[10px] uppercase tracking-wider text-muted-foreground/50">24h</span>
          </div>
        </div>
      )}
    </div>
  )
}
