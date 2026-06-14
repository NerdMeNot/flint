import type { ProjectHealth } from '#/lib/api/types'

// A project "needs attention" when it has a persistent problem — currently red,
// or a pass rate below this threshold over the recent window — not merely one
// failed run on any branch.
export const ATTENTION_PASS_RATE = 70

export function needsAttention(health?: ProjectHealth): boolean {
  if (!health || health.totalRuns === 0) return false
  return health.failingNow || health.passRate < ATTENTION_PASS_RATE
}
