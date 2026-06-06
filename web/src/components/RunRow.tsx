import { Link } from '@tanstack/react-router'
import {
  GitBranch,
  GitCommit,
  Clock,
  CheckCircle,
  XCircle,
  Loader2,
  Ban,
  Timer,
} from 'lucide-react'

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
  workflowFile: string
}

interface RunRowProps {
  run: Run
  showProject?: boolean
  action?: React.ReactNode
}

const accentColors: Record<string, string> = {
  succeeded: 'bg-success',
  failed: 'bg-destructive',
  running: 'bg-primary',
  pending: 'bg-muted-foreground/30',
  cancelled: 'bg-muted-foreground/30',
}

const statusConfig: Record<string, { icon: React.ReactNode; label: string; className: string }> = {
  succeeded: {
    icon: <CheckCircle size={14} />,
    label: 'Passed',
    className: 'text-success',
  },
  failed: {
    icon: <XCircle size={14} />,
    label: 'Failed',
    className: 'text-destructive',
  },
  running: {
    icon: <Loader2 size={14} className="animate-spin" />,
    label: 'Running',
    className: 'text-primary',
  },
  pending: {
    icon: <Clock size={14} />,
    label: 'Pending',
    className: 'text-muted-foreground',
  },
  cancelled: {
    icon: <Ban size={14} />,
    label: 'Cancelled',
    className: 'text-muted-foreground',
  },
}

// Parse duration string like "2m 34s" to seconds for relative bar
function parseDurationToSeconds(dur: string): number {
  let total = 0
  const mMatch = dur.match(/(\d+)m/)
  const sMatch = dur.match(/(\d+)s/)
  if (mMatch) total += parseInt(mMatch[1]!) * 60
  if (sMatch) total += parseInt(sMatch[1]!)
  return total || 1
}

export function RunRow({ run, showProject = true, action }: RunRowProps) {
  const status = statusConfig[run.status] ?? statusConfig.pending!
  const accent = accentColors[run.status] ?? accentColors.pending!
  const isRunning = run.status === 'running'
  const durationSecs = parseDurationToSeconds(run.duration)
  // Assume 5min (300s) as a "typical" run for the relative bar
  const durationPct = Math.min(100, (durationSecs / 300) * 100)

  return (
    <Link
      to="/runs/$id"
      params={{ id: run.id }}
      className="flex hover:bg-accent/50 transition-colors group relative"
    >
      {/* Left accent bar */}
      <div className={`w-[3px] shrink-0 ${accent} ${isRunning ? 'running-accent' : ''}`} />

      {/* Content */}
      <div className="flex-1 px-4 lg:px-5 py-3 lg:py-3.5 min-w-0 space-y-1.5">
        {/* Row 1: Status + project + commit message */}
        <div className="flex items-start gap-3">
          <span className={`shrink-0 mt-2 ${status.className}`}>
            {status.icon}
          </span>

          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              {showProject && (
                <>
                  <div className="w-1.5 h-1.5 rounded-full shrink-0" style={{ backgroundColor: run.projectColour }} />
                  <span className="font-semibold text-base text-foreground group-hover:text-primary transition-colors truncate">
                    {run.projectName}
                  </span>
                  <span className="text-muted-foreground/30">·</span>
                </>
              )}
              <span className="text-xs text-muted-foreground font-mono opacity-50">{run.workflowFile}</span>
            </div>

            {/* Commit message — prominent */}
            <div className="flex items-center gap-1.5 mt-1">
              <GitCommit size={12} className="text-muted-foreground/40 shrink-0" />
              <p className="text-sm text-foreground/80 truncate">{run.commitMessage}</p>
            </div>
          </div>

          {/* Action button (cancel/retry) */}
          {action && (
            <div className="shrink-0" onClick={(e) => e.preventDefault()}>
              {action}
            </div>
          )}
        </div>

        {/* Row 2: Metadata — compact with dot separators */}
        <div className="flex items-center gap-0 pl-[26px] text-[12px] text-muted-foreground">
          <span className="flex items-center gap-1">
            <GitBranch size={11} />
            <span className="font-mono">{run.branch}</span>
          </span>

          <span className="mx-2 opacity-25">·</span>

          <span className="font-mono opacity-60">{run.commitSha}</span>

          <span className="mx-2 opacity-25">·</span>

          <span className="flex items-center gap-1">
            <Timer size={11} />
            {run.duration}
          </span>

          {/* Duration bar */}
          <div className="hidden sm:flex items-center gap-1.5 ml-2">
            <div className="w-[40px] h-[3px] rounded-full bg-border overflow-hidden">
              <div
                className={`h-full rounded-full ${accent} transition-all`}
                style={{ width: `${durationPct}%` }}
              />
            </div>
          </div>

          <span className="mx-2 opacity-25">·</span>

          <span className="opacity-50">{run.startedAt}</span>

          <span className="mx-2 opacity-25">·</span>

          <span className="opacity-40">{run.triggeredBy}</span>
        </div>
      </div>

      {/* Running shimmer overlay */}
      {isRunning && (
        <div className="absolute inset-0 pointer-events-none overflow-hidden">
          <div className="running-shimmer" />
        </div>
      )}
    </Link>
  )
}
