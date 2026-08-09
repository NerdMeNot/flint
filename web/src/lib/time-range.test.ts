import { describe, expect, it } from 'vitest'

import {
  QUICK_RANGES,
  formatRange,
  isActiveRange,
  parseRelative,
  relExpr,
  resolveTime,
} from './time-range'

// A fixed "now" so relative resolution is checked as arithmetic rather than
// against a moving wall clock.
const NOW = Date.UTC(2026, 0, 15, 12, 0, 0)
const MINUTE = 60_000
const HOUR = 3_600_000
const DAY = 86_400_000
const WEEK = 604_800_000

describe('resolveTime', () => {
  it('resolves "now" to the supplied clock, not the real one', () => {
    expect(resolveTime('now', NOW)).toBe(NOW)
  })

  it.each([
    ['now-15m', 15 * MINUTE],
    ['now-4h', 4 * HOUR],
    ['now-7d', 7 * DAY],
    ['now-2w', 2 * WEEK],
  ])('resolves %s by subtracting the right unit', (token, offset) => {
    expect(resolveTime(token, NOW)).toBe(NOW - offset)
  })

  it('passes an absolute epoch-ms token through unchanged', () => {
    expect(resolveTime(String(NOW), NOW + 999)).toBe(NOW)
  })

  it('returns undefined for an absent token', () => {
    expect(resolveTime(undefined, NOW)).toBeUndefined()
  })

  // A malformed token must not silently resolve to NaN or epoch 0 and quietly
  // widen a query to all of history.
  it.each(['now+1h', 'yesterday', 'now-h', 'now-5y', ''])(
    'returns undefined for the unparseable token %o',
    (token) => {
      expect(resolveTime(token, NOW)).toBeUndefined()
    },
  )
})

describe('parseRelative', () => {
  it('splits a relative token into amount and unit', () => {
    expect(parseRelative('now-30m')).toEqual({ n: 30, unit: 'm' })
    expect(parseRelative('now-90d')).toEqual({ n: 90, unit: 'd' })
  })

  it.each(['now', String(NOW), 'now+1h', undefined])(
    'returns null for the non-relative token %o',
    (token) => {
      expect(parseRelative(token)).toBeNull()
    },
  )

  it('round-trips with relExpr', () => {
    expect(parseRelative(relExpr(12, 'h'))).toEqual({ n: 12, unit: 'h' })
  })
})

describe('isActiveRange', () => {
  it('is inactive only when both bounds are absent', () => {
    expect(isActiveRange({})).toBe(false)
    expect(isActiveRange({ from: 'now-1h' })).toBe(true)
    expect(isActiveRange({ to: 'now' })).toBe(true)
  })
})

describe('formatRange', () => {
  it('labels an empty range as unbounded', () => {
    expect(formatRange({})).toBe('Any time')
  })

  // Every preset must render as its own label rather than falling through to
  // the generic relative or absolute wording.
  it.each(QUICK_RANGES)('renders the $label preset by name', (quick) => {
    expect(formatRange({ from: quick.from, to: quick.to })).toBe(quick.label)
  })

  it('describes a custom relative range in words', () => {
    expect(formatRange({ from: 'now-3h', to: 'now' })).toBe('Last 3 hours')
    expect(formatRange({ from: 'now-5w' })).toBe('Last 5 weeks')
  })

  it('falls back to explicit endpoints for an absolute range', () => {
    const label = formatRange({ from: String(NOW), to: String(NOW + HOUR) })
    expect(label).toContain('→')
    expect(label).not.toBe('Any time')
  })

  it('shows an open upper bound as "now"', () => {
    expect(formatRange({ from: String(NOW) })).toContain('now')
  })
})

describe('QUICK_RANGES', () => {
  it('are all parseable by resolveTime', () => {
    for (const quick of QUICK_RANGES) {
      expect(resolveTime(quick.from, NOW), quick.label).toBeDefined()
      expect(resolveTime(quick.to, NOW), quick.label).toBeDefined()
    }
  })

  it('all start before they end', () => {
    for (const quick of QUICK_RANGES) {
      expect(resolveTime(quick.from, NOW)!, quick.label)
        .toBeLessThan(resolveTime(quick.to, NOW)!)
    }
  })
})
