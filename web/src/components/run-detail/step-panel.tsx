import { useState, useEffect } from 'react'
import { CheckCircle, XCircle, Loader2, Terminal, Timer, RotateCcw, ChevronLeft, ChevronRight, Maximize2, Minimize2, X, ArrowRight, History } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { LogView } from '#/components/pipeline/log-view'
import { StepTimelinePanel } from '#/components/pipeline/step-timeline-panel'
import type { StepLogLine } from '#/lib/api/types'
import { StatusBadge, StatusIcon, formatDuration } from '#/components/run-detail/run-status'

export function LogPanel({
  step, steps, runId, runStatus, entries, live, maximized, onToggleMaximize, onSelectStep, onBackToOverview,
}: {
  step: any
  steps: any[]
  runId: string
  runStatus: string
  entries: StepLogLine[] | null
  live: boolean
  maximized: boolean
  onToggleMaximize: () => void
  onSelectStep: (name: string) => void
  onBackToOverview: () => void
}) {
  // Output is the default tab; Timeline shows the step's dispatch lifecycle
  // (queued → assigned machine → started → finished). Switching steps snaps
  // back to Output so ←/→ browsing always lands on logs.
  const [tab, setTab] = useState<'output' | 'timeline'>('output')
  useEffect(() => {
    setTab('output')
  }, [step.name])

  const idx = steps.findIndex((s) => s.name === step.name)
  const prev = idx > 0 ? steps[idx - 1] : null
  const next = idx < steps.length - 1 ? steps[idx + 1] : null

  // Keyboard nav while logs are open: ←/→ step through siblings, Esc
  // exits maximize first then closes the panel. We skip when the user
  // is typing in a form field so palette typing isn't hijacked.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const t = e.target as HTMLElement | null
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return
      if (e.key === 'ArrowLeft' && prev) {
        e.preventDefault()
        onSelectStep(prev.name)
      } else if (e.key === 'ArrowRight' && next) {
        e.preventDefault()
        onSelectStep(next.name)
      } else if (e.key === 'Escape' && !maximized) {
        // When maximized the parent's Esc handler runs first to drop
        // the overlay; this branch closes the panel only when already
        // not maximized.
        e.preventDefault()
        onBackToOverview()
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [prev, next, onSelectStep, onBackToOverview, maximized])

  // When maximized, the panel takes over the viewport. We use a fixed
  // overlay (still inside the React tree, no portal) so its state stays
  // co-located with the rest of the run page.
  const containerClass = maximized
    ? 'fixed inset-0 z-50 flex flex-col'
    : 'island-shell !p-0 overflow-hidden flex flex-col'

  const containerStyle = maximized
    ? { background: 'var(--background)' }
    : undefined

  const logBodyClass = maximized
    ? 'flex-1 bg-[#0d1117] p-4 sm:p-5 lg:p-6 font-mono text-[13px] lg:text-[14px] leading-[1.65] text-[#c9d1d9] overflow-auto'
    : 'bg-[#0d1117] p-4 sm:p-5 lg:p-6 font-mono text-[13px] lg:text-[14px] leading-[1.65] text-[#c9d1d9] min-h-[400px] sm:min-h-[500px] max-h-[75vh] overflow-auto'

  // Header bar grows in maximized mode so the close affordances are
  // unmistakable. Buttons get explicit text labels at sm+ to make the
  // exit path obvious.
  const headerClass = maximized
    ? 'flex items-center justify-between gap-3 px-4 sm:px-5 lg:px-6 py-3.5 border-b border-border shrink-0 min-w-0'
    : 'flex items-center justify-between gap-3 px-3 sm:px-4 py-3 border-b border-border shrink-0 min-w-0'
  const navBtnSize = maximized ? 'w-9 h-9' : 'w-7 h-7'
  const iconBtnSize = maximized ? 14 : 13
  const chevronSize = maximized ? 16 : 14

  return (
    <div className={containerClass} style={containerStyle}>
      <div className={headerClass}>
        <div className="flex items-center gap-2 sm:gap-3 min-w-0">
          <StatusIcon status={step.status} size={maximized ? 18 : 16} />
          <span className={`font-semibold ${maximized ? 'text-base lg:text-lg' : 'text-sm sm:text-base'} text-foreground truncate`}>
            {step.name}
          </span>
          <StatusBadge status={step.status} />
          {step.finishedAt && step.startedAt && (
            <span className="hidden md:flex items-center gap-1 text-xs text-muted-foreground whitespace-nowrap">
              <Timer size={11} />
              {formatDuration(step.startedAt, step.finishedAt)}
            </span>
          )}
        </div>

        <div className="flex items-center gap-1 shrink-0">
          <button
            type="button"
            onClick={() => prev && onSelectStep(prev.name)}
            disabled={!prev}
            title={prev ? `Previous: ${prev.name} (←)` : 'No previous step'}
            className={`flex items-center justify-center ${navBtnSize} rounded-md text-muted-foreground hover:text-foreground hover:bg-accent disabled:opacity-25 disabled:pointer-events-none transition-colors`}
          >
            <ChevronLeft size={chevronSize} />
          </button>
          <button
            type="button"
            onClick={() => next && onSelectStep(next.name)}
            disabled={!next}
            title={next ? `Next: ${next.name} (→)` : 'No next step'}
            className={`flex items-center justify-center ${navBtnSize} rounded-md text-muted-foreground hover:text-foreground hover:bg-accent disabled:opacity-25 disabled:pointer-events-none transition-colors`}
          >
            <ChevronRight size={chevronSize} />
          </button>
          <span className="block w-px h-5 bg-border mx-1.5" />
          <StepActions runId={runId} runStatus={runStatus} step={step} />
          {maximized ? (
            <>
              <button
                type="button"
                onClick={onToggleMaximize}
                title="Exit fullscreen (Esc)"
                className="flex items-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
              >
                <Minimize2 size={iconBtnSize} />
                <span className="hidden sm:inline">Exit fullscreen</span>
              </button>
              <button
                type="button"
                onClick={onBackToOverview}
                title="Close logs"
                className="flex items-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-destructive hover:border-destructive transition-colors"
              >
                <X size={iconBtnSize} />
                <span className="hidden sm:inline">Close</span>
              </button>
            </>
          ) : (
            <>
              <button
                type="button"
                onClick={onToggleMaximize}
                title="Maximize logs"
                className="flex items-center justify-center w-7 h-7 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
              >
                <Maximize2 size={13} />
              </button>
              <button
                type="button"
                onClick={onBackToOverview}
                title="Close logs (Esc)"
                className="flex items-center justify-center w-7 h-7 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
              >
                <X size={14} />
              </button>
            </>
          )}
        </div>
      </div>

      <div className="flex items-center gap-1 px-3 sm:px-4 py-1.5 border-b border-border shrink-0 bg-muted">
        <PanelTab
          icon={<Terminal size={12} />}
          label="Output"
          active={tab === 'output'}
          onClick={() => setTab('output')}
        />
        <PanelTab
          icon={<History size={12} />}
          label="Timeline"
          active={tab === 'timeline'}
          onClick={() => setTab('timeline')}
        />
        <span className="ml-auto flex items-center gap-2 text-[11px] text-muted-foreground/60">
          {(prev || next) && (
            <span className="hidden sm:inline">
              <kbd className="px-1 py-0.5 rounded border border-border bg-transparent font-mono text-[10px]">←</kbd>{' '}
              <kbd className="px-1 py-0.5 rounded border border-border bg-transparent font-mono text-[10px]">→</kbd>{' '}
              steps
            </span>
          )}
          {maximized && (
            <span>
              <kbd className="px-1 py-0.5 rounded border border-border bg-transparent font-mono text-[10px]">Esc</kbd> exit
            </span>
          )}
        </span>
      </div>

      {tab === 'output' ? (
        <div className={logBodyClass}>
          <LogView
            entries={entries ?? []}
            emptyText={
              step.status === 'pending'
                ? 'Step has not started yet.'
                : step.status === 'running'
                  ? 'Waiting for output…'
                  : 'No logs available.'
            }
          />
        </div>
      ) : (
        <div className={maximized ? 'flex-1 overflow-auto' : 'min-h-[400px] sm:min-h-[500px] max-h-[75vh] overflow-auto'}>
          <StepTimelinePanel runId={runId} stepName={step.name} live={live} />
        </div>
      )}
    </div>
  )
}

export function PanelTab({ icon, label, active, onClick }: {
  icon: React.ReactNode
  label: string
  active: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium transition-colors ${
        active ? 'text-foreground bg-accent' : 'text-muted-foreground hover:text-foreground'
      }`}
    >
      {icon}
      {label}
    </button>
  )
}

// ---------------------------------------------------------------------------
// Failure banner — leads with the failing step on a failed run
// ---------------------------------------------------------------------------

export function FailureBanner({ step, onViewLogs }: { step: any; onViewLogs: () => void }) {
  return (
    <div className="island-shell !p-0 overflow-hidden border-destructive mb-4 lg:mb-5">
      <div className="flex items-center gap-3 px-4 py-3 bg-destructive-subtle">
        <XCircle size={16} className="text-destructive shrink-0" />
        <div className="min-w-0 flex-1">
          <p className="text-sm font-semibold text-destructive">
            Failed at <span className="font-mono">{step.name}</span>
            {step.exitCode != null && (
              <span className="font-normal text-destructive/70"> · exit {step.exitCode}</span>
            )}
          </p>
          {step.error && (
            <p className="text-xs text-destructive/80 mt-0.5 font-mono truncate">{step.error}</p>
          )}
        </div>
        <button
          type="button"
          onClick={onViewLogs}
          className="shrink-0 flex items-center gap-1.5 rounded-lg border border-destructive px-3 py-1.5 text-xs font-medium text-destructive hover:bg-destructive-subtle transition-colors"
        >
          View logs <ArrowRight size={12} />
        </button>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Trigger icon/label
// ---------------------------------------------------------------------------

export function StepActions({ runId, runStatus, step }: { runId: string; runStatus: string; step: any }) {
  const invalidate = [
    orpc.runs.get.key({ input: { id: runId } }),
    orpc.runs.steps.key({ input: { runId } }),
    orpc.runs.events.key({ input: { runId } }),
  ]
  const retryFrom = useAction(
    (_: void) => client.runs.retryFromStep({ runId, stepName: step.name }),
    { invalidate: [orpc.runs.list.key()] },
  )
  const resolve = useAction(
    (outcome: 'succeeded' | 'failed' | 'skipped') =>
      client.runs.resolveStep({ runId, stepName: step.name, outcome }),
    { invalidate },
  )

  const runDone = ['succeeded', 'failed', 'cancelled'].includes(runStatus)
  const stepStuck = !STEP_TERMINAL.includes(step.status)

  if (!runDone && !stepStuck) return null

  return (
    <div className="flex items-center gap-1.5 mr-1">
      {runDone && (
        <button
          type="button"
          onClick={() => retryFrom.mutate()}
          disabled={retryFrom.isPending}
          title="Re-run the pipeline starting from this step"
          className="flex items-center gap-1.5 rounded-md border border-border px-2.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-primary hover:border-primary transition-colors disabled:opacity-50"
        >
          {retryFrom.isPending ? <Loader2 size={12} className="animate-spin" /> : <RotateCcw size={12} />}
          <span className="hidden sm:inline">Retry from here</span>
        </button>
      )}
      {stepStuck && (
        <>
          <button
            type="button"
            onClick={() => resolve.mutate('succeeded')}
            disabled={resolve.isPending}
            title="Force this step to succeeded"
            className="flex items-center justify-center w-7 h-7 rounded-md text-muted-foreground hover:text-emerald-500 hover:bg-accent transition-colors disabled:opacity-50"
          >
            <CheckCircle size={13} />
          </button>
          <button
            type="button"
            onClick={() => resolve.mutate('skipped')}
            disabled={resolve.isPending}
            title="Force this step to skipped"
            className="flex items-center justify-center w-7 h-7 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent transition-colors disabled:opacity-50"
          >
            <ChevronRight size={14} />
          </button>
          <button
            type="button"
            onClick={() => resolve.mutate('failed')}
            disabled={resolve.isPending}
            title="Force this step to failed"
            className="flex items-center justify-center w-7 h-7 rounded-md text-muted-foreground hover:text-destructive hover:bg-accent transition-colors disabled:opacity-50"
          >
            <XCircle size={13} />
          </button>
        </>
      )}
    </div>
  )
}

// StepActions exposes operator controls on the selected step: retry the whole run
// from this step (on a finished run), or force-resolve a wedged non-terminal step.
export const STEP_TERMINAL = ['succeeded', 'failed', 'skipped', 'cancelled']
