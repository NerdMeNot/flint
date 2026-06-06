/**
 * PipelineProgress — segmented bar showing step-by-step pipeline status.
 *
 * Compact mode: thin bar for run list rows (no labels).
 * Full mode: taller bar with step names on hover for run detail header.
 */

interface Step {
  name: string
  status: string
  execType?: string
}

interface PipelineProgressProps {
  steps: Step[]
  compact?: boolean
}

const statusColors: Record<string, string> = {
  succeeded: 'bg-success',
  failed: 'bg-destructive',
  running: 'bg-primary animate-pulse',
  waiting: 'bg-warning',
  queued: 'bg-purple-400/60',
  pending: 'bg-muted-foreground/20',
  skipped: 'bg-muted-foreground/10',
  cancelled: 'bg-muted-foreground/15',
}

const gateColor = 'bg-warning/70'

export function PipelineProgress({ steps, compact }: PipelineProgressProps) {
  if (steps.length === 0) return null

  const completed = steps.filter((s) => s.status === 'succeeded').length
  const failed = steps.filter((s) => s.status === 'failed').length
  const running = steps.filter((s) => s.status === 'running').length

  const height = compact ? 'h-1.5' : 'h-2.5'
  const gap = compact ? 'gap-px' : 'gap-0.5'
  const rounded = compact ? 'rounded-full' : 'rounded-sm'

  return (
    <div className="flex items-center gap-2">
      <div className={`flex ${gap} flex-1 min-w-0`}>
        {steps.map((step, i) => {
          const isGate = step.execType === 'gate'
          const color = isGate && step.status === 'pending'
            ? gateColor
            : statusColors[step.status] ?? statusColors.pending

          return (
            <div
              key={step.name}
              className={`${height} ${rounded} ${color} flex-1 min-w-[3px] transition-colors`}
              title={`${step.name}: ${step.status}`}
              style={{
                borderRadius: i === 0
                  ? compact ? '9999px 0 0 9999px' : '2px 0 0 2px'
                  : i === steps.length - 1
                    ? compact ? '0 9999px 9999px 0' : '0 2px 2px 0'
                    : undefined,
              }}
            />
          )
        })}
      </div>

      <span className={`shrink-0 font-mono ${compact ? 'text-[11px]' : 'text-[12px]'} text-muted-foreground`}>
        {failed > 0 ? (
          <span className="text-destructive">{completed}/{steps.length}</span>
        ) : running > 0 ? (
          <span className="text-primary">{completed}/{steps.length}</span>
        ) : completed === steps.length ? (
          <span className="text-success">{completed}/{steps.length}</span>
        ) : (
          <>{completed}/{steps.length}</>
        )}
      </span>
    </div>
  )
}
