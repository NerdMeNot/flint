import { Clock, CheckCircle, XCircle, Loader2, GitCommitHorizontal, GitPullRequest, MousePointerClick, CalendarClock, Pause } from 'lucide-react'

export function StatusBadge({ status }: { status: string }) {
  const config: Record<string, { icon: React.ReactNode; label: string; className: string }> = {
    succeeded: {
      icon: <CheckCircle size={12} />,
      label: 'Passed',
      className: 'bg-success-subtle text-success border-success',
    },
    failed: {
      icon: <XCircle size={12} />,
      label: 'Failed',
      className: 'bg-destructive-subtle text-destructive border-destructive',
    },
    running: {
      icon: <Loader2 size={12} className="animate-spin" />,
      label: 'Running',
      className: 'bg-accent text-primary border-primary',
    },
    pending: {
      icon: <Clock size={12} />,
      label: 'Pending',
      className: 'bg-secondary text-muted-foreground border-border',
    },
    waiting: {
      icon: <Clock size={12} />,
      label: 'Waiting',
      className: 'bg-warning-subtle text-warning border-warning',
    },
    paused: {
      icon: <Pause size={12} />,
      label: 'Paused',
      className: 'bg-warning-subtle text-warning border-warning',
    },
    cancelled: {
      icon: <Clock size={12} />,
      label: 'Cancelled',
      className: 'bg-secondary text-muted-foreground border-border',
    },
  }

  const c = config[status] ?? config.pending!

  return (
    <span className={`inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[12px] font-semibold whitespace-nowrap ${c.className}`}>
      {c.icon}
      {c.label}
    </span>
  )
}

export function StatusIcon({ status, size = 18 }: { status: string; size?: number }) {
  switch (status) {
    case 'succeeded': return <CheckCircle size={size} className="text-success shrink-0" />
    case 'failed': return <XCircle size={size} className="text-destructive shrink-0" />
    case 'running': return <Loader2 size={size} className="text-primary shrink-0 animate-spin" />
    case 'waiting': return <Clock size={size} className="text-warning shrink-0" />
    default: return <Clock size={size} className="text-muted-foreground shrink-0" />
  }
}

export function triggerLabel(type: string): string {
  switch (type) {
    case 'push': return 'Push'
    case 'pull_request': return 'Pull request'
    case 'manual': return 'Manual'
    case 'schedule': return 'Scheduled'
    default: return type
  }
}

export function TriggerIcon({ type }: { type: string }) {
  const props = { size: 13 }
  switch (type) {
    case 'pull_request': return <GitPullRequest {...props} />
    case 'manual': return <MousePointerClick {...props} />
    case 'schedule': return <CalendarClock {...props} />
    default: return <GitCommitHorizontal {...props} />
  }
}

// ---------------------------------------------------------------------------
// Shared bits
// ---------------------------------------------------------------------------

export function formatDuration(start: string, end: string): string {
  const ms = new Date(end).getTime() - new Date(start).getTime()
  const seconds = Math.floor(ms / 1000)
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}

export function fmtSecs(secs: number): string {
  if (secs < 60) return `${secs}s`
  const m = Math.floor(secs / 60)
  if (m < 60) return `${m}m ${secs % 60}s`
  return `${Math.floor(m / 60)}h ${m % 60}m`
}
