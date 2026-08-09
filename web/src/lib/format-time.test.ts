import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  formatAgo,
  formatClock,
  formatDateTime,
  formatExactISO,
  formatTime,
  formatTimeline,
  formatTimelineCompact,
  formatTimelineCompactISO,
  formatTimelineISO,
} from './format-time'

const MINUTE = 60_000
const HOUR = 3_600_000
const DAY = 86_400_000

// These functions all read the wall clock, so it is pinned. Mid-June avoids
// month/year boundaries confusing the "this year" branches.
const NOW = new Date('2026-06-15T12:00:00Z').getTime()
const ago = (ms: number) => NOW - ms

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(NOW)
})

afterEach(() => {
  vi.useRealTimers()
})

describe('formatAgo', () => {
  it.each([
    ['just now', 30_000],
    ['8m ago', 8 * MINUTE],
    ['3h ago', 3 * HOUR],
    ['9d ago', 9 * DAY],
    ['2mo ago', 70 * DAY],
    ['2y ago', 800 * DAY],
  ])('renders %s', (expected, elapsed) => {
    expect(formatAgo(ago(elapsed))).toBe(expected)
  })

  // Absent timestamps are extremely common (a run that never started), and the
  // empty string is what lets callers render nothing at all.
  it('returns empty for a missing timestamp', () => {
    expect(formatAgo(undefined)).toBe('')
    expect(formatAgo(0)).toBe('')
  })
})

describe('formatTimeline', () => {
  it('stays relative inside a week', () => {
    expect(formatTimeline(ago(2 * DAY))).toBe('2d ago')
    expect(formatTimeline(ago(30 * MINUTE))).toBe('30m ago')
  })

  // Past a week a date beats "37d ago", and crossing a year boundary has to
  // include the year or "Jun 6" is ambiguous.
  it('switches to a date past a week, without the year inside this year', () => {
    const label = formatTimeline(ago(30 * DAY))
    expect(label).not.toContain('ago')
    expect(label).not.toContain('2026')
  })

  it('includes the year for a prior year', () => {
    expect(formatTimeline(new Date('2024-06-06T14:34:00Z').getTime())).toContain('2024')
  })

  it('renders an em dash for a missing timestamp', () => {
    expect(formatTimeline(undefined)).toBe('—')
  })
})

describe('formatTimelineCompact', () => {
  it('stays relative inside a week', () => {
    expect(formatTimelineCompact(ago(3 * DAY))).toBe('3d ago')
  })

  // The whole point of this variant is a hard width bound — it shares a nowrap
  // row with the branch name, so overflow is stolen from the branch.
  it('never exceeds 10 characters', () => {
    const samples = [30_000, 5 * MINUTE, 5 * HOUR, 3 * DAY, 30 * DAY, 400 * DAY, 800 * DAY]
    for (const elapsed of samples) {
      const label = formatTimelineCompact(ago(elapsed))
      expect(label.length, `${label} (${elapsed}ms ago)`).toBeLessThanOrEqual(10)
    }
  })

  it('abbreviates the year for a prior year', () => {
    expect(formatTimelineCompact(new Date('2024-06-06T14:34:00Z').getTime())).toContain("'24")
  })

  it('omits the time of day, unlike formatTimeline', () => {
    const old = ago(30 * DAY)
    expect(formatTimelineCompact(old)).not.toMatch(/AM|PM/)
    expect(formatTimeline(old)).toMatch(/AM|PM/)
  })
})

// Several endpoints hand back a pre-formatted string like "3 min ago" rather
// than an ISO timestamp. Re-formatting those would render "Invalid Date", so
// the ISO variants must pass anything unparseable straight through.
describe('ISO variants pass unparseable input through', () => {
  it.each([
    ['formatTimelineISO', formatTimelineISO],
    ['formatTimelineCompactISO', formatTimelineCompactISO],
    ['formatExactISO', formatExactISO],
  ])('%s', (_name, fn) => {
    expect(fn('3 min ago')).toBe('3 min ago')
  })

  it('still formats a real ISO timestamp', () => {
    expect(formatTimelineISO(new Date(ago(2 * DAY)).toISOString())).toBe('2d ago')
  })

  it('renders a placeholder for a missing value', () => {
    expect(formatTimelineISO(undefined)).toBe('—')
    expect(formatTimelineCompactISO(undefined)).toBe('—')
    expect(formatExactISO(undefined)).toBe('')
  })
})

describe('formatTime', () => {
  it('describes recent instants in relative words', () => {
    expect(formatTime(new Date(ago(30_000)).toISOString())).toBe('just now')
    expect(formatTime(new Date(ago(5 * MINUTE)).toISOString())).toContain('minute')
  })

  it('returns a non-ISO string unchanged', () => {
    expect(formatTime('3 min ago')).toBe('3 min ago')
  })
})

describe('absolute formatters', () => {
  it('formatClock renders a time of day and no date', () => {
    const label = formatClock(NOW)
    expect(label).toMatch(/AM|PM/)
    expect(label).not.toMatch(/Jun/)
  })

  it('formatClock returns empty for a missing timestamp', () => {
    expect(formatClock(undefined)).toBe('')
  })

  it('formatDateTime renders both date and time', () => {
    const label = formatDateTime(NOW)
    expect(label).toMatch(/Jun/)
    expect(label).toMatch(/AM|PM/)
  })

  it('formatDateTime renders an em dash for a missing timestamp', () => {
    expect(formatDateTime(undefined)).toBe('—')
  })
})
