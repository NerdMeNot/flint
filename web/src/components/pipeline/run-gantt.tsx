import { useEffect, useState } from 'react'
import { Activity, ShieldCheck, Hourglass, RotateCw } from 'lucide-react'
import { formatClock } from '#/lib/format-time'
import type { PipelineStep } from '#/lib/api/types'

interface RunGanttProps {
  steps: PipelineStep[]
  selectedStep: string | null
  onStepClick: (name: string) => void
  /** Optional content rendered in the top-right of the card header (e.g. view toggle). */
  toolbar?: React.ReactNode
}

// Column widths are driven by CSS vars so the axis, gridlines, and every row
// share one coordinate system — that alignment is what makes it read as a
// chart rather than a stack of boxes.
const TRACK_VARS =
  '[--gantt-name:108px] sm:[--gantt-name:140px] lg:[--gantt-name:168px] [--gantt-meta:52px] sm:[--gantt-meta:64px]'
const TRACK_COLS = 'grid-cols-[var(--gantt-name)_minmax(0,1fr)_var(--gantt-meta)]'

/**
 * Gantt-style timeline. Each started step is a bar positioned across the run's
 * elapsed span, split into a faint **queue** segment (scheduled → started:
 * runner / pod cold-start wait) and a solid **run** segment (started →
 * finished). Retried steps are striped, and hovering a bar reveals its offsets
 * from run start. Steps that haven't begun sit in a small "pending" footer.
 */
export function RunGantt({ steps, selectedStep, onStepClick, toolbar }: RunGanttProps) {
  // Tick "now" once per second while anything is still running so the
  // running bars grow live.
  const isLive = steps.some((s) => s.status === 'running' || s.status === 'waiting')
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!isLive) return
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [isLive])

  const startedSteps = steps.filter((s) => !!s.startedAt)
  const pendingSteps = steps.filter((s) => !s.startedAt && s.status !== 'cancelled' && s.status !== 'skipped')

  if (startedSteps.length === 0) {
    return (
      <div className="island-shell !p-0 overflow-hidden">
        <PanelHeader icon={<Activity size={14} className="text-primary" />} title="Timeline" toolbar={toolbar} />
        <div className="flex flex-col items-center justify-center gap-2 py-16 text-center text-muted-foreground">
          <Hourglass size={28} strokeWidth={1.4} className="opacity-50" />
          <p className="text-sm">Run hasn't started yet.</p>
          <p className="text-xs opacity-70">Steps will appear here as they begin.</p>
        </div>
      </div>
    )
  }

  // Per-step times: scheduled (queue start) → started → ended (or live now).
  const rows = startedSteps
    .map((step) => {
      const startMs = new Date(step.startedAt!).getTime()
      const schedMs = step.scheduledAt ? new Date(step.scheduledAt).getTime() : startMs
      const endMs = step.finishedAt ? new Date(step.finishedAt).getTime() : now
      return { step, schedMs, startMs, endMs }
    })
    .sort((a, b) => a.schedMs - b.schedMs || a.step.wave - b.step.wave)

  const runStart = Math.min(...rows.map((r) => r.schedMs))
  const runEnd = Math.max(...rows.map((r) => r.endMs), isLive ? now : runStart)
  const totalMs = Math.max(runEnd - runStart, 1000)

  const totalWait = rows.reduce((sum, r) => sum + Math.max(0, r.startMs - r.schedMs), 0)
  const anyWait = totalWait >= 1000

  return (
    <div className="island-shell !p-0 overflow-hidden">
      <PanelHeader
        icon={<Activity size={14} className="text-primary" />}
        title="Timeline"
        subtitle={
          <>
            {rows.length} step{rows.length === 1 ? '' : 's'}
            {isLive && <span className="text-primary font-medium ml-1.5">· live</span>}
            <span className="ml-2 font-mono opacity-70">{formatMs(totalMs)}</span>
            {anyWait && <span className="ml-2 font-mono opacity-50">· {formatMs(totalWait)} queued</span>}
          </>
        }
        toolbar={toolbar}
      />

      <div className={`px-3 sm:px-4 lg:px-5 py-3 lg:py-4 ${TRACK_VARS}`}>
        <TimeAxis totalMs={totalMs} showQueueLegend={anyWait} />

        <div className="relative mt-2.5">
          {/* Continuous gridlines behind every row — aligned to the plot column. */}
          <div className="pointer-events-none absolute inset-y-0" style={{ left: 'var(--gantt-name)', right: 'var(--gantt-meta)' }}>
            {[0, 25, 50, 75, 100].map((p) => (
              <span
                key={p}
                className={`absolute top-0 bottom-0 w-px ${p === 0 || p === 100 ? 'bg-border/40' : 'bg-border/20'}`}
                style={{ left: `${p}%` }}
              />
            ))}
          </div>

          <div>
            {rows.map((r) => (
              <GanttRow
                key={r.step.name}
                row={r}
                runStart={runStart}
                totalMs={totalMs}
                isSelected={r.step.name === selectedStep}
                onClick={() => onStepClick(r.step.name)}
              />
            ))}
          </div>
        </div>

        {pendingSteps.length > 0 && (
          <div className="mt-3 pt-3 border-t border-border/50 flex items-center gap-2 flex-wrap">
            <span className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground/60">Pending</span>
            {pendingSteps.map((s) => (
              <span
                key={s.name}
                title={`${s.name} — hasn't started yet`}
                className="inline-flex items-center gap-1.5 rounded-full border border-dashed border-border px-2 py-0.5 text-xs font-medium text-muted-foreground/60"
              >
                <span className="w-1.5 h-1.5 rounded-full bg-muted-foreground opacity-30" />
                {s.name}
              </span>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// One Gantt row
// ---------------------------------------------------------------------------

interface Row {
  step: PipelineStep
  schedMs: number
  startMs: number
  endMs: number
}

function GanttRow({
  row, runStart, totalMs, isSelected, onClick,
}: {
  row: Row
  runStart: number
  totalMs: number
  isSelected: boolean
  onClick: () => void
}) {
  const { step, schedMs, startMs, endMs } = row
  const isRunning = !step.finishedAt && (step.status === 'running' || step.status === 'waiting')
  const isGate = step.execType === 'gate'
  const waitMs = Math.max(0, startMs - schedMs)
  const runMs = Math.max(0, endMs - startMs)
  const retried = step.attempt > 1

  // Bar geometry: the whole bar spans scheduled→end; the queue portion is the
  // leading fraction up to "started".
  const leftPct = ((schedMs - runStart) / totalMs) * 100
  const spanPct = Math.max(((endMs - schedMs) / totalMs) * 100, 1.2)
  const waitFracPct = endMs > schedMs ? (waitMs / (endMs - schedMs)) * 100 : 0

  const tip = [
    step.name,
    waitMs >= 500 ? `Queued  ${formatMs(waitMs)}  (runner wait)` : null,
    `Started  +${formatMs(startMs - runStart)}  ·  ${formatClock(startMs)}`,
    step.finishedAt ? `Ran  ${formatMs(runMs)}` : isRunning ? `Running…  ${formatMs(runMs)}` : null,
    retried ? `Attempt ${step.attempt} of ${step.maxAttempts}` : null,
  ].filter(Boolean).join('\n')

  return (
    <button
      type="button"
      onClick={onClick}
      title={tip}
      className={`group relative grid items-center ${TRACK_COLS} h-8 rounded-md text-left transition-colors ${
        isSelected ? 'bg-primary/[0.07] ring-1 ring-inset ring-primary/20' : 'hover:bg-accent/60'
      }`}
    >
      {/* Name + status dot + retry badge */}
      <div className="pr-2 sm:pr-3 flex items-center gap-1.5 min-w-0">
        <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${dotClass(step.status, isGate)} ${isRunning ? 'animate-pulse' : ''}`} />
        {isGate && <ShieldCheck size={11} className="text-warning shrink-0" />}
        <span className={`text-xs font-medium truncate transition-colors ${isSelected ? 'text-primary' : 'text-foreground'} group-hover:text-primary`}>
          {step.name}
        </span>
        {retried && (
          <span className="shrink-0 inline-flex items-center gap-0.5 rounded-full bg-warning/15 px-1 text-[9px] font-mono font-semibold text-warning" title={`Retried — attempt ${step.attempt}/${step.maxAttempts}`}>
            <RotateCw size={8} />{step.attempt}
          </span>
        )}
      </div>

      {/* Plot */}
      <div className="relative h-full">
        <div
          className="absolute top-1/2 -translate-y-1/2 h-2.5 flex rounded-full overflow-hidden ring-1 ring-inset ring-black/5 dark:ring-white/5"
          style={{ left: `${leftPct}%`, width: `${spanPct}%` }}
        >
          {/* Queue / wait segment */}
          {waitFracPct > 0.5 && (
            <span
              className="h-full shrink-0 bg-foreground/[0.09]"
              style={{
                width: `${waitFracPct}%`,
                backgroundImage:
                  'repeating-linear-gradient(45deg, color-mix(in oklab, var(--color-foreground) 14%, transparent) 0 1.5px, transparent 1.5px 5px)',
              }}
            />
          )}
          {/* Run segment */}
          <span className={`relative h-full flex-1 ${barClass(step.status, isGate)}`}>
            {retried && (
              <span
                className="absolute inset-0"
                style={{ backgroundImage: 'repeating-linear-gradient(45deg, rgba(0,0,0,0.22) 0 3px, transparent 3px 7px)' }}
              />
            )}
            {isRunning && <span className="absolute inset-0 running-shimmer" />}
          </span>
        </div>
      </div>

      {/* Run duration */}
      <span className={`pl-2 sm:pl-3 text-right text-[11px] font-mono tabular-nums whitespace-nowrap ${isRunning ? 'text-primary' : 'text-muted-foreground'}`}>
        {step.finishedAt || isRunning ? formatMs(runMs) : '—'}
      </span>
    </button>
  )
}

function TimeAxis({ totalMs, showQueueLegend }: { totalMs: number; showQueueLegend: boolean }) {
  return (
    <div>
      <div className={`grid items-center ${TRACK_COLS} text-[10px] sm:text-[11px] text-muted-foreground/60`}>
        <span className="pr-2 sm:pr-3 truncate">{showQueueLegend && <Legend />}</span>
        <span className="flex justify-between font-mono tabular-nums">
          <span>0:00</span>
          <span className="hidden sm:inline">{formatMs(totalMs * 0.25)}</span>
          <span>{formatMs(totalMs * 0.5)}</span>
          <span className="hidden sm:inline">{formatMs(totalMs * 0.75)}</span>
          <span>{formatMs(totalMs)}</span>
        </span>
        <span />
      </div>
    </div>
  )
}

function Legend() {
  return (
    <span className="inline-flex items-center gap-2.5">
      <span className="inline-flex items-center gap-1">
        <span
          className="h-2 w-3 rounded-full bg-foreground/10"
          style={{ backgroundImage: 'repeating-linear-gradient(45deg, color-mix(in oklab, var(--color-foreground) 14%, transparent) 0 1.5px, transparent 1.5px 5px)' }}
        />
        queue
      </span>
      <span className="inline-flex items-center gap-1">
        <span className="h-2 w-3 rounded-full bg-muted-foreground/50" />
        run
      </span>
    </span>
  )
}

// ---------------------------------------------------------------------------
// Shared panel header — used by Timeline, DAG, and gate cards so they share
// the same top-edge alignment and aesthetic.
// ---------------------------------------------------------------------------

export function PanelHeader({
  icon, title, subtitle, toolbar,
}: {
  icon: React.ReactNode
  title: string
  subtitle?: React.ReactNode
  toolbar?: React.ReactNode
}) {
  return (
    <div className="flex items-center justify-between gap-3 px-4 lg:px-5 py-3 border-b border-border min-h-[48px]">
      <div className="flex items-center gap-2 min-w-0">
        {icon}
        <span className="text-sm lg:text-base font-semibold text-foreground">{title}</span>
        {subtitle && <span className="text-xs text-muted-foreground truncate">{subtitle}</span>}
      </div>
      {toolbar && <div className="shrink-0">{toolbar}</div>}
    </div>
  )
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

function barClass(status: PipelineStep['status'], isGate: boolean): string {
  if (isGate && (status === 'waiting' || status === 'pending')) return 'bg-warning/70'
  switch (status) {
    case 'succeeded': return 'bg-success'
    case 'failed': return 'bg-destructive'
    case 'running': return 'bg-primary shadow-[0_0_8px_-1px] shadow-primary/50'
    case 'waiting': return 'bg-warning'
    case 'cancelled': return 'bg-muted-foreground/40'
    case 'skipped': return 'bg-muted-foreground/25'
    case 'queued': return 'bg-purple-400/60'
    default: return 'bg-muted-foreground/40'
  }
}

function dotClass(status: PipelineStep['status'], isGate: boolean): string {
  if (isGate && (status === 'waiting' || status === 'pending')) return 'bg-warning'
  switch (status) {
    case 'succeeded': return 'bg-success'
    case 'failed': return 'bg-destructive'
    case 'running': return 'bg-primary'
    case 'waiting': return 'bg-warning'
    case 'queued': return 'bg-purple-400'
    default: return 'bg-muted-foreground/40'
  }
}

function formatMs(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`
  const totalSeconds = Math.floor(ms / 1000)
  if (totalSeconds < 60) return `${totalSeconds}s`
  const m = Math.floor(totalSeconds / 60)
  const s = totalSeconds % 60
  if (m < 60) return s === 0 ? `${m}m` : `${m}m ${s}s`
  const h = Math.floor(m / 60)
  return `${h}h ${m % 60}m`
}
