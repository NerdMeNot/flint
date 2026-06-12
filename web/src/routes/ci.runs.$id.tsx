import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery, useQuery } from '@tanstack/react-query'
import { useState, lazy, Suspense, useEffect } from 'react'
import {
  GitBranch,
  GitCommit,
  Clock,
  User,
  CheckCircle,
  XCircle,
  Loader2,
  Terminal,
  Timer,
  Network,
  Activity,
  Ban,
  RotateCcw,
  ChevronLeft,
  ChevronRight,
  Maximize2,
  Minimize2,
  X,
} from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { PipelineProgress } from '#/components/PipelineProgress'
import { RunGantt, PanelHeader } from '#/components/pipeline/run-gantt'
import { StepRail } from '#/components/pipeline/step-rail'
import { GatePanel } from '#/components/pipeline/gate-panel'

const DagView = lazy(() =>
  import('#/components/pipeline/dag-view').then((m) => ({ default: m.DagView }))
)

export const Route = createFileRoute('/ci/runs/$id')({
  component: RunDetailPage,
})

type OverviewView = 'timeline' | 'dag'

function RunDetailPage() {
  const { id } = Route.useParams()
  const [selectedStep, setSelectedStep] = useState<string | null>(null)
  const [overview, setOverview] = useState<OverviewView>('timeline')
  const [logsMaximized, setLogsMaximized] = useState(false)

  // Esc closes the maximized log overlay. Body scroll is locked while
  // the overlay is up so background content can't be scrolled behind it.
  useEffect(() => {
    if (!logsMaximized) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setLogsMaximized(false)
    }
    document.addEventListener('keydown', onKey)
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = prev
    }
  }, [logsMaximized])

  // Deselecting a step also exits maximize.
  useEffect(() => {
    if (selectedStep === null && logsMaximized) setLogsMaximized(false)
  }, [selectedStep, logsMaximized])

  const { data: run } = useSuspenseQuery(
    orpc.runs.get.queryOptions({ input: { id } }),
  )
  const { data: stepsData } = useSuspenseQuery(
    orpc.runs.steps.queryOptions({ input: { runId: id } }),
  )
  const steps = stepsData?.steps ?? []
  const step = selectedStep
    ? steps.find((s) => s.name === selectedStep)
    : null
  // Logs are only fetched for non-gate steps; gates have their own panel.
  const isGateStep = step?.execType === 'gate'
  const { data: logsData } = useQuery({
    ...orpc.runs.stepLogs.queryOptions({
      input: { runId: id, stepName: selectedStep ?? '' },
    }),
    enabled: selectedStep !== null && !isGateStep,
  })

  function handleStepClick(name: string) {
    // Steps that haven't started have no logs, so we keep them out of
    // the log panel entirely. The rail/Gantt also disable clicks on
    // those rows, but we guard here too in case a new entry-point is
    // added later.
    const target = steps.find((s) => s.name === name)
    if (!target?.startedAt) return
    // Toggle: clicking the active step deselects (returns to overview).
    setSelectedStep(name === selectedStep ? null : name)
  }

  return (
    <div className="rise-in">
      {/* Run header */}
      <RunHeader run={run} />

      {/* Pipeline progress bar */}
      <div className="mb-4 lg:mb-5">
        <PipelineProgress steps={steps} />
      </div>

      {/* Two-column workspace: rail + content. Below lg the rail
          collapses into a horizontal pill strip stacked above the
          content. */}
      <div className="flex flex-col gap-4 lg:gap-5 lg:grid lg:grid-cols-[280px_minmax(0,1fr)]">
        <aside className="lg:sticky lg:top-[88px] lg:self-start">
          <StepRail
            steps={steps}
            selectedStep={selectedStep}
            onStepClick={handleStepClick}
          />
        </aside>

        <section className="min-w-0">
          {selectedStep && step ? (
            isGateStep ? (
              <GatePanel
                step={step}
                steps={steps}
                run={run}
                onSelectStep={setSelectedStep}
                onBackToOverview={() => setSelectedStep(null)}
              />
            ) : (
              <LogPanel
                step={step}
                steps={steps}
                logs={logsData?.lines ?? null}
                maximized={logsMaximized}
                onToggleMaximize={() => setLogsMaximized((v) => !v)}
                onSelectStep={setSelectedStep}
                onBackToOverview={() => setSelectedStep(null)}
              />
            )
          ) : (
            <OverviewPanel
              steps={steps}
              view={overview}
              onViewChange={setOverview}
              selectedStep={selectedStep}
              onStepClick={handleStepClick}
            />
          )}
        </section>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Run header
// ---------------------------------------------------------------------------

function RunHeader({ run }: { run: any }) {
  const invalidate = [orpc.runs.get.key({ input: { id: run.id } }), orpc.runs.list.key()]
  const cancel = useAction((id: string) => client.runs.cancel({ runId: id }), { invalidate })
  const retry = useAction((id: string) => client.runs.retry({ runId: id }), { invalidate })
  return (
    <div className="island-shell p-4 sm:p-5 lg:p-6 mb-4 lg:mb-5">
      <div className="space-y-2 lg:space-y-3">
        <div className="flex flex-wrap items-center gap-2 sm:gap-3">
          <StatusBadge status={run.status} />
          <Link
            to="/ci/projects/$id"
            params={{ id: run.projectId }}
            className="display-title text-xl sm:text-2xl lg:text-3xl font-bold text-foreground hover:text-primary transition-colors truncate"
          >
            {run.projectName}
          </Link>
          <span className="text-xs text-muted-foreground font-mono opacity-60">
            {run.workflowFile}
          </span>
        </div>
        <p className="text-sm lg:text-base text-muted-foreground flex items-center gap-2">
          <GitCommit size={14} className="shrink-0" />
          <span className="truncate">{run.commitMessage}</span>
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-3 sm:gap-5 mt-4 text-xs text-muted-foreground">
        <span className="flex items-center gap-1.5">
          <GitBranch size={13} />
          <span className="font-mono">{run.branch}</span>
        </span>
        <span className="flex items-center gap-1.5">
          <GitCommit size={13} />
          <span className="font-mono">{run.commitSha}</span>
        </span>
        <span className="flex items-center gap-1.5">
          <User size={13} />
          {run.triggeredBy}
        </span>
        <span className="flex items-center gap-1.5">
          <Clock size={13} />
          {run.duration}
        </span>
        <span className="island-kicker !text-[11px]">{run.triggerType}</span>

        <div className="flex items-center gap-2 ml-auto">
          {(run.status === 'running' || run.status === 'pending') && (
            <button
              type="button"
              onClick={() => cancel.mutate(run.id)}
              disabled={cancel.isPending}
              className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-destructive hover:border-destructive/30 transition-colors disabled:opacity-50"
            >
              <Ban size={12} />
              Cancel
            </button>
          )}
          {(run.status === 'failed' || run.status === 'cancelled') && (
            <button
              type="button"
              onClick={() => retry.mutate(run.id)}
              disabled={retry.isPending}
              className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-primary hover:border-primary/30 transition-colors disabled:opacity-50"
            >
              <RotateCcw size={12} />
              Retry
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Overview panel — Timeline / DAG toggle, no step selected
// ---------------------------------------------------------------------------

function OverviewPanel({
  steps, view, onViewChange, selectedStep, onStepClick,
}: {
  steps: any[]
  view: OverviewView
  onViewChange: (v: OverviewView) => void
  selectedStep: string | null
  onStepClick: (name: string) => void
}) {
  // Toolbar lives inside the card header so the card's top edge aligns
  // with the Steps rail card.
  const toolbar = (
    <div
      className="flex items-center rounded-lg border border-border p-0.5"
      style={{ background: 'var(--surface)' }}
    >
      <ViewToggle
        label="Timeline"
        icon={<Activity size={13} />}
        active={view === 'timeline'}
        onClick={() => onViewChange('timeline')}
      />
      <ViewToggle
        label="DAG"
        icon={<Network size={13} />}
        active={view === 'dag'}
        onClick={() => onViewChange('dag')}
      />
    </div>
  )

  if (view === 'timeline') {
    return (
      <RunGantt
        steps={steps}
        selectedStep={selectedStep}
        onStepClick={onStepClick}
        toolbar={toolbar}
      />
    )
  }

  return (
    <div className="island-shell !p-0 overflow-hidden">
      <PanelHeader
        icon={<Network size={14} className="text-primary" />}
        title="DAG"
        subtitle={`${steps.length} step${steps.length === 1 ? '' : 's'}`}
        toolbar={toolbar}
      />
      {/* DAG canvas: respects a comfortable minimum at every breakpoint
          and grows to fill the remaining viewport when there's room
          below. The calc subtracts the static stack above (header,
          run-card, progress, gaps) so the DAG fits to the bottom edge
          on tall windows without overflowing on short ones. The max
          keeps it sane on ultrawide vertical monitors. */}
      <div className="min-h-[420px] sm:min-h-[520px] lg:min-h-[600px] h-[calc(100vh-360px)] max-h-[1100px]">
        <Suspense fallback={
          <div className="h-full flex items-center justify-center text-muted-foreground text-sm">
            Loading DAG…
          </div>
        }>
          <DagView
            steps={steps}
            direction="DOWN"
            onStepClick={onStepClick}
          />
        </Suspense>
      </div>
    </div>
  )
}

function ViewToggle({ label, icon, active, onClick }: {
  label: string
  icon: React.ReactNode
  active: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium whitespace-nowrap transition-all duration-150 ${
        active ? 'text-white shadow-sm' : 'text-muted-foreground hover:text-foreground'
      }`}
      style={active ? { background: 'color-mix(in oklab, var(--ring), black 35%)' } : undefined}
    >
      {icon}
      <span className="hidden sm:inline">{label}</span>
    </button>
  )
}

// ---------------------------------------------------------------------------
// Log panel — replaces overview when a step is selected
// ---------------------------------------------------------------------------

function LogPanel({
  step, steps, logs, maximized, onToggleMaximize, onSelectStep, onBackToOverview,
}: {
  step: any
  steps: any[]
  logs: string | null
  maximized: boolean
  onToggleMaximize: () => void
  onSelectStep: (name: string) => void
  onBackToOverview: () => void
}) {
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
                className="flex items-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-destructive hover:border-destructive/30 transition-colors"
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

      <div className="flex items-center gap-1.5 px-3 sm:px-4 py-2 border-b border-border shrink-0 bg-muted/20">
        <Terminal size={12} className="text-primary" />
        <span className="text-xs font-semibold text-foreground">Output</span>
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

      <div className={logBodyClass}>
        {logs ? (
          logs.split('\n').map((line, i) => (
            <div key={i} className="flex gap-4 hover:bg-[#161b22] -mx-2 px-2 py-px rounded">
              <span className="text-[#484f58] select-none shrink-0 w-7 text-right">
                {i + 1}
              </span>
              <span className={colorizeLine(line)}>{line}</span>
            </div>
          ))
        ) : (
          <span className="text-[#484f58] italic">
            {step.status === 'pending'
              ? 'Step has not started yet.'
              : step.status === 'running'
                ? 'Waiting for output…'
                : 'No logs available.'}
          </span>
        )}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Shared bits
// ---------------------------------------------------------------------------

function colorizeLine(line: string): string {
  if (line.includes('PASS') || line.includes('complete') || line.includes('saved'))
    return 'text-[#7ee787]'
  if (line.includes('Error') || line.includes('FAIL'))
    return 'text-[#ff7b72]'
  if (line.includes('Warning') || line.includes('deprecated'))
    return 'text-[#d29922]'
  if (line.startsWith('['))
    return 'text-[#c9d1d9]'
  return 'text-[#8b949e]'
}

function StatusBadge({ status }: { status: string }) {
  const config: Record<string, { icon: React.ReactNode; label: string; className: string }> = {
    succeeded: {
      icon: <CheckCircle size={12} />,
      label: 'Passed',
      className: 'bg-success/10 text-success border-success/20',
    },
    failed: {
      icon: <XCircle size={12} />,
      label: 'Failed',
      className: 'bg-destructive/10 text-destructive border-destructive/20',
    },
    running: {
      icon: <Loader2 size={12} className="animate-spin" />,
      label: 'Running',
      className: 'bg-primary/10 text-primary border-primary/20',
    },
    pending: {
      icon: <Clock size={12} />,
      label: 'Pending',
      className: 'bg-secondary text-muted-foreground border-border',
    },
    waiting: {
      icon: <Clock size={12} />,
      label: 'Waiting',
      className: 'bg-warning/10 text-warning border-warning/20',
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

function StatusIcon({ status, size = 18 }: { status: string; size?: number }) {
  switch (status) {
    case 'succeeded': return <CheckCircle size={size} className="text-success shrink-0" />
    case 'failed': return <XCircle size={size} className="text-destructive shrink-0" />
    case 'running': return <Loader2 size={size} className="text-primary shrink-0 animate-spin" />
    case 'waiting': return <Clock size={size} className="text-warning shrink-0" />
    default: return <Clock size={size} className="text-muted-foreground shrink-0" />
  }
}

function formatDuration(start: string, end: string): string {
  const ms = new Date(end).getTime() - new Date(start).getTime()
  const seconds = Math.floor(ms / 1000)
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}
