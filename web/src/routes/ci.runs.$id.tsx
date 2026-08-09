import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery, useQuery } from '@tanstack/react-query'
import { useState, lazy, Suspense, useEffect, useMemo, useRef } from 'react'
import { Loader2, Network, Activity, ScrollText, ListTree, History } from 'lucide-react'
import { relativeToMinutes, median, parseDurationToSeconds } from '#/lib/run-feed'
import { formatDateTime, formatAgo } from '#/lib/format-time'
import { orpc } from '#/lib/orpc'
import { useRunStream } from '#/hooks/use-run-stream'
import { useStepLogStream } from '#/hooks/use-step-log-stream'
import { RunGantt, PanelHeader } from '#/components/pipeline/run-gantt'
import { StepSpine } from '#/components/pipeline/step-spine'
import { GatePanel } from '#/components/pipeline/gate-panel'
import { RunAnnotations } from '#/components/pipeline/run-annotations'
import { BackLink } from '#/components/BackLink'
import { EVENT_LABELS, EVENT_TONE } from '#/lib/run-events'
import { AllLogsPanel } from '#/components/run-detail/run-logs'
import { FailureBanner, LogPanel } from '#/components/run-detail/step-panel'
import { RunHeader, RunProgress, RunSummary } from '#/components/run-detail/run-summary'

const DagView = lazy(() =>
  import('#/components/pipeline/dag-view').then((m) => ({ default: m.DagView }))
)

export const Route = createFileRoute('/ci/runs/$id')({
  component: RunDetailPage,
})

type RunView = 'steps' | 'waterfall' | 'dag' | 'timeline' | 'output'

// Ticking clock — re-renders every second only while a run is live.
function useNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!active) return
    setNow(Date.now())
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [active])
  return now
}

// Refetch while a run is in flight so its state advances on its own; stop once
// it reaches a terminal status.
const liveRefetch = (q: { state: { data?: { status?: string } } }) => {
  const s = q.state.data?.status
  return s === 'running' || s === 'pending' || s === 'paused' ? 4000 : false
}

function RunDetailPage() {
  const { id } = Route.useParams()
  const [selectedStep, setSelectedStep] = useState<string | null>(null)
  const [view, setView] = useState<RunView>('steps')
  const [logsMaximized, setLogsMaximized] = useState(false)
  const didFocus = useRef(false)

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

  // Real-time: when the run-state SSE is connected, it feeds the cache and we
  // suspend polling; otherwise we fall back to the 4s poll. sseRef is read by
  // the refetchInterval callbacks (which run after the stream connects).
  const sseRef = useRef(false)

  const { data: run } = useSuspenseQuery({
    ...orpc.runs.get.queryOptions({ input: { id } }),
    refetchInterval: (q) => (sseRef.current ? false : liveRefetch(q)),
  })
  const isLive = run.status === 'running' || run.status === 'pending'
  const { data: stepsData } = useSuspenseQuery({
    ...orpc.runs.steps.queryOptions({ input: { runId: id } }),
    refetchInterval: () => (isLive && !sseRef.current ? 4000 : false),
  })
  // Memoised so the identity is stable between renders: the `?? []` fallback
  // allocates a fresh array every render, which made every effect keyed on
  // `steps` re-run on every render of a page that polls every 4s.
  const steps = useMemo(() => stepsData?.steps ?? [], [stepsData])

  // Subscribe to live run state over SSE; mirror its connection into sseRef so
  // the polling fallback engages only when the stream is down.
  const sseConnected = useRunStream(id, isLive)
  useEffect(() => {
    sseRef.current = sseConnected
  }, [sseConnected])

  // Live elapsed clock. Prefer the earliest step start when it's recent
  // (in-flight runs are anchored near now); otherwise derive a ticking baseline
  // from the run's relative startedAt.
  const now = useNow(isLive)
  const mountedAt = useRef(Date.now())
  const startMsList = steps.map((s) => (s.startedAt ? Date.parse(s.startedAt) : NaN)).filter((n) => !Number.isNaN(n))
  const earliest = startMsList.length ? Math.min(...startMsList) : NaN
  let elapsedSecs = 0
  if (isLive) {
    if (!Number.isNaN(earliest) && now - earliest < 86_400_000) {
      elapsedSecs = Math.max(0, Math.floor((now - earliest) / 1000))
    } else {
      const baseline = Math.floor(relativeToMinutes(run.startedAt) * 60)
      elapsedSecs = baseline + Math.floor((now - mountedAt.current) / 1000)
    }
  }

  // Typical (median) duration of this project's finished runs, for an ETA-style
  // hint. Supplementary, so non-blocking.
  const { data: projRunsData } = useQuery(orpc.runs.list.queryOptions({ input: { projectId: run.projectId, limit: 30 } }))
  const medianSecs = median(
    (projRunsData?.items ?? [])
      .filter((r) => r.status === 'succeeded' || r.status === 'failed')
      .map((r) => parseDurationToSeconds(r.duration))
      .filter((s) => s > 0),
  )
  const step = selectedStep
    ? steps.find((s) => s.name === selectedStep)
    : null
  // Logs are only fetched for non-gate steps; gates have their own panel.
  const isGateStep = step?.execType === 'gate'
  // A running step's logs stream in over SSE; if the stream is down we fall
  // back to a 3s poll so output still advances. Finished steps fetch once.
  const stepRunning = step?.status === 'running'
  const logStreamConnected = useStepLogStream(
    id,
    selectedStep !== null && !isGateStep && stepRunning ? selectedStep : null,
  )
  const { data: logsData } = useQuery({
    ...orpc.runs.stepLogs.queryOptions({
      input: { runId: id, stepName: selectedStep ?? '' },
    }),
    enabled: selectedStep !== null && !isGateStep,
    refetchInterval: stepRunning && !logStreamConnected ? 3000 : false,
  })

  // Auto-focus the step that matters on first load: the failing step (failed
  // run) or whatever's in flight (live run). A clean/pending run focuses
  // nothing, so the detail pane shows the run summary instead.
  useEffect(() => {
    if (didFocus.current || steps.length === 0) return
    didFocus.current = true
    const focus =
      steps.find((s) => s.status === 'failed') ??
      steps.find((s) => s.status === 'running') ??
      steps.find((s) => s.status === 'waiting')
    if (focus) setSelectedStep(focus.name)
  }, [steps])

  function handleStepClick(name: string) {
    // Steps that haven't started have no logs. Toggle: clicking the active
    // step deselects (returns to the run summary).
    const target = steps.find((s) => s.name === name)
    if (!target?.startedAt) return
    setSelectedStep(name === selectedStep ? null : name)
  }

  // Drill in from a full-width analysis view (Waterfall / DAG) → open the
  // step's logs in the master/detail Steps view.
  function drillToStep(name: string) {
    const target = steps.find((s) => s.name === name)
    if (!target?.startedAt) return
    setSelectedStep(name)
    setView('steps')
  }

  // Lead with the failure: when the run failed, surface the failing step + error.
  const failedStep = run.status === 'failed' ? steps.find((s) => s.status === 'failed') : null

  const tabs = <ViewTabs view={view} onChange={setView} />

  return (
    <div className="rise-in">
      <div className="mb-3">
        <BackLink fallbackTo="/ci/runs" label="Back" />
      </div>

      {/* Run header */}
      <RunHeader run={run} isLive={isLive} elapsedSecs={elapsedSecs} />

      {/* Failure banner — names the failing step + error, jumps to its logs */}
      {failedStep && selectedStep !== failedStep.name && (
        <FailureBanner step={failedStep} onViewLogs={() => drillToStep(failedStep.name)} />
      )}

      {/* Annotations — markdown panels published by steps (test summaries,
          coverage). High-signal output first, before any logs are opened. */}
      <RunAnnotations runId={id} live={isLive} />

      {/* Progress — segmented bar + live step/elapsed/ETA context while running */}
      <div className="mb-4 lg:mb-5">
        <RunProgress steps={steps} isLive={isLive} elapsedSecs={elapsedSecs} medianSecs={medianSecs} />
      </div>

      {/* Body-level view switcher — controls the whole workspace, so it lives
          above it (full width) rather than crammed into a panel header. */}
      <div className="flex items-center justify-end mb-3">{tabs}</div>

      {/* Master/detail workspace. The Steps spine is the canonical navigator +
          duration chart; the detail pane shows the selected step's logs (or the
          run summary). Waterfall / DAG / Output are full-width opt-in views. */}
      {view === 'steps' ? (
        <div className="grid gap-4 lg:gap-5 items-start lg:grid-cols-[clamp(300px,32%,400px)_minmax(0,1fr)]">
          <div className="island-shell !p-0 overflow-hidden">
            <PanelHeader
              icon={<ListTree size={14} className="text-primary" />}
              title="Steps"
              subtitle={`${steps.filter((s) => s.startedAt).length}/${steps.length}`}
            />
            <StepSpine steps={steps} selectedStep={selectedStep} onStepClick={handleStepClick} now={now} />
          </div>

          <div className="min-w-0">
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
                  runId={id}
                  runStatus={run.status}
                  entries={logsData?.lines ?? null}
                  live={isLive}
                  maximized={logsMaximized}
                  onToggleMaximize={() => setLogsMaximized((v) => !v)}
                  onSelectStep={setSelectedStep}
                  onBackToOverview={() => setSelectedStep(null)}
                />
              )
            ) : (
              <RunSummary run={run} steps={steps} isLive={isLive} now={now} onStepClick={handleStepClick} />
            )}
          </div>
        </div>
      ) : view === 'waterfall' ? (
        <RunGantt steps={steps} selectedStep={selectedStep} onStepClick={drillToStep} />
      ) : view === 'output' ? (
        <AllLogsPanel runId={id} steps={steps} />
      ) : view === 'timeline' ? (
        <RunTimeline runId={id} />
      ) : (
        <div className="island-shell !p-0 overflow-hidden">
          <PanelHeader
            icon={<Network size={14} className="text-primary" />}
            title="DAG"
            subtitle={`${steps.length} step${steps.length === 1 ? '' : 's'}`}
          />
          <div className="min-h-[420px] sm:min-h-[520px] lg:min-h-[600px] h-[calc(100vh-360px)] max-h-[1100px]">
            <Suspense fallback={
              <div className="h-full flex items-center justify-center text-muted-foreground text-sm">
                Loading DAG…
              </div>
            }>
              <DagView steps={steps} direction="DOWN" onStepClick={drillToStep} />
            </Suspense>
          </div>
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// View tabs — Steps (master/detail) · Waterfall · DAG · Output
// ---------------------------------------------------------------------------

function ViewTabs({ view, onChange }: { view: RunView; onChange: (v: RunView) => void }) {
  return (
    <div className="flex items-center rounded-lg border border-border p-0.5" style={{ background: 'var(--surface)' }}>
      <ViewToggle label="Steps" icon={<ListTree size={13} />} active={view === 'steps'} onClick={() => onChange('steps')} />
      <ViewToggle label="Waterfall" icon={<Activity size={13} />} active={view === 'waterfall'} onClick={() => onChange('waterfall')} />
      <ViewToggle label="DAG" icon={<Network size={13} />} active={view === 'dag'} onClick={() => onChange('dag')} />
      <ViewToggle label="Timeline" icon={<History size={13} />} active={view === 'timeline'} onClick={() => onChange('timeline')} />
      <ViewToggle label="Output" icon={<ScrollText size={13} />} active={view === 'output'} onClick={() => onChange('output')} />
    </div>
  )
}

// ---------------------------------------------------------------------------
// Timeline — the durable transition history (engine_events) for this run.
// ---------------------------------------------------------------------------

function RunTimeline({ runId }: { runId: string }) {
  const { data, isLoading } = useQuery(orpc.runs.events.queryOptions({ input: { runId } }))
  const events = data?.events ?? []

  return (
    <div className="island-shell !p-0 overflow-hidden">
      <PanelHeader
        icon={<History size={14} className="text-primary" />}
        title="Timeline"
        subtitle={`${events.length} event${events.length === 1 ? '' : 's'}`}
      />
      {isLoading ? (
        <div className="p-8 flex items-center justify-center text-muted-foreground text-sm">
          <Loader2 size={14} className="animate-spin mr-2" /> Loading timeline…
        </div>
      ) : events.length === 0 ? (
        <div className="p-8 text-center text-muted-foreground text-sm">No events recorded yet.</div>
      ) : (
        <ol className="divide-y divide-border/60">
          {events.map((e, i) => {
            const label = EVENT_LABELS[e.event] ?? e.event
            const tone = EVENT_TONE[e.event] ?? 'text-foreground'
            return (
              <li key={i} className="flex items-start gap-3 px-4 py-2.5 text-sm">
                <span className="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-current opacity-70" />
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-baseline gap-x-2">
                    <span className={`font-medium ${tone}`}>{label}</span>
                    {e.stepName && (
                      <span className="font-mono text-xs text-muted-foreground truncate">{e.stepName}</span>
                    )}
                    {typeof e.attempt === 'number' && e.attempt > 0 && (
                      <span className="text-[10px] text-muted-foreground">attempt {e.attempt + 1}</span>
                    )}
                  </div>
                  {(e.reason || e.actor !== 'engine') && (
                    <div className="mt-0.5 text-xs text-muted-foreground">
                      {e.actor !== 'engine' && <span className="opacity-80">{e.actor}</span>}
                      {e.reason && <span>{e.actor !== 'engine' ? ' · ' : ''}{e.reason}</span>}
                    </div>
                  )}
                </div>
                <time className="shrink-0 text-[11px] text-muted-foreground tabular-nums" title={formatDateTime(Date.parse(e.at))}>
                  {formatAgo(Date.parse(e.at))}
                </time>
              </li>
            )
          })}
        </ol>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Run header
// ---------------------------------------------------------------------------

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
        active ? 'text-white' : 'text-muted-foreground hover:text-foreground'
      }`}
      style={active ? { background: 'color-mix(in oklab, var(--ring), black 35%)' } : undefined}
    >
      {icon}
      <span className="hidden sm:inline">{label}</span>
    </button>
  )
}

// ---------------------------------------------------------------------------
// All-output panel — one contiguous, collapsible-by-step log stream
// ---------------------------------------------------------------------------
