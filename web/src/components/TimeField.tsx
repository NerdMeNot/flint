import { Clock } from 'lucide-react'

const pad2 = (n: number) => String(n).padStart(2, '0')
const clamp = (n: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, n))

/**
 * Reusable 12-hour time field: HH : MM with an AM/PM toggle. Works in 24-hour
 * values (hour 0–23) but presents 12-hour; emits the resolved 24-hour hour.
 */
export function TimeField({ hour, minute, onChange, label }: {
  hour: number
  minute: number
  onChange: (hour24: number, minute: number) => void
  label?: string
}) {
  const h12 = hour % 12 === 0 ? 12 : hour % 12
  const meridiem: 'AM' | 'PM' = hour < 12 ? 'AM' : 'PM'
  const to24 = (h: number, mer: 'AM' | 'PM') => (mer === 'PM' ? (h % 12) + 12 : h % 12)
  const input = 'w-10 rounded-md border border-border bg-transparent px-1.5 py-1 text-sm text-center text-foreground focus:outline-none focus:ring-2 focus:ring-ring/40'

  return (
    <div className="flex items-center gap-1.5">
      {label
        ? <span className="text-[11px] font-medium text-muted-foreground w-8 shrink-0">{label}</span>
        : <Clock size={13} className="text-muted-foreground shrink-0" />}
      <input
        type="text" inputMode="numeric" aria-label="Hour"
        value={String(h12)}
        onChange={(e) => onChange(to24(clamp(parseInt(e.target.value.replace(/\D/g, '') || '12', 10), 1, 12), meridiem), minute)}
        className={input}
      />
      <span className="text-muted-foreground">:</span>
      <input
        type="text" inputMode="numeric" aria-label="Minute"
        value={pad2(minute)}
        onChange={(e) => onChange(hour, clamp(parseInt(e.target.value.replace(/\D/g, '') || '0', 10), 0, 59))}
        className={input}
      />
      <div className="ml-auto inline-flex items-center rounded-md border border-border p-0.5">
        {(['AM', 'PM'] as const).map((mer) => (
          <button
            key={mer}
            type="button"
            onClick={() => onChange(to24(h12, mer), minute)}
            className={`rounded px-1.5 py-0.5 text-[11px] font-medium transition-colors ${
              meridiem === mer ? 'bg-accent text-foreground' : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            {mer}
          </button>
        ))}
      </div>
    </div>
  )
}
