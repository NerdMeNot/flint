import {
  CheckCircle2,
  XCircle,
  Loader2,
  Pause,
  Clock,
  Ban,
  SkipForward,
  Circle,
  ShieldCheck,
  RotateCw,
  ChevronRight,
} from 'lucide-react'
import type { PipelineStep } from '#/lib/api/types'

interface StepSpineProps {
  steps: PipelineStep[]
  selectedStep: string | null
  onStepClick: (name: string) => void
  /** Ticking clock for live bars (running steps grow toward `now`). */
  now: number
}

/**
 * The canonical step list — navigator and at-a-glance duration chart in one.
 * Each started step carries a duration bar scaled to the **slowest** step (so
 * every bar stays legible regardless of the run's long pole), with a faint
 * leading segment for runner-queue wait. Parallel waves are bracketed; steps
 * that haven't begun stay visible (muted) so you can see what's still to come.
 */
export function StepSpine({ steps, selectedStep, onStepClick, now }: StepSpineProps) {
  const timed = steps
    .filter((s) => s.startedAt)
    .map((s) => {
      const startMs = Date.parse(s.startedAt!)
      const schedMs = s.scheduledAt ? Date.parse(s.scheduledAt) : startMs
      const endMs = s.finishedAt ? Date.parse(s.finishedAt) : now
      return { name: s.name, span: Math.max(0, endMs - schedMs), gate: s.execType === 'gate' }
    })
  // Scale bars to the slowest *executing* step — a human-approval gate is a
  // wait, not slow work, and would otherwise dwarf every real step. Gates clamp
  // to a full bar instead.
  const execSpans = timed.filter((t) => !t.gate).map((t) => t.span)
  const maxSpan = Math.max(1, ...(execSpans.length ? execSpans : timed.map((t) => t.span)))
  const spanOf = (name: string) => timed.find((t) => t.name === name)?.span ?? 0

  // Group consecutive steps by wave so parallel groups read as one block.
  const groups: { wave: number; steps: PipelineStep[] }[] = []
  for (const s of steps) {
    const g = groups[groups.length - 1]
    if (g && g.wave === s.wave) g.steps.push(s)
    else groups.push({ wave: s.wave, steps: [s] })
  }

  return (
    <div className="max-h-[calc(100vh-300px)] min-h-[280px] overflow-auto py-1.5">
      {groups.map((g) => {
        const parallel = g.steps.length > 1
        return (
          <div key={g.wave} className={parallel ? 'relative my-0.5 pl-2' : ''}>
            {parallel && (
              <>
                <span className="absolute left-2 top-2 bottom-2 w-px bg-border" />
                <span className="absolute left-1.5 top-1.5 text-[9px] font-semibold uppercase tracking-[0.12em] text-muted-foreground/40 rotate-180 [writing-mode:vertical-rl]">
                  ∥
                </span>
              </>
            )}
            <div className={parallel ? 'pl-2' : ''}>
              {g.steps.map((s) => (
                <SpineRow
                  key={s.name}
                  step={s}
                  selected={s.name === selectedStep}
                  onClick={() => onStepClick(s.name)}
                  now={now}
                  span={spanOf(s.name)}
                  maxSpan={maxSpan}
                />
              ))}
            </div>
          </div>
        )
      })}
    </div>
  )
}

function SpineRow({
  step, selected, onClick, now, span, maxSpan,
}: {
  step: PipelineStep
  selected: boolean
  onClick: () => void
  now: number
  span: number
  maxSpan: number
}) {
  const started = !!step.startedAt
  const isRunning = step.status === 'running'
  const isWaiting = step.status === 'waiting'
  const isGate = step.execType === 'gate'
  const retried = step.attempt > 1

  const startMs = started ? Date.parse(step.startedAt!) : 0
  const schedMs = step.scheduledAt ? Date.parse(step.scheduledAt) : startMs
  const endMs = step.finishedAt ? Date.parse(step.finishedAt) : now
  const waitMs = Math.max(0, startMs - schedMs)
  const runMs = Math.max(0, endMs - startMs)

  const fillPct = span > 0 ? (span / maxSpan) * 100 : 0
  const queuePct = span > 0 ? (waitMs / span) * 100 : 0

  const durLabel = isWaiting
    ? 'waiting'
    : started
      ? fmtDur(runMs)
      : step.status === 'skipped'
        ? 'skipped'
        : 'pending'

  const Row = started ? 'button' : 'div'

  return (
    <Row
      {...(started ? { type: 'button' as const, onClick } : {})}
      title={
        started
          ? [
              step.name,
              waitMs >= 500 ? `Queued ${fmtDur(waitMs)} (runner wait)` : null,
              isRunning ? `Running… ${fmtDur(runMs)}` : isWaiting ? 'Awaiting approval' : `Ran ${fmtDur(runMs)}`,
              retried ? `Attempt ${step.attempt} of ${step.maxAttempts}` : null,
            ].filter(Boolean).join('\n')
          : `${step.name} — ${step.status}`
      }
      className={`group grid w-full grid-cols-[1fr_auto] items-center gap-2 border-l-2 pl-2.5 pr-2.5 py-2 text-left transition-colors ${
        selected
          ? 'border-primary bg-primary/[0.07]'
          : started
            ? 'border-transparent hover:border-border hover:bg-accent/50'
            : 'border-transparent opacity-55'
      }`}
    >
      {/* Name + status */}
      <span className="flex items-center gap-2 min-w-0">
        <StatusGlyph status={step.status} />
        {isGate && <ShieldCheck size={12} className="text-warning shrink-0" />}
        <span className={`text-[13px] truncate ${selected ? 'font-semibold text-foreground' : 'font-medium text-foreground/90'} ${started ? 'group-hover:text-foreground' : ''}`}>
          {step.name}
        </span>
        {retried && (
          <span className="shrink-0 inline-flex items-center gap-0.5 rounded-full bg-warning/15 px-1 text-[9px] font-mono font-semibold text-warning">
            <RotateCw size={8} />{step.attempt}
          </span>
        )}
      </span>

      {/* Duration + relative bar */}
      <span className="flex items-center gap-2 shrink-0">
        <span className={`w-12 text-right text-[11px] font-mono tabular-nums ${
          isRunning || isWaiting ? 'text-primary' : started ? 'text-muted-foreground' : 'text-muted-foreground/50'
        }`}>
          {durLabel}
        </span>
        <span className="hidden sm:block w-16 lg:w-20 h-1.5 rounded-full bg-muted/50 overflow-hidden">
          {started && (
            <span className="flex h-full rounded-full" style={{ width: `${Math.min(Math.max(fillPct, 3), 100)}%` }}>
              {queuePct > 1 && (
                <span
                  className="h-full shrink-0"
                  style={{
                    width: `${queuePct}%`,
                    backgroundImage:
                      'repeating-linear-gradient(45deg, color-mix(in oklab, var(--color-foreground) 18%, transparent) 0 1.5px, transparent 1.5px 5px)',
                  }}
                />
              )}
              <span className={`h-full flex-1 ${barColor(step.status, isGate)} ${isRunning ? 'animate-pulse' : ''}`} />
            </span>
          )}
        </span>
        <ChevronRight size={13} className={`shrink-0 transition-opacity ${selected ? 'text-primary opacity-100' : 'text-muted-foreground opacity-0 group-hover:opacity-50'} ${started ? '' : 'invisible'}`} />
      </span>
    </Row>
  )
}

function StatusGlyph({ status }: { status: PipelineStep['status'] }) {
  const c = 'shrink-0'
  switch (status) {
    case 'succeeded': return <CheckCircle2 size={15} className={`${c} text-success`} />
    case 'failed': return <XCircle size={15} className={`${c} text-destructive`} />
    case 'running': return <Loader2 size={15} className={`${c} text-primary animate-spin`} />
    case 'waiting': return <Pause size={15} className={`${c} text-warning`} />
    case 'queued': return <Clock size={15} className={`${c} text-purple-400`} />
    case 'skipped': return <SkipForward size={15} className={`${c} text-muted-foreground/50`} />
    case 'cancelled': return <Ban size={15} className={`${c} text-muted-foreground/50`} />
    default: return <Circle size={15} className={`${c} text-muted-foreground/40`} strokeDasharray="2 2" />
  }
}

function barColor(status: PipelineStep['status'], isGate: boolean): string {
  if (isGate && (status === 'waiting' || status === 'pending')) return 'bg-warning'
  switch (status) {
    case 'succeeded': return 'bg-success'
    case 'failed': return 'bg-destructive'
    case 'running': return 'bg-primary'
    case 'waiting': return 'bg-warning'
    case 'cancelled': return 'bg-muted-foreground/40'
    case 'skipped': return 'bg-muted-foreground/25'
    case 'queued': return 'bg-purple-400/60'
    default: return 'bg-muted-foreground/40'
  }
}

export function fmtDur(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return s % 60 === 0 ? `${m}m` : `${m}m ${s % 60}s`
  const h = Math.floor(m / 60)
  return `${h}h ${m % 60}m`
}
