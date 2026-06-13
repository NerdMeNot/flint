// Helpers for the runs triage feed: duration parsing/baseline and time bucketing.

// Parse a duration string like "2m 34s" / "48s" / "1h 02m" into seconds.
export function parseDurationToSeconds(dur: string): number {
  let total = 0
  const h = dur.match(/(\d+)\s*h/)
  const m = dur.match(/(\d+)\s*m/)
  const s = dur.match(/(\d+)\s*s/)
  if (h) total += parseInt(h[1]!) * 3600
  if (m) total += parseInt(m[1]!) * 60
  if (s) total += parseInt(s[1]!)
  return total
}

export function median(nums: number[]): number {
  if (nums.length === 0) return 0
  const sorted = [...nums].sort((a, b) => a - b)
  const mid = Math.floor(sorted.length / 2)
  return sorted.length % 2 ? sorted[mid]! : (sorted[mid - 1]! + sorted[mid]!) / 2
}

// Approximate "minutes ago" from a relative display string ("just now",
// "30 sec ago", "8 min ago", "2 hours ago", "1 day ago") or an ISO timestamp.
// Used only for bucketing/sorting the feed — display strings are kept as-is.
export function relativeToMinutes(s: string): number {
  const iso = Date.parse(s)
  if (!Number.isNaN(iso)) return Math.max(0, (Date.now() - iso) / 60000)
  if (/just now/i.test(s)) return 0
  const n = parseInt(s.match(/\d+/)?.[0] ?? '0', 10)
  if (/sec/i.test(s)) return n / 60
  if (/min/i.test(s)) return n
  if (/hour/i.test(s)) return n * 60
  if (/day/i.test(s)) return n * 1440
  if (/week/i.test(s)) return n * 10080
  return Number.MAX_SAFE_INTEGER
}

export type FeedBucket = 'Today' | 'Yesterday' | 'Earlier'

export function bucketOf(startedAt: string): FeedBucket {
  const mins = relativeToMinutes(startedAt)
  if (mins < 1440) return 'Today'
  if (mins < 2880) return 'Yesterday'
  return 'Earlier'
}

export const FEED_BUCKET_ORDER: FeedBucket[] = ['Today', 'Yesterday', 'Earlier']

// Group runs into ordered time buckets, preserving each run's order within.
export function groupByBucket<T extends { startedAt: string }>(runs: T[]): { bucket: FeedBucket; runs: T[] }[] {
  const map = new Map<FeedBucket, T[]>()
  for (const r of runs) {
    const b = bucketOf(r.startedAt)
    const arr = map.get(b) ?? []
    arr.push(r)
    map.set(b, arr)
  }
  return FEED_BUCKET_ORDER.filter((b) => map.has(b)).map((bucket) => ({ bucket, runs: map.get(bucket)! }))
}
