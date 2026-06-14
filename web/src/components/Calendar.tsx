import { useState } from 'react'
import { ChevronLeft, ChevronRight } from 'lucide-react'

const WEEKDAYS = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa']
const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December']
const MONTHS_SHORT = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

export interface CalendarView {
  y: number
  m: number
}

const iconBtn = 'flex items-center justify-center w-6 h-6 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent transition-colors'

/**
 * Reusable month calendar with fast month/year drill (click the heading to go
 * days → months → years; pick to drill back). Selection-agnostic: the caller
 * styles each day via `dayClassName` and handles `onPickDay`. Shared by the
 * single date-time picker and the range calendar.
 */
export function Calendar({ view, onView, dayClassName, onPickDay }: {
  view: CalendarView
  onView: (v: CalendarView) => void
  dayClassName: (date: Date) => string
  onPickDay: (date: Date) => void
}) {
  const [mode, setMode] = useState<'days' | 'months' | 'years'>('days')
  const yearStart = Math.floor(view.y / 12) * 12

  const stepMonth = (delta: number) => {
    const m = view.m + delta
    if (m < 0) onView({ y: view.y - 1, m: 11 })
    else if (m > 11) onView({ y: view.y + 1, m: 0 })
    else onView({ y: view.y, m })
  }
  const step = (delta: number) =>
    mode === 'days' ? stepMonth(delta)
      : mode === 'months' ? onView({ ...view, y: view.y + delta })
        : onView({ ...view, y: view.y + delta * 12 })
  const heading =
    mode === 'days' ? `${MONTHS[view.m]} ${view.y}`
      : mode === 'months' ? `${view.y}`
        : `${yearStart} – ${yearStart + 11}`

  const startWeekday = new Date(view.y, view.m, 1).getDay()
  const daysInMonth = new Date(view.y, view.m + 1, 0).getDate()
  const cells: (number | null)[] = [
    ...Array.from({ length: startWeekday }, () => null),
    ...Array.from({ length: daysInMonth }, (_, i) => i + 1),
  ]

  return (
    <div>
      <div className="flex items-center justify-between mb-2">
        <button type="button" onClick={() => step(-1)} className={iconBtn}><ChevronLeft size={15} /></button>
        <button
          type="button"
          onClick={() => setMode(mode === 'days' ? 'months' : mode === 'months' ? 'years' : 'days')}
          className="rounded-md px-2 py-0.5 text-sm font-semibold text-foreground hover:bg-accent transition-colors"
        >
          {heading}
        </button>
        <button type="button" onClick={() => step(1)} className={iconBtn}><ChevronRight size={15} /></button>
      </div>

      {mode === 'days' && (
        <>
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
                onClick={() => onPickDay(new Date(view.y, view.m, d))}
                className={`h-7 text-xs transition-colors ${dayClassName(new Date(view.y, view.m, d))}`}
              >
                {d}
              </button>
            ))}
          </div>
        </>
      )}

      {mode === 'months' && (
        <div className="grid grid-cols-3 gap-1">
          {MONTHS_SHORT.map((label, i) => (
            <button
              key={label}
              type="button"
              onClick={() => { onView({ ...view, m: i }); setMode('days') }}
              className={`h-9 rounded-md text-xs font-medium transition-colors ${
                view.m === i ? 'bg-primary text-primary-foreground' : 'text-foreground hover:bg-accent'
              }`}
            >
              {label}
            </button>
          ))}
        </div>
      )}

      {mode === 'years' && (
        <div className="grid grid-cols-3 gap-1">
          {Array.from({ length: 12 }, (_, i) => yearStart + i).map((y) => (
            <button
              key={y}
              type="button"
              onClick={() => { onView({ ...view, y }); setMode('months') }}
              className={`h-9 rounded-md text-xs font-medium transition-colors ${
                view.y === y ? 'bg-primary text-primary-foreground' : 'text-foreground hover:bg-accent'
              }`}
            >
              {y}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
