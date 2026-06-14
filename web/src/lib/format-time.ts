const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })
const dtf = new Intl.DateTimeFormat('en', {
  month: 'short',
  day: 'numeric',
  hour: 'numeric',
  minute: '2-digit',
})

const MINUTE = 60_000
const HOUR = 3_600_000
const DAY = 86_400_000

/**
 * Format an ISO timestamp into a human-readable string.
 * - < 1 min: "just now"
 * - < 1 hour: "5 min ago"
 * - < 24 hours: "3 hours ago"
 * - otherwise: "Jan 14, 4:00 PM"
 */
export function formatTime(iso: string): string {
  const date = new Date(iso)
  if (isNaN(date.getTime())) return iso // fallback for non-ISO strings like "3 min ago"

  const now = Date.now()
  const diff = now - date.getTime()

  if (diff < MINUTE) return 'just now'
  if (diff < HOUR) return rtf.format(-Math.floor(diff / MINUTE), 'minute')
  if (diff < DAY) return rtf.format(-Math.floor(diff / HOUR), 'hour')
  if (diff < 7 * DAY) return rtf.format(-Math.floor(diff / DAY), 'day')

  return dtf.format(date)
}

const clockFmt = new Intl.DateTimeFormat('en', { hour: 'numeric', minute: '2-digit' })
const dtfFullYear = new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric', year: 'numeric', hour: 'numeric', minute: '2-digit' })
const dtfExact = new Intl.DateTimeFormat('en', { weekday: 'short', month: 'short', day: 'numeric', year: 'numeric', hour: 'numeric', minute: '2-digit' })

/** Absolute wall-clock time of day, e.g. "2:34 PM". */
export function formatClock(ms?: number): string {
  if (!ms) return ''
  return clockFmt.format(new Date(ms))
}

/** Absolute date + time, e.g. "Jun 14, 2:34 PM". */
export function formatDateTime(ms?: number): string {
  if (!ms) return '—'
  return dtf.format(new Date(ms))
}

/** Full absolute date + time for tooltips, e.g. "Mon, Jun 14, 2026, 2:34 PM". */
export function formatExact(ms?: number): string {
  if (!ms) return '—'
  return dtfExact.format(new Date(ms))
}

/** Pure relative "ago" — always elapsed, never a date. e.g. "8m ago", "9d ago". */
export function formatAgo(ms?: number): string {
  if (!ms) return ''
  const diff = Date.now() - ms
  if (diff < MINUTE) return 'just now'
  if (diff < HOUR) return `${Math.floor(diff / MINUTE)}m ago`
  if (diff < DAY) return `${Math.floor(diff / HOUR)}h ago`
  if (diff < 30 * DAY) return `${Math.floor(diff / DAY)}d ago`
  if (diff < 365 * DAY) return `${Math.floor(diff / (30 * DAY))}mo ago`
  return `${Math.floor(diff / (365 * DAY))}y ago`
}

/**
 * Adaptive timeline label for listings: relative while recent (most scannable),
 * switching to an absolute date + time once it's older than a week (where a
 * date is more useful than "37d ago"). The year is added across year boundaries.
 *   <1m "just now" · <1h "8m ago" · <1d "3h ago" · <7d "2d ago"
 *   this year → "Jun 6, 2:34 PM" · prior year → "Jun 6, 2024, 2:34 PM"
 */
export function formatTimeline(ms?: number): string {
  if (!ms) return '—'
  const diff = Date.now() - ms
  if (diff < 7 * DAY) return formatAgo(ms)
  const d = new Date(ms)
  return (d.getFullYear() === new Date().getFullYear() ? dtf : dtfFullYear).format(d)
}
