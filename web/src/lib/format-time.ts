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
