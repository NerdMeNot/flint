import {
  CheckCircle,
  XCircle,
  Loader2,
  Clock,
  Pause,
  SkipForward,
  Ban,
  Circle,
  Timer,
} from 'lucide-react'
import type { PipelineStep } from './dag-view'

interface StepTimelineProps {
  steps: PipelineStep[]
  selectedStep: string | null
  onStepClick: (name: string) => void
}

export function StepTimeline({ steps, selectedStep, onStepClick }: StepTimelineProps) {
  // Group steps by wave for parallel visualization
  const waves = new Map<number, PipelineStep[]>()
  for (const step of steps) {
    const group = waves.get(step.wave) ?? []
    group.push(step)
    waves.set(step.wave, group)
  }

  const sortedWaves = [...waves.entries()].sort((a, b) => a[0] - b[0])

  return (
    <div className="space-y-0">
      {sortedWaves.map(([wave, waveSteps], waveIdx) => (
        <div key={wave}>
          {/* Connector line between waves */}
          {waveIdx > 0 && (
            <div className="flex justify-center py-0.5">
              <div className="w-px h-4" style={{ background: 'var(--border)' }} />
            </div>
          )}

          {/* Single step or parallel group */}
          {waveSteps.length === 1 ? (
            <StepCard
              step={waveSteps[0]!}
              isSelected={selectedStep === waveSteps[0]!.name}
              onClick={() => onStepClick(waveSteps[0]!.name)}
            />
          ) : (
            <div className="space-y-1">
              {/* Parallel indicator */}
              <div className="flex items-center gap-2 px-3">
                <div className="flex-1 h-px" style={{ background: 'var(--border)' }} />
                <span className="text-[0.6rem] font-medium text-muted-foreground uppercase tracking-wider">
                  parallel
                </span>
                <div className="flex-1 h-px" style={{ background: 'var(--border)' }} />
              </div>
              <div className="grid gap-1.5" style={{ gridTemplateColumns: `repeat(${Math.min(waveSteps.length, 3)}, 1fr)` }}>
                {waveSteps.map((step) => (
                  <StepCard
                    key={step.name}
                    step={step}
                    isSelected={selectedStep === step.name}
                    onClick={() => onStepClick(step.name)}
                    compact={waveSteps.length > 2}
                  />
                ))}
              </div>
              <div className="flex items-center gap-2 px-3">
                <div className="flex-1 h-px" style={{ background: 'var(--border)' }} />
              </div>
            </div>
          )}
        </div>
      ))}
    </div>
  )
}

function StepCard({
  step,
  isSelected,
  onClick,
  compact,
}: {
  step: PipelineStep
  isSelected: boolean
  onClick: () => void
  compact?: boolean
}) {
  const duration = step.finishedAt && step.startedAt
    ? formatDuration(step.startedAt, step.finishedAt)
    : null

  return (
    <button
      type="button"
      onClick={onClick}
      className={`w-full flex items-center gap-3 rounded-lg border px-3 py-2.5 text-left transition-all group ${
        isSelected
          ? 'border-primary/40 bg-primary/8 ring-1 ring-primary/20'
          : 'border-border hover:border-primary/25 bg-card hover:bg-accent'
      }`}
    >
      <StatusIcon status={step.status} />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className={`font-medium text-foreground truncate ${compact ? 'text-xs' : 'text-sm'} ${
            isSelected ? 'text-primary' : 'group-hover:text-primary'
          } transition-colors`}>
            {step.name}
          </span>
          {step.execType === 'gate' && (
            <span className="text-[0.6rem] font-semibold uppercase tracking-wide text-warning px-1.5 py-0.5 rounded bg-warning/10 border border-warning/20">
              gate
            </span>
          )}
        </div>
        <div className="flex items-center gap-2 mt-0.5 text-[0.7rem] text-muted-foreground">
          {step.status === 'running' && (
            <span className="text-primary font-medium">Running...</span>
          )}
          {step.status === 'waiting' && (
            <span className="text-warning font-medium">Awaiting approval</span>
          )}
          {step.status === 'pending' && (
            <span>Pending</span>
          )}
          {duration && (
            <span className="flex items-center gap-1">
              <Timer size={10} />
              {duration}
            </span>
          )}
          {step.status === 'succeeded' && !compact && (
            <span className="text-success">Passed</span>
          )}
          {step.status === 'failed' && (
            <span className="text-destructive">Failed</span>
          )}
        </div>
      </div>
    </button>
  )
}

function StatusIcon({ status }: { status: PipelineStep['status'] }) {
  const size = 18
  switch (status) {
    case 'succeeded':
      return <CheckCircle size={size} className="text-success shrink-0" />
    case 'failed':
      return <XCircle size={size} className="text-destructive shrink-0" />
    case 'running':
      return <Loader2 size={size} className="text-primary shrink-0 animate-spin" />
    case 'waiting':
      return <Pause size={size} className="text-warning shrink-0" />
    case 'queued':
      return <Clock size={size} className="text-purple-400 shrink-0" />
    case 'skipped':
      return <SkipForward size={size} className="text-muted-foreground shrink-0" />
    case 'cancelled':
      return <Ban size={size} className="text-muted-foreground shrink-0" />
    default:
      return <Circle size={size} className="text-muted-foreground opacity-40 shrink-0" />
  }
}

function formatDuration(start: string, end: string): string {
  const ms = new Date(end).getTime() - new Date(start).getTime()
  const seconds = Math.floor(ms / 1000)
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}
