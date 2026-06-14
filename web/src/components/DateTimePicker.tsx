import { useState, useRef } from 'react'
import { CalendarDays } from 'lucide-react'
import { useClickOutside } from '#/hooks/use-click-outside'
import { formatDateTime } from '#/lib/format-time'
import { Calendar, type CalendarView } from '#/components/Calendar'
import { TimeField } from '#/components/TimeField'

const sameDay = (a: Date, b: Date) =>
  a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()

/**
 * Reusable themed date + time picker (popover field). Composes the shared
 * Calendar + TimeField. Value/onChange are epoch ms.
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
  const [view, setView] = useState<CalendarView>(() => {
    const d = sel ?? new Date()
    return { y: d.getFullYear(), m: d.getMonth() }
  })

  const pickDay = (date: Date) => {
    const base = sel ?? new Date()
    onChange(new Date(date.getFullYear(), date.getMonth(), date.getDate(), base.getHours(), base.getMinutes(), 0, 0).getTime())
  }
  const setTime = (h: number, mi: number) => {
    const base = sel ?? new Date()
    onChange(new Date(base.getFullYear(), base.getMonth(), base.getDate(), h, mi, 0, 0).getTime())
  }
  const dayClassName = (d: Date) => {
    if (sel && sameDay(d, sel)) return 'rounded-md bg-primary text-primary-foreground font-semibold'
    if (sameDay(d, new Date())) return 'rounded-md text-primary ring-1 ring-primary/30'
    return 'rounded-md text-foreground hover:bg-accent'
  }

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
          <Calendar view={view} onView={setView} dayClassName={dayClassName} onPickDay={pickDay} />
          <div className="mt-2.5 pt-2.5 border-t border-border">
            <TimeField hour={sel ? sel.getHours() : 0} minute={sel ? sel.getMinutes() : 0} onChange={setTime} />
          </div>
        </div>
      )}
    </div>
  )
}
