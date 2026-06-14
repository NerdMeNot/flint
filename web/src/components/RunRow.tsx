import { Link } from '@tanstack/react-router'
import {
  GitBranch,
  GitCommitHorizontal,
  GitPullRequest,
  MousePointerClick,
  CalendarClock,
  Timer,
  ChevronUp,
  ChevronDown,
} from 'lucide-react'
import { runStatusVisualFor } from '#/lib/status'
import { parseDurationToSeconds } from '#/lib/run-feed'
import { formatTimeline, formatExact } from '#/lib/format-time'
import type { RunStepSummary, StepStatusValue } from '#/lib/api/types'

interface Run {
  id: string
  projectId: string
  projectName: string
  projectColour: string
  status: string
  branch: string
  commitSha: string
  commitMessage: string
  triggeredBy: string
  triggerType: string
  duration: string
  startedAt: string
  startedAtTs?: number
  finishedAtTs?: number
  workflowFile: string
  steps?: RunStepSummary[]
}

interface RunRowProps {
  run: Run
  showProject?: boolean
  /** Project's median run duration (seconds) for the slower/faster indicator. */
  baselineSecs?: number
}

const TRIGGERS: Record<string, { Icon: typeof GitBranch; label: string }> = {
  push: { Icon: GitCommitHorizontal, label: 'Push' },
  pull_request: { Icon: GitPullRequest, label: 'Pull request' },
  manual: { Icon: MousePointerClick, label: 'Manual' },
  schedule: { Icon: CalendarClock, label: 'Scheduled' },
}

const PIP_CLASS: Record<StepStatusValue, string> = {
  succeeded: 'bg-success',
  failed: 'bg-destructive',
  running: 'bg-primary animate-pulse',
  cancelled: 'bg-muted-foreground/40',
  skipped: 'bg-muted-foreground/15',
  pending: 'bg-border',
  queued: 'bg-border',
  waiting: 'bg-border',
}

function StagePips({ steps }: { steps: RunStepSummary[] }) {
  const shown = steps.slice(0, 10)
  const overflow = steps.length - shown.length
  return (
    <div className="hidden sm:flex items-center gap-0.5 shrink-0" title={steps.map((s) => `${s.name}: ${s.status}`).join('\n')}>
      {shown.map((s, i) => (
        <span key={i} className={`w-3.5 h-1.5 rounded-[2px] ${PIP_CLASS[s.status] ?? 'bg-border'}`} />
      ))}
      {overflow > 0 && <span className="text-[10px] text-muted-foreground/60 ml-0.5">+{overflow}</span>}
    </div>
  )
}

function DurationDelta({ durationSecs, baselineSecs }: { durationSecs: number; baselineSecs?: number }) {
  if (!baselineSecs || !durationSecs) return null
  const ratio = durationSecs / baselineSecs
  if (ratio >= 1.3) {
    return <ChevronUp size={11} className="text-warning" aria-label="slower than usual" />
  }
  if (ratio <= 0.7) {
    return <ChevronDown size={11} className="text-success" aria-label="faster than usual" />
  }
  return null
}

export function RunRow({ run, showProject = true, baselineSecs }: RunRowProps) {
  const status = runStatusVisualFor(run.status)
  const accent = status.accent
  const isRunning = run.status === 'running'
  const steps = run.steps ?? []
  const durationSecs = parseDurationToSeconds(run.duration)
  const trigger = TRIGGERS[run.triggerType] ?? TRIGGERS.push!
  const done = steps.filter((s) => s.status === 'succeeded').length

  return (
    <Link
      to="/ci/runs/$id"
      params={{ id: run.id }}
      className="flex hover:bg-accent/50 transition-colors group relative"
    >
      {/* Left accent bar */}
      <div className={`w-[3px] shrink-0 ${accent} ${isRunning ? 'running-accent' : ''}`} />

      <div className="flex-1 px-4 lg:px-5 py-2.5 min-w-0 space-y-1">
        {/* Row 1: status · project · commit message · stage pips */}
        <div className="flex items-center gap-2.5">
          <span className={`shrink-0 ${status.text}`}>
            <status.Icon size={14} className={status.spin ? 'animate-spin' : undefined} />
          </span>

          {showProject && (
            <span className="flex items-center gap-1.5 shrink-0 max-w-[40%] min-w-0">
              <span className="w-1.5 h-1.5 rounded-full shrink-0" style={{ backgroundColor: run.projectColour }} />
              <span className="font-semibold text-sm text-foreground group-hover:text-primary transition-colors truncate">
                {run.projectName}
              </span>
            </span>
          )}

          <p className="flex-1 text-sm text-foreground/75 truncate min-w-0">{run.commitMessage}</p>

          {steps.length > 0 && <StagePips steps={steps} />}
        </div>

        {/* Row 2: trigger · branch · sha · duration(±) · time · actor */}
        <div className="flex items-center gap-2 pl-[26px] text-[12px] text-muted-foreground min-w-0">
          <span className="flex items-center gap-1 shrink-0" title={`${trigger.label} · ${run.workflowFile}`}>
            <trigger.Icon size={12} className="opacity-70" />
          </span>
          <span className="flex items-center gap-1 shrink-0">
            <GitBranch size={11} />
            <span className="font-mono">{run.branch}</span>
          </span>
          <span className="opacity-25">·</span>
          <span className="font-mono opacity-60 shrink-0">{run.commitSha}</span>
          <span className="opacity-25">·</span>
          <span className="flex items-center gap-1 shrink-0">
            <Timer size={11} />
            {isRunning && steps.length > 0 ? `${done}/${steps.length}` : run.duration}
            {!isRunning && <DurationDelta durationSecs={durationSecs} baselineSecs={baselineSecs} />}
          </span>
          <span className="opacity-25">·</span>
          <span
            className="opacity-50 shrink-0"
            title={run.startedAtTs
              ? `Started ${formatExact(run.startedAtTs)}${run.finishedAtTs ? `\nEnded ${formatExact(run.finishedAtTs)}` : ''}`
              : undefined}
          >
            {run.startedAtTs ? formatTimeline(run.startedAtTs) : run.startedAt}
          </span>
          <span className="hidden md:inline opacity-25">·</span>
          <span className="hidden md:inline opacity-50 shrink-0">{run.triggeredBy}</span>
        </div>
      </div>

      {isRunning && (
        <div className="absolute inset-0 pointer-events-none overflow-hidden">
          <div className="running-shimmer" />
        </div>
      )}
    </Link>
  )
}
