import { formatDateTime } from '#/lib/format-time'

// A time range is two tokens, Kibana-style: each is either a relative
// expression ("now", "now-24h") or an absolute epoch-ms string. Storing tokens
// (not resolved ms) keeps relative ranges live and the URL shareable.
export interface TimeRange {
  from?: string
  to?: string
}

export interface QuickRange {
  label: string
  from: string
  to: string
}

export const QUICK_RANGES: QuickRange[] = [
  { label: 'Last 15 minutes', from: 'now-15m', to: 'now' },
  { label: 'Last 30 minutes', from: 'now-30m', to: 'now' },
  { label: 'Last 1 hour', from: 'now-1h', to: 'now' },
  { label: 'Last 4 hours', from: 'now-4h', to: 'now' },
  { label: 'Last 12 hours', from: 'now-12h', to: 'now' },
  { label: 'Last 24 hours', from: 'now-24h', to: 'now' },
  { label: 'Last 2 days', from: 'now-2d', to: 'now' },
  { label: 'Last 7 days', from: 'now-7d', to: 'now' },
  { label: 'Last 30 days', from: 'now-30d', to: 'now' },
  { label: 'Last 90 days', from: 'now-90d', to: 'now' },
]

export type RelUnit = 'm' | 'h' | 'd' | 'w'
const UNIT_MS: Record<RelUnit, number> = { m: 60_000, h: 3_600_000, d: 86_400_000, w: 604_800_000 }
export const UNIT_LABELS: Record<RelUnit, string> = { m: 'minutes', h: 'hours', d: 'days', w: 'weeks' }

const REL_RE = /^now-(\d+)([mhdw])$/

// Resolve a token to epoch ms against the given "now". Relative tokens stay live;
// absolute (numeric) tokens pass through.
export function resolveTime(token: string | undefined, now: number): number | undefined {
  if (!token) return undefined
  if (token === 'now') return now
  const m = REL_RE.exec(token)
  if (m) return now - parseInt(m[1]!, 10) * UNIT_MS[m[2] as RelUnit]
  const n = Number(token)
  return Number.isFinite(n) ? n : undefined
}

export function parseRelative(token: string | undefined): { n: number; unit: RelUnit } | null {
  if (!token) return null
  const m = REL_RE.exec(token)
  return m ? { n: parseInt(m[1]!, 10), unit: m[2] as RelUnit } : null
}

export function relExpr(n: number, unit: RelUnit): string {
  return `now-${n}${unit}`
}

export function isActiveRange(r: TimeRange): boolean {
  return !!(r.from || r.to)
}

// Human label for the trigger button.
export function formatRange(r: TimeRange): string {
  if (!isActiveRange(r)) return 'Any time'
  const quick = QUICK_RANGES.find((q) => q.from === r.from && q.to === r.to)
  if (quick) return quick.label
  const rel = parseRelative(r.from)
  if (rel && (r.to === 'now' || !r.to)) return `Last ${rel.n} ${UNIT_LABELS[rel.unit]}`
  const now = Date.now()
  const fromMs = resolveTime(r.from, now)
  const toMs = resolveTime(r.to, now)
  return `${fromMs ? formatDateTime(fromMs) : '…'} → ${toMs ? formatDateTime(toMs) : 'now'}`
}
