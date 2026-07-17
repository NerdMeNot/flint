import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery, useQuery } from '@tanstack/react-query'
import { useState, lazy, Suspense, useEffect, useRef } from 'react'
import {
  GitBranch,
  GitCommit,
  Clock,
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
  ArrowRight,
  GitCommitHorizontal,
  GitPullRequest,
  MousePointerClick,
  CalendarClock,
  ScrollText,
  Search,
  Copy,
  Download,
  Check,
  ChevronDown,
  Pause,
  ListTree,
  Hourglass,
  Gauge,
  History,
  Server,
} from 'lucide-react'
import { useCopyToClipboard } from '#/hooks/use-copy-to-clipboard'
import { relativeToMinutes, median, parseDurationToSeconds } from '#/lib/run-feed'
import { formatDateTime, formatAgo } from '#/lib/format-time'
import { orpc } from '#/lib/orpc'
import { client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { useRunStream } from '#/hooks/use-run-stream'
import { useStepLogStream } from '#/hooks/use-step-log-stream'
import { PipelineProgress } from '#/components/PipelineProgress'
import { RunGantt, PanelHeader } from '#/components/pipeline/run-gantt'
import { StepSpine, fmtDur } from '#/components/pipeline/step-spine'
import { GatePanel } from '#/components/pipeline/gate-panel'
import { LogView } from '#/components/pipeline/log-view'
import { StepTimelinePanel } from '#/components/pipeline/step-timeline-panel'
import { RunAnnotations } from '#/components/pipeline/run-annotations'
import { BackLink } from '#/components/BackLink'
import { EVENT_LABELS, EVENT_TONE } from '#/lib/run-events'
import { stripAnsi } from '#/lib/ansi'
import type { StepLogLine } from '#/lib/api/types'

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

function fmtSecs(secs: number): string {
  if (secs < 60) return `${secs}s`
  const m = Math.floor(secs / 60)
  if (m < 60) return `${m}m ${secs % 60}s`
  return `${Math.floor(m / 60)}h ${m % 60}m`
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
  const steps = stepsData?.steps ?? []

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

function RunHeader({ run, isLive, elapsedSecs }: { run: any; isLive: boolean; elapsedSecs: number }) {
  const invalidate = [orpc.runs.get.key({ input: { id: run.id } }), orpc.runs.list.key()]
  const cancel = useAction((id: string) => client.runs.cancel({ runId: id }), { invalidate })
  const retry = useAction((id: string) => client.runs.retry({ runId: id }), { invalidate })
  const pause = useAction((id: string) => client.runs.pause({ runId: id }), { invalidate })
  const resume = useAction((id: string) => client.runs.resume({ runId: id }), { invalidate })
  const rerunFailed = useAction((id: string) => client.runs.rerunFailed({ runId: id }), { invalidate })
  return (
    <div className="island-shell p-4 sm:p-5 mb-4 lg:mb-5">
      <div className="flex flex-wrap items-center gap-2 sm:gap-3">
        <StatusBadge status={run.status} />
        <Link
          to="/ci/projects/$id"
          params={{ id: run.projectId }}
          className="display-title text-lg sm:text-xl lg:text-2xl font-bold text-foreground hover:text-primary transition-colors truncate"
        >
          {run.projectName}
        </Link>
        <span className="hidden sm:inline text-muted-foreground/30">/</span>
        <p className="text-sm text-muted-foreground flex items-center gap-1.5 min-w-0 flex-1">
          <GitCommit size={13} className="shrink-0 opacity-60" />
          <span className="truncate">{run.commitMessage}</span>
        </p>
        <span className="hidden lg:inline text-xs text-muted-foreground font-mono opacity-50 shrink-0">
          {run.workflowFile}
        </span>
      </div>

      <div className="flex flex-wrap items-center gap-3 sm:gap-5 mt-3 text-xs text-muted-foreground">
        <span className="flex items-center gap-1.5">
          <GitBranch size={13} />
          <span className="font-mono">{run.branch}</span>
        </span>
        <span className="flex items-center gap-1.5">
          <GitCommit size={13} />
          <span className="font-mono">{run.commitSha}</span>
        </span>
        <span className="flex items-center gap-1.5" title={triggerLabel(run.triggerType)}>
          <TriggerIcon type={run.triggerType} />
          {run.triggeredBy}
        </span>
        <span className={`flex items-center gap-1.5 ${isLive ? 'text-primary font-medium' : ''}`}>
          <Timer size={13} className={isLive ? 'animate-pulse' : ''} />
          {isLive ? `${fmtSecs(elapsedSecs)} elapsed` : run.duration}
        </span>
        <span className="flex items-center gap-1.5" title="Started">
          <Clock size={13} />
          Started {run.startedAtTs ? formatDateTime(run.startedAtTs) : run.startedAt}
          {run.startedAtTs && <span className="opacity-50">· {formatAgo(run.startedAtTs)}</span>}
        </span>
        {run.finishedAtTs && (
          <span className="flex items-center gap-1.5">
            <CheckCircle size={13} />
            Ended {formatDateTime(run.finishedAtTs)}
          </span>
        )}
        <RunCostChip runId={run.id} live={isLive} />

        <div className="flex items-center gap-2 ml-auto">
          {run.status === 'running' && (
            <button
              type="button"
              onClick={() => pause.mutate(run.id)}
              disabled={pause.isPending}
              className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:border-foreground/30 transition-colors disabled:opacity-50"
            >
              {pause.isPending ? <Loader2 size={12} className="animate-spin" /> : <Pause size={12} />}
              {pause.isPending ? 'Pausing…' : 'Pause'}
            </button>
          )}
          {run.status === 'paused' && (
            <button
              type="button"
              onClick={() => resume.mutate(run.id)}
              disabled={resume.isPending}
              className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-primary hover:border-primary/30 transition-colors disabled:opacity-50"
            >
              {resume.isPending ? <Loader2 size={12} className="animate-spin" /> : <ArrowRight size={12} />}
              {resume.isPending ? 'Resuming…' : 'Resume'}
            </button>
          )}
          {(run.status === 'running' || run.status === 'pending' || run.status === 'paused') && (
            <button
              type="button"
              onClick={() => cancel.mutate(run.id)}
              disabled={cancel.isPending}
              className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-destructive hover:border-destructive/30 transition-colors disabled:opacity-50"
            >
              {cancel.isPending ? <Loader2 size={12} className="animate-spin" /> : <Ban size={12} />}
              {cancel.isPending ? 'Cancelling…' : 'Cancel'}
            </button>
          )}
          {(run.status === 'failed' || run.status === 'cancelled') && (
            <>
              <button
                type="button"
                onClick={() => rerunFailed.mutate(run.id)}
                disabled={rerunFailed.isPending}
                className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-primary hover:border-primary/30 transition-colors disabled:opacity-50"
              >
                {rerunFailed.isPending ? <Loader2 size={12} className="animate-spin" /> : <RotateCcw size={12} />}
                {rerunFailed.isPending ? 'Re-running…' : 'Re-run failed'}
              </button>
              <button
                type="button"
                onClick={() => retry.mutate(run.id)}
                disabled={retry.isPending}
                className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-primary hover:border-primary/30 transition-colors disabled:opacity-50"
              >
                {retry.isPending ? <Loader2 size={12} className="animate-spin" /> : <RotateCcw size={12} />}
                {retry.isPending ? 'Retrying…' : 'Re-run all'}
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Run cost — requested compute × wall-clock with a $ estimate. Flint schedules
// the pods, so it knows exactly what each step asked for and for how long.
function RunCostChip({ runId, live }: { runId: string; live: boolean }) {
  const { data } = useQuery({
    ...orpc.runs.cost.queryOptions({ input: { runId } }),
    refetchInterval: live ? 10_000 : false,
  })
  if (!data || data.totalCoreSecs <= 0) return null
  const usd = data.estimatedUsd
  const money = usd >= 0.01 ? `$${usd.toFixed(2)}` : `$${usd.toFixed(4)}`
  return (
    <span
      className="flex items-center gap-1.5"
      title={`${data.totalCoreSecs.toFixed(0)} core-seconds · ${data.totalGbSecs.toFixed(0)} GB-seconds (requested compute × duration, at $${data.rates.cpuCoreHourUsd}/core-hr)`}
    >
      <span className="font-mono">≈ {money}</span>
      <span className="opacity-50">{data.totalCoreSecs.toFixed(0)} core-s</span>
    </span>
  )
}

// Run progress — segmented bar + live step/elapsed/ETA context while running
// ---------------------------------------------------------------------------

const TERMINAL_STATUSES = ['succeeded', 'failed', 'skipped', 'cancelled']

function RunProgress({ steps, isLive, elapsedSecs, medianSecs }: {
  steps: any[]
  isLive: boolean
  elapsedSecs: number
  medianSecs: number
}) {
  const total = steps.length
  const done = steps.filter((s) => TERMINAL_STATUSES.includes(s.status)).length
  const current = steps.find((s) => s.status === 'running') ?? steps.find((s) => s.status === 'waiting')
  const pct = total > 0 ? Math.round((done / total) * 100) : 0

  return (
    <div className="space-y-2">
      <PipelineProgress steps={steps} />
      {isLive && total > 0 && (
        <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1 text-xs text-muted-foreground">
          <span className="text-foreground font-medium">Step {Math.min(done + 1, total)} of {total}</span>
          <span className="opacity-40">·</span>
          <span className="tabular-nums">{pct}%</span>
          {current && (
            <>
              <span className="opacity-40">·</span>
              <span className={`flex items-center gap-1 ${current.status === 'waiting' ? 'text-warning' : 'text-primary'}`}>
                {current.status === 'waiting'
                  ? <><Pause size={11} /> Awaiting approval:</>
                  : <><Loader2 size={11} className="animate-spin" /> Running:</>}
                <span className="font-mono">{current.name}</span>
              </span>
            </>
          )}
          <span className="opacity-40">·</span>
          <span className="flex items-center gap-1">
            <Timer size={11} />
            <span className="tabular-nums">{fmtSecs(elapsedSecs)}</span> elapsed
            {medianSecs > 0 && <span className="opacity-70"> · ~{fmtSecs(Math.round(medianSecs))} typical</span>}
          </span>
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Run summary — the detail pane when no step is selected (clean/pending runs)
// ---------------------------------------------------------------------------

function RunSummary({ run, steps, isLive, now, onStepClick }: {
  run: any
  steps: any[]
  isLive: boolean
  now: number
  onStepClick: (name: string) => void
}) {
  const started = steps.filter((s) => s.startedAt)
  const passed = steps.filter((s) => s.status === 'succeeded').length
  const total = steps.length

  // Per-step run/queue time. Gates are human waits, not slow work, so they're
  // excluded from the "slowest steps" ranking and its scale.
  const timed = started
    .map((s) => {
      const startMs = Date.parse(s.startedAt)
      const schedMs = s.scheduledAt ? Date.parse(s.scheduledAt) : startMs
      const endMs = s.finishedAt ? Date.parse(s.finishedAt) : now
      return { step: s, runMs: Math.max(0, endMs - startMs), waitMs: Math.max(0, startMs - schedMs) }
    })
  const slowest = timed
    .filter((t) => t.step.execType !== 'gate')
    .sort((a, b) => b.runMs - a.runMs)
  const maxRun = Math.max(1, ...slowest.map((t) => t.runMs))
  const totalQueue = timed.reduce((sum, t) => sum + t.waitMs, 0)

  const wallMs = run.startedAtTs && run.finishedAtTs ? run.finishedAtTs - run.startedAtTs : 0

  return (
    <div className="island-shell !p-0 overflow-hidden">
      <PanelHeader icon={<Gauge size={14} className="text-primary" />} title="Run summary" />

      <div className="p-4 sm:p-5 space-y-5">
        {/* Headline stats */}
        <div className="grid grid-cols-3 gap-3">
          <Stat
            icon={<CheckCircle size={14} className={passed === total ? 'text-success' : 'text-muted-foreground'} />}
            label="Steps passed"
            value={`${passed}/${total}`}
          />
          <Stat
            icon={<Timer size={14} className={isLive ? 'text-primary' : 'text-muted-foreground'} />}
            label={isLive ? 'Elapsed' : 'Duration'}
            value={wallMs ? fmtDur(wallMs) : run.duration}
          />
          <Stat
            icon={<Hourglass size={14} className="text-muted-foreground" />}
            label="Queued"
            value={fmtDur(totalQueue)}
          />
        </div>

        {/* Where the time went */}
        {slowest.length > 0 && (
          <div className="space-y-2">
            <p className="text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground/60">
              Slowest steps
            </p>
            <div className="space-y-1.5">
              {slowest.slice(0, 5).map(({ step, runMs }) => (
                <button
                  key={step.name}
                  type="button"
                  onClick={() => onStepClick(step.name)}
                  className="group w-full flex items-center gap-3 text-left"
                >
                  <StatusIcon status={step.status} size={13} />
                  <span className="text-[13px] text-foreground/90 group-hover:text-primary transition-colors w-32 sm:w-40 truncate shrink-0">
                    {step.name}
                  </span>
                  <span className="flex-1 h-2 rounded-full bg-muted/50 overflow-hidden">
                    <span
                      className={`block h-full rounded-full ${step.status === 'failed' ? 'bg-destructive' : 'bg-success'}`}
                      style={{ width: `${Math.max((runMs / maxRun) * 100, 4)}%` }}
                    />
                  </span>
                  <span className="text-[11px] font-mono tabular-nums text-muted-foreground w-12 text-right shrink-0">
                    {fmtDur(runMs)}
                  </span>
                </button>
              ))}
            </div>
          </div>
        )}

        <PlacementSection runId={run.id} live={isLive} />

        <p className="flex items-center gap-2 text-xs text-muted-foreground pt-1 border-t border-border/60">
          <ArrowRight size={12} className="text-muted-foreground/50" />
          Select a step to view its logs
        </p>
      </div>
    </div>
  )
}

// PlacementSection is decision transparency at run scope: which machine ran
// each step, at what price, and how long it queued. Absent for runs with no
// machine steps (http-only, sim).
function PlacementSection({ runId, live }: { runId: string; live: boolean }) {
  const { data } = useQuery({
    ...orpc.runs.placement.queryOptions({ input: { runId } }),
    refetchInterval: live ? 5000 : false,
  })
  const placements = data?.placements ?? []
  if (placements.length === 0) return null

  return (
    <div className="space-y-2">
      <p className="text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground/60">
        Placement
      </p>
      <div className="space-y-1.5">
        {placements.map((p) => (
          <div key={`${p.stepName}-${p.attempt}`} className="flex items-center gap-3 text-[12px]">
            <Server size={12} className="text-muted-foreground/60 shrink-0" />
            <span className="text-foreground/90 w-32 sm:w-40 truncate shrink-0">{p.stepName}</span>
            <span className="font-mono text-muted-foreground truncate flex-1">
              {p.machineId
                ? `${p.machineId.slice(0, 8)}${p.instanceType ? ` · ${p.instanceType}` : ''}${p.capacityType === 'spot' ? ' · spot' : ''}`
                : p.status === 'pending' ? 'waiting for a machine…' : p.status}
            </span>
            {p.queueWaitMs != null && (
              <span className="text-[11px] font-mono tabular-nums text-muted-foreground shrink-0" title="queue wait">
                +{fmtDur(p.queueWaitMs)}
              </span>
            )}
            {p.pricePerHourUsd != null && (
              <span className="text-[11px] font-mono tabular-nums text-muted-foreground w-16 text-right shrink-0">
                ${p.pricePerHourUsd.toFixed(3)}/hr
              </span>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}

function Stat({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <div className="rounded-lg border border-border bg-muted/20 px-3 py-2.5">
      <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
        {icon}
        <span className="truncate">{label}</span>
      </div>
      <p className="mt-1 text-lg font-bold tabular-nums text-foreground">{value}</p>
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
// All-output panel — one contiguous, collapsible-by-step log stream
// ---------------------------------------------------------------------------

function AllLogsPanel({ runId, steps, toolbar }: { runId: string; steps: any[]; toolbar?: React.ReactNode }) {
  const { data } = useQuery(orpc.runs.logs.queryOptions({ input: { runId } }))
  const logsMap = data?.logs ?? {}
  const { copied, copy } = useCopyToClipboard()
  const [search, setSearch] = useState('')
  const [overrides, setOverrides] = useState<Record<string, boolean>>({})
  const q = search.toLowerCase().trim()

  // Started steps, in execution order. Collapse succeeded by default; expand
  // failed/running so the interesting output is visible without a click. A
  // search expands everything (and hides sections with no match).
  const started = steps
    .filter((s) => s.startedAt)
    .sort((a, b) => Date.parse(a.startedAt) - Date.parse(b.startedAt))

  const defaultOpen = (s: any) => s.status === 'failed' || s.status === 'running'
  const isOpen = (s: any) => (q ? true : s.name in overrides ? overrides[s.name] : defaultOpen(s))
  const toggle = (s: any) => setOverrides((o) => ({ ...o, [s.name]: !(s.name in o ? o[s.name] : defaultOpen(s)) }))

  const allOpen = started.every(isOpen)
  const setAll = (open: boolean) => setOverrides(Object.fromEntries(started.map((s) => [s.name, open])))

  const fullText = started.map((s) => `===== ${s.name} (${s.status}) =====\n${logsMap[s.name] ?? ''}`).join('\n\n')
  function download() {
    const blob = new Blob([fullText], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${runId}.log`
    a.click()
    URL.revokeObjectURL(url)
  }

  const visible = started.filter((s) => !q || stripAnsi(logsMap[s.name] ?? '').toLowerCase().includes(q))

  return (
    <div className="island-shell !p-0 overflow-hidden flex flex-col">
      <PanelHeader
        icon={<ScrollText size={14} className="text-primary" />}
        title="Output"
        subtitle={`${started.length} step${started.length === 1 ? '' : 's'}`}
        toolbar={toolbar}
      />

      <div className="flex items-center gap-2 px-3 sm:px-4 py-2 border-b border-border bg-muted/20">
        <div className="relative flex-1 max-w-xs">
          <Search size={12} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground pointer-events-none" />
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search all output…"
            className="w-full pl-7 pr-2 py-1 text-xs rounded-md border border-border bg-transparent text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-1 focus:ring-ring/40"
          />
        </div>
        <button
          type="button"
          onClick={() => setAll(!allOpen)}
          className="text-[11px] font-medium text-muted-foreground hover:text-foreground transition-colors px-1.5"
        >
          {allOpen ? 'Collapse all' : 'Expand all'}
        </button>
        <span className="block w-px h-4 bg-border" />
        <button
          type="button"
          onClick={() => copy(fullText)}
          title="Copy all output"
          className="flex items-center gap-1 text-[11px] font-medium text-muted-foreground hover:text-foreground transition-colors px-1.5"
        >
          {copied ? <Check size={12} className="text-success" /> : <Copy size={12} />}
          <span className="hidden sm:inline">{copied ? 'Copied' : 'Copy'}</span>
        </button>
        <button
          type="button"
          onClick={download}
          title="Download .log"
          className="flex items-center gap-1 text-[11px] font-medium text-muted-foreground hover:text-foreground transition-colors px-1.5"
        >
          <Download size={12} />
          <span className="hidden sm:inline">Download</span>
        </button>
      </div>

      <div className="bg-[#0d1117] overflow-auto max-h-[75vh] min-h-[300px]">
        {visible.length === 0 ? (
          <p className="p-6 text-center text-sm text-[#484f58]">
            {q ? 'No matching output.' : 'No output yet.'}
          </p>
        ) : (
          visible.map((s) => {
            const text = logsMap[s.name] ?? ''
            const lines = text ? text.split('\n') : []
            const matches = q ? lines.filter((l) => stripAnsi(l).toLowerCase().includes(q)).length : 0
            const open = isOpen(s)
            return (
              <div key={s.name} className="border-b border-[#21262d] last:border-0">
                <button
                  type="button"
                  onClick={() => toggle(s)}
                  className="w-full flex items-center gap-2 px-3 sm:px-4 py-2 hover:bg-[#161b22] text-left sticky top-0 bg-[#0d1117] z-10 border-b border-[#21262d]"
                >
                  <ChevronDown size={13} className={`text-[#8b949e] shrink-0 transition-transform ${open ? '' : '-rotate-90'}`} />
                  <StatusIcon status={s.status} size={13} />
                  <span className="font-mono text-sm text-[#c9d1d9]">{s.name}</span>
                  {s.startedAt && s.finishedAt && (
                    <span className="text-[11px] text-[#484f58]">{formatDuration(s.startedAt, s.finishedAt)}</span>
                  )}
                  <span className="ml-auto text-[11px] text-[#484f58]">
                    {q ? `${matches} match${matches === 1 ? '' : 'es'}` : `${lines.length} line${lines.length === 1 ? '' : 's'}`}
                  </span>
                </button>
                {open && (
                  <div className="px-3 sm:px-4 py-2 font-mono text-[13px] leading-[1.65]">
                    <LogView entries={lines.map((line) => ({ content: line }))} search={q} />
                  </div>
                )}
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Log panel — replaces overview when a step is selected
// ---------------------------------------------------------------------------

// StepActions exposes operator controls on the selected step: retry the whole run
// from this step (on a finished run), or force-resolve a wedged non-terminal step.
const STEP_TERMINAL = ['succeeded', 'failed', 'skipped', 'cancelled']

function StepActions({ runId, runStatus, step }: { runId: string; runStatus: string; step: any }) {
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
          className="flex items-center gap-1.5 rounded-md border border-border px-2.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-primary hover:border-primary/30 transition-colors disabled:opacity-50"
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

function LogPanel({
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

      <div className="flex items-center gap-1 px-3 sm:px-4 py-1.5 border-b border-border shrink-0 bg-muted/20">
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

function PanelTab({ icon, label, active, onClick }: {
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

function FailureBanner({ step, onViewLogs }: { step: any; onViewLogs: () => void }) {
  return (
    <div className="island-shell !p-0 overflow-hidden border-destructive/30 mb-4 lg:mb-5">
      <div className="flex items-center gap-3 px-4 py-3 bg-destructive/5">
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
          className="shrink-0 flex items-center gap-1.5 rounded-lg border border-destructive/30 px-3 py-1.5 text-xs font-medium text-destructive hover:bg-destructive/10 transition-colors"
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

function triggerLabel(type: string): string {
  switch (type) {
    case 'push': return 'Push'
    case 'pull_request': return 'Pull request'
    case 'manual': return 'Manual'
    case 'schedule': return 'Scheduled'
    default: return type
  }
}

function TriggerIcon({ type }: { type: string }) {
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
    paused: {
      icon: <Pause size={12} />,
      label: 'Paused',
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
