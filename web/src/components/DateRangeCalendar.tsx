import { useState } from 'react'
import { Calendar, type CalendarView } from '#/components/Calendar'
import { TimeField } from '#/components/TimeField'

const combine = (date: Date, h: number, mi: number) =>
  new Date(date.getFullYear(), date.getMonth(), date.getDate(), h, mi, 0, 0).getTime()
const dayEq = (a: Date, b: Date) =>
  a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()

/**
 * Reusable range calendar: click a start day, then an end day (the span between
 * is highlighted). Time-of-day for each end is edited below. Values are epoch
 * ms; onChange(from, to) — `to` is undefined while only the start is chosen.
 */
export function DateRangeCalendar({ from, to, onChange }: {
  from?: number
  to?: number
  onChange: (from?: number, to?: number) => void
}) {
  const fromD = from != null ? new Date(from) : null
  const toD = to != null ? new Date(to) : null
  const [view, setView] = useState<CalendarView>(() => {
    const d = fromD ?? new Date()
    return { y: d.getFullYear(), m: d.getMonth() }
  })

  function pickDay(date: Date) {
    // Start a new range when nothing or a full range is selected; otherwise
    // complete it (restarting if the click lands before the chosen start).
    if (from == null || to != null) {
      onChange(combine(date, fromD?.getHours() ?? 0, fromD?.getMinutes() ?? 0), undefined)
      return
    }
    if (combine(date, 23, 59) < from) {
      onChange(combine(date, 0, 0), undefined)
      return
    }
    onChange(from, combine(date, toD?.getHours() ?? 23, toD?.getMinutes() ?? 59))
  }

  function dayClassName(d: Date) {
    if ((fromD && dayEq(d, fromD)) || (toD && dayEq(d, toD))) {
      return 'rounded-md bg-primary text-primary-foreground font-semibold'
    }
    const mid = combine(d, 12, 0)
    if (from != null && to != null && mid > from && mid < to) return 'bg-primary/15 text-foreground'
    if (dayEq(d, new Date())) return 'rounded-md text-primary ring-1 ring-primary/30'
    return 'rounded-md text-foreground hover:bg-accent'
  }

  return (
    <div className="space-y-2.5">
      <Calendar view={view} onView={setView} dayClassName={dayClassName} onPickDay={pickDay} />
      <div className="space-y-1.5 pt-2.5 border-t border-border">
        {fromD
          ? <TimeField label="From" hour={fromD.getHours()} minute={fromD.getMinutes()} onChange={(h, mi) => onChange(combine(fromD, h, mi), to)} />
          : <p className="text-[11px] text-muted-foreground/60 pl-1">Pick a start date…</p>}
        {toD
          ? <TimeField label="To" hour={toD.getHours()} minute={toD.getMinutes()} onChange={(h, mi) => onChange(from, combine(toD, h, mi))} />
          : fromD && <p className="text-[11px] text-muted-foreground/60 pl-1">Pick an end date…</p>}
      </div>
    </div>
  )
}
