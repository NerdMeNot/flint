import type { ProjectHealth, RunStatusValue } from '#/lib/api/types'

const SPARK: Record<RunStatusValue, string> = {
  succeeded: 'bg-success/70',
  failed: 'bg-destructive',
  running: 'bg-primary animate-pulse',
  pending: 'bg-border',
  cancelled: 'bg-muted-foreground/40',
}

// A pass/fail run-history sparkline — newest-first input, rendered oldest→newest.
export function RunSparkline({ runs, barClass = 'w-1 h-3' }: { runs: RunStatusValue[]; barClass?: string }) {
  const ordered = [...runs].reverse()
  return (
    <div className="flex items-end gap-[2px] shrink-0">
      {ordered.map((s, i) => (
        <span key={i} className={`${barClass} rounded-[1px] ${SPARK[s] ?? 'bg-border'}`} />
      ))}
    </div>
  )
}

// Health summary for a project: recent-run sparkline + pass rate + failing flag.
export function ProjectHealthBar({ health }: { health?: ProjectHealth }) {
  if (!health || health.totalRuns === 0) {
    return <span className="text-[11px] text-muted-foreground/50">no runs yet</span>
  }
  const rate = health.passRate
  const rateColor = rate >= 90 ? 'text-success' : rate >= 70 ? 'text-warning' : 'text-destructive'
  return (
    <div className="flex items-center gap-2 min-w-0" title={`${rate}% pass over last ${health.totalRuns} runs`}>
      <RunSparkline runs={health.recentRuns} />
      <span className={`text-[12px] font-medium tabular-nums ${rateColor}`}>{rate}%</span>
      {health.failingNow && (
        <span className="flex items-center gap-1 text-[11px] font-medium text-destructive">
          <span className="w-1.5 h-1.5 rounded-full bg-destructive" /> failing
        </span>
      )}
    </div>
  )
}
