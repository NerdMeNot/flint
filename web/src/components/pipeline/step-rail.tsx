import {
  CheckCircle,
  XCircle,
  Loader2,
  Clock,
  Pause,
  SkipForward,
  Ban,
  Circle,
  ShieldCheck,
  Timer,
} from 'lucide-react'
import type { PipelineStep } from '#/lib/api/types'

interface StepRailProps {
  steps: PipelineStep[]
  selectedStep: string | null
  onStepClick: (name: string) => void
}

/**
 * Step rail. On lg+ this renders a sticky vertical list grouped by wave
 * with a thin "parallel" bracket between concurrent steps. Below lg it
 * collapses into a single-row horizontal pill strip that scrolls
 * sideways — same data, just resorted for the available width.
 */
export function StepRail({ steps, selectedStep, onStepClick }: StepRailProps) {
  return (
    <>
      {/* Mobile / tablet: horizontal pill strip */}
      <div className="lg:hidden flex items-center gap-1.5 overflow-x-auto -mx-1 px-1 pb-2 scroll-px-1">
        {steps.map((s) => (
          <StepPill
            key={s.name}
            step={s}
            isSelected={s.name === selectedStep}
            onClick={() => onStepClick(s.name)}
          />
        ))}
      </div>

      {/* Desktop: vertical rail */}
      <div className="hidden lg:block">
        <VerticalRail
          steps={steps}
          selectedStep={selectedStep}
          onStepClick={onStepClick}
        />
      </div>
    </>
  )
}

// ---------------------------------------------------------------------------
// Mobile pill — compact, horizontally scrollable
// ---------------------------------------------------------------------------

function StepPill({
  step, isSelected, onClick,
}: {
  step: PipelineStep
  isSelected: boolean
  onClick: () => void
}) {
  const isGate = step.execType === 'gate'
  const hasLogs = !!step.startedAt
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={!hasLogs}
      title={hasLogs ? `${step.name} — ${step.status}` : `${step.name} — not started yet`}
      aria-disabled={!hasLogs}
      className={`flex items-center gap-1.5 shrink-0 rounded-md px-2.5 py-1.5 text-xs font-medium transition-all ${
        isSelected
          ? 'bg-primary/10 text-primary ring-1 ring-primary/25'
          : hasLogs
            ? 'text-muted-foreground hover:text-foreground hover:bg-accent'
            : 'text-muted-foreground/50 cursor-default'
      }`}
    >
      <StepGlyph status={step.status} compact />
      {isGate && <ShieldCheck size={10} className="text-warning shrink-0" />}
      <span className="whitespace-nowrap">{step.name}</span>
    </button>
  )
}

// ---------------------------------------------------------------------------
// Desktop vertical rail
// ---------------------------------------------------------------------------

function VerticalRail({
  steps, selectedStep, onStepClick,
}: StepRailProps) {
  // Group by wave for parallel labels.
  const waves = new Map<number, PipelineStep[]>()
  for (const s of steps) {
    const list = waves.get(s.wave) ?? []
    list.push(s)
    waves.set(s.wave, list)
  }
  const sortedWaves = [...waves.entries()].sort((a, b) => a[0] - b[0])

  return (
    <div className="island-shell !p-0 overflow-hidden">
      {/* Header — matches PanelHeader in run-gantt for top-edge alignment with the right-side card. */}
      <div className="flex items-center justify-between gap-3 px-4 lg:px-5 py-3 border-b border-border min-h-[48px]">
        <span className="text-sm lg:text-base font-semibold text-foreground">Steps</span>
        <span className="text-xs text-muted-foreground font-mono">{steps.length}</span>
      </div>
      <div className="px-2 py-2 space-y-0.5 max-h-[calc(100vh-280px)] overflow-y-auto">
        {sortedWaves.map(([wave, waveSteps], wi) => (
          <div key={wave}>
            {wi > 0 && (
              <div className="flex items-center gap-2 px-2 py-1.5 my-0.5">
                <div className="flex-1 h-px bg-border/60" />
                <span className="text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground/40">
                  then
                </span>
                <div className="flex-1 h-px bg-border/60" />
              </div>
            )}

            {waveSteps.length === 1 ? (
              <RailRow
                step={waveSteps[0]!}
                isSelected={waveSteps[0]!.name === selectedStep}
                onClick={() => onStepClick(waveSteps[0]!.name)}
              />
            ) : (
              <div className="relative pl-3">
                {/* Parallel bracket */}
                <div className="absolute left-0 top-1.5 bottom-1.5 w-px bg-primary/25" />
                <span className="absolute -left-px top-1.5 w-1.5 h-1.5 rounded-full bg-primary/40" />
                <span className="absolute -left-px bottom-1.5 w-1.5 h-1.5 rounded-full bg-primary/40" />
                <div className="space-y-0.5">
                  {waveSteps.map((s) => (
                    <RailRow
                      key={s.name}
                      step={s}
                      isSelected={s.name === selectedStep}
                      onClick={() => onStepClick(s.name)}
                      parallel
                    />
                  ))}
                </div>
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}

function RailRow({
  step, isSelected, onClick, parallel,
}: {
  step: PipelineStep
  isSelected: boolean
  onClick: () => void
  parallel?: boolean
}) {
  const isGate = step.execType === 'gate'
  const hasLogs = !!step.startedAt
  const duration = step.finishedAt && step.startedAt
    ? formatDuration(step.startedAt, step.finishedAt)
    : null

  return (
    <button
      type="button"
      onClick={onClick}
      disabled={!hasLogs}
      aria-disabled={!hasLogs}
      title={hasLogs ? undefined : `${step.name} — hasn't started yet`}
      className={`group w-full flex items-center gap-2.5 rounded-md px-2 py-1.5 text-left transition-colors ${
        isSelected
          ? 'bg-primary/8 ring-1 ring-primary/25'
          : hasLogs
            ? 'hover:bg-accent'
            : 'cursor-default opacity-60'
      }`}
    >
      <StepGlyph status={step.status} />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5">
          {isGate && <ShieldCheck size={11} className="text-warning shrink-0" />}
          <span className={`text-sm font-medium truncate ${
            isSelected ? 'text-primary' : 'text-foreground'
          } ${hasLogs ? 'group-hover:text-primary' : ''} transition-colors`}>
            {step.name}
          </span>
        </div>
        {(duration || step.status === 'running' || step.status === 'waiting' || parallel) && (
          <div className="flex items-center gap-1.5 mt-0.5 text-[11px] text-muted-foreground">
            {step.status === 'running' && (
              <span className="text-primary font-medium">Running…</span>
            )}
            {step.status === 'waiting' && (
              <span className="text-warning font-medium">Awaiting approval</span>
            )}
            {step.status === 'pending' && parallel && <span>Pending</span>}
            {duration && (
              <span className="flex items-center gap-1 font-mono">
                <Timer size={9} />
                {duration}
              </span>
            )}
          </div>
        )}
      </div>
    </button>
  )
}

// ---------------------------------------------------------------------------
// Status glyph used by both pill + rail row
// ---------------------------------------------------------------------------

function StepGlyph({ status, compact }: { status: PipelineStep['status']; compact?: boolean }) {
  const size = compact ? 12 : 14
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
