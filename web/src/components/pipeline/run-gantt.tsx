import { useEffect, useState } from 'react'
import { Activity, ShieldCheck, Hourglass } from 'lucide-react'
import type { PipelineStep } from '#/lib/api/types'

interface RunGanttProps {
  steps: PipelineStep[]
  selectedStep: string | null
  onStepClick: (name: string) => void
  /** Optional content rendered in the top-right of the card header (e.g. view toggle). */
  toolbar?: React.ReactNode
}

/**
 * Gantt-style timeline. Each step that has begun is rendered as a bar
 * positioned by `startedAt..finishedAt` relative to the run's total
 * duration. Steps still pending appear in a small "queued" footer so
 * the eye can spot them without forcing fake placeholders into the bars.
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
        <PanelHeader
          icon={<Activity size={14} className="text-primary" />}
          title="Timeline"
          toolbar={toolbar}
        />
        <div className="flex flex-col items-center justify-center gap-2 py-16 text-center text-muted-foreground">
          <Hourglass size={28} strokeWidth={1.4} className="opacity-50" />
          <p className="text-sm">Run hasn't started yet.</p>
          <p className="text-xs opacity-70">Steps will appear here as they begin.</p>
        </div>
      </div>
    )
  }

  const startTimes = startedSteps.map((s) => new Date(s.startedAt!).getTime())
  const endTimes = startedSteps.map((s) =>
    s.finishedAt ? new Date(s.finishedAt).getTime() : now,
  )
  const runStart = Math.min(...startTimes)
  const runEnd = Math.max(...endTimes, isLive ? now : runStart)
  // Clamp to at least 1s so bars never collapse to zero width.
  const totalMs = Math.max(runEnd - runStart, 1000)

  // Build wave groupings preserving step order so we can stripe waves
  // visually (subtle row-zebra) and label parallel groups.
  const orderedSteps = [...startedSteps].sort((a, b) => {
    const aStart = new Date(a.startedAt!).getTime()
    const bStart = new Date(b.startedAt!).getTime()
    if (aStart !== bStart) return aStart - bStart
    return a.wave - b.wave
  })

  return (
    <div className="island-shell !p-0 overflow-hidden">
      <PanelHeader
        icon={<Activity size={14} className="text-primary" />}
        title="Timeline"
        subtitle={
          <>
            {orderedSteps.length} step{orderedSteps.length === 1 ? '' : 's'}
            {isLive && <span className="text-primary font-medium ml-1.5">· live</span>}
            <span className="ml-2 font-mono opacity-70">{formatMs(totalMs)} total</span>
          </>
        }
        toolbar={toolbar}
      />

      <div className="px-3 sm:px-4 lg:px-5 py-3 lg:py-4">
        <TimeAxis totalMs={totalMs} />
        <div className="space-y-0.5 lg:space-y-1 mt-2">
          {orderedSteps.map((step) => (
            <GanttRow
              key={step.name}
              step={step}
              runStart={runStart}
              totalMs={totalMs}
              now={now}
              isSelected={step.name === selectedStep}
              onClick={() => onStepClick(step.name)}
            />
          ))}
        </div>

        {pendingSteps.length > 0 && (
          <div className="mt-3 pt-3 border-t border-border/50 flex items-center gap-2 flex-wrap">
            <span className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground/60">
              Pending
            </span>
            {pendingSteps.map((s) => (
              <span
                key={s.name}
                title={`${s.name} — hasn't started yet`}
                className="inline-flex items-center gap-1.5 rounded-md border border-dashed border-border px-2 py-0.5 text-xs font-medium text-muted-foreground/60"
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

const NAME_COL = 'w-[110px] sm:w-[140px] lg:w-[160px]'
const META_COL = 'w-[58px] sm:w-[68px]'

function GanttRow({
  step, runStart, totalMs, now, isSelected, onClick,
}: {
  step: PipelineStep
  runStart: number
  totalMs: number
  now: number
  isSelected: boolean
  onClick: () => void
}) {
  const startMs = new Date(step.startedAt!).getTime()
  const endMs = step.finishedAt ? new Date(step.finishedAt).getTime() : now
  const isRunning = !step.finishedAt && (step.status === 'running' || step.status === 'waiting')
  const leftPct = ((startMs - runStart) / totalMs) * 100
  const widthPct = Math.max(((endMs - startMs) / totalMs) * 100, 1.5)
  const isGate = step.execType === 'gate'

  return (
    <button
      type="button"
      onClick={onClick}
      className={`group w-full flex items-center gap-2 sm:gap-3 rounded-md px-1.5 py-1.5 text-left transition-colors ${
        isSelected
          ? 'bg-primary/8 ring-1 ring-primary/20'
          : 'hover:bg-accent'
      }`}
    >
      <div className={`${NAME_COL} shrink-0 flex items-center gap-1.5 min-w-0`}>
        {isGate && <ShieldCheck size={11} className="text-warning shrink-0" />}
        <span className={`text-xs font-medium truncate ${
          isSelected ? 'text-primary' : 'text-foreground'
        } group-hover:text-primary transition-colors`}>
          {step.name}
        </span>
      </div>

      <div className="flex-1 relative h-5 sm:h-6 rounded bg-muted/40 overflow-hidden">
        <div
          className={`absolute top-0 bottom-0 rounded transition-[width] duration-500 ease-out ${barClasses(step.status, isGate)}`}
          style={{ left: `${leftPct}%`, width: `${widthPct}%` }}
        >
          {isRunning && <span className="absolute inset-0 running-shimmer" />}
        </div>
      </div>

      <span className={`${META_COL} shrink-0 text-right text-[11px] font-mono whitespace-nowrap ${
        isRunning ? 'text-primary' : 'text-muted-foreground'
      }`}>
        {formatMs(endMs - startMs)}
      </span>
    </button>
  )
}

function TimeAxis({ totalMs }: { totalMs: number }) {
  return (
    <div className={`flex items-center gap-2 sm:gap-3 text-[10px] sm:text-[11px] text-muted-foreground/60`}>
      <div className={`${NAME_COL} shrink-0`} />
      <div className="flex-1 flex justify-between font-mono">
        <span>0:00</span>
        <span className="hidden sm:inline">{formatMs(totalMs * 0.25)}</span>
        <span>{formatMs(totalMs * 0.5)}</span>
        <span className="hidden sm:inline">{formatMs(totalMs * 0.75)}</span>
        <span>{formatMs(totalMs)}</span>
      </div>
      <div className={`${META_COL} shrink-0`} />
    </div>
  )
}

// ---------------------------------------------------------------------------
// Shared panel header — used by Timeline and DAG cards so they share
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
        {subtitle && (
          <span className="text-xs text-muted-foreground truncate">{subtitle}</span>
        )}
      </div>
      {toolbar && <div className="shrink-0">{toolbar}</div>}
    </div>
  )
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

function barClasses(status: PipelineStep['status'], isGate: boolean): string {
  if (isGate && status === 'pending') return 'bg-warning/30 border border-dashed border-warning/60'
  switch (status) {
    case 'succeeded': return 'bg-success/80'
    case 'failed': return 'bg-destructive/80'
    case 'running': return 'bg-primary/70'
    case 'waiting': return 'bg-warning/70'
    case 'cancelled': return 'bg-muted-foreground/30'
    case 'skipped': return 'bg-muted-foreground/20'
    case 'queued': return 'bg-purple-400/50'
    default: return 'bg-muted-foreground/30'
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
