import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  bucketOf,
  groupByBucket,
  median,
  parseDurationToSeconds,
  relativeToMinutes,
  runEpochMs,
} from './run-feed'

// Bucketing is calendar-based, so "now" is pinned mid-afternoon: a rolling-24h
// implementation would pass a midday check by luck and fail near midnight.
const NOW = new Date('2026-06-15T15:00:00').getTime()
const MINUTE_MS = 60_000
const DAY_MS = 86_400_000

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(NOW)
})

afterEach(() => {
  vi.useRealTimers()
})

describe('parseDurationToSeconds', () => {
  it.each([
    ['48s', 48],
    ['2m 34s', 154],
    ['1h 02m', 3720],
    ['1h 2m 3s', 3723],
  ])('parses %s', (input, expected) => {
    expect(parseDurationToSeconds(input)).toBe(expected)
  })

  it('returns 0 for a string with no recognisable units', () => {
    expect(parseDurationToSeconds('')).toBe(0)
    expect(parseDurationToSeconds('—')).toBe(0)
  })
})

describe('median', () => {
  it('averages the middle pair for an even count', () => {
    expect(median([1, 2, 3, 4])).toBe(2.5)
  })

  it('takes the middle value for an odd count', () => {
    expect(median([5, 1, 3])).toBe(3)
  })

  it('returns 0 for an empty set rather than NaN', () => {
    expect(median([])).toBe(0)
  })

  // The feed reuses the caller's array to compute a baseline; mutating it would
  // silently reorder the rendered list.
  it('does not mutate its input', () => {
    const input = [3, 1, 2]
    median(input)
    expect(input).toEqual([3, 1, 2])
  })
})

describe('relativeToMinutes', () => {
  it.each([
    ['just now', 0],
    ['30 sec ago', 0.5],
    ['8 min ago', 8],
    ['2 hours ago', 120],
    ['1 day ago', 1440],
    ['2 weeks ago', 20160],
  ])('converts %s', (input, expected) => {
    expect(relativeToMinutes(input)).toBeCloseTo(expected, 5)
  })

  it('prefers a real ISO timestamp over the phrase parser', () => {
    const iso = new Date(NOW - 90 * MINUTE_MS).toISOString()
    expect(relativeToMinutes(iso)).toBeCloseTo(90, 5)
  })

  // Sorting is by "minutes ago" ascending, so an unrecognised string has to
  // sink to the bottom instead of masquerading as brand new.
  it('sorts unrecognised strings last', () => {
    expect(relativeToMinutes('sometime')).toBe(Number.MAX_SAFE_INTEGER)
  })
})

describe('bucketOf', () => {
  it('puts this morning in Today', () => {
    expect(bucketOf(new Date('2026-06-15T00:30:00').getTime())).toBe('Today')
  })

  // The reason bucketing is calendar-based rather than rolling: a run at 11pm
  // last night is 16 hours old but is emphatically "Yesterday".
  it('puts late last night in Yesterday, not Today', () => {
    expect(bucketOf(new Date('2026-06-14T23:00:00').getTime())).toBe('Yesterday')
  })

  it('puts anything older in Earlier', () => {
    expect(bucketOf(new Date('2026-06-13T23:59:00').getTime())).toBe('Earlier')
  })
})

describe('runEpochMs', () => {
  it('prefers the exact timestamp when present', () => {
    const ts = NOW - 5 * MINUTE_MS
    expect(runEpochMs({ startedAt: '2 hours ago', startedAtTs: ts })).toBe(ts)
  })

  it('falls back to parsing the display string', () => {
    expect(runEpochMs({ startedAt: '10 min ago' })).toBeCloseTo(NOW - 10 * MINUTE_MS, -2)
  })
})

describe('groupByBucket', () => {
  const run = (id: string, startedAtTs: number) => ({ id, startedAt: '', startedAtTs })

  it('orders buckets Today → Yesterday → Earlier', () => {
    const groups = groupByBucket([
      run('old', NOW - 5 * DAY_MS),
      run('today', NOW - MINUTE_MS),
      run('yesterday', new Date('2026-06-14T10:00:00').getTime()),
    ])

    expect(groups.map((g) => g.bucket)).toEqual(['Today', 'Yesterday', 'Earlier'])
  })

  it('omits buckets with no runs', () => {
    const groups = groupByBucket([run('today', NOW - MINUTE_MS)])
    expect(groups.map((g) => g.bucket)).toEqual(['Today'])
  })

  // The feed is a timeline, so ordering must come from the timestamps and not
  // from whatever order the API happened to return.
  it('sorts newest-first within a bucket regardless of input order', () => {
    const groups = groupByBucket([
      run('older', NOW - 3 * MINUTE_MS),
      run('newest', NOW - MINUTE_MS),
      run('middle', NOW - 2 * MINUTE_MS),
    ])

    expect(groups[0]!.runs.map((r) => r.id)).toEqual(['newest', 'middle', 'older'])
  })

  it('returns nothing for an empty feed', () => {
    expect(groupByBucket([])).toEqual([])
  })

  it('does not mutate the input array', () => {
    const runs = [run('a', NOW - MINUTE_MS), run('b', NOW - 2 * MINUTE_MS)]
    const snapshot = runs.map((r) => r.id)
    groupByBucket(runs)
    expect(runs.map((r) => r.id)).toEqual(snapshot)
  })
})
