import { createFileRoute, Link } from '@tanstack/react-router'
import { useSuspenseQuery, useQuery } from '@tanstack/react-query'
import { useState, lazy, Suspense } from 'react'
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
  ArrowLeft,
  Network,
  List,
  Ban,
  RotateCcw,
} from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { client } from '#/lib/orpc'
import { PipelineProgress } from '#/components/PipelineProgress'
import { StepTimeline } from '#/components/pipeline/step-timeline'

const DagView = lazy(() =>
  import('#/components/pipeline/dag-view').then((m) => ({ default: m.DagView }))
)

export const Route = createFileRoute('/runs/$id')({
  component: RunDetailPage,
})

function RunDetailPage() {
  const { id } = Route.useParams()
  const [selectedStep, setSelectedStep] = useState<string | null>(null)
  const [view, setView] = useState<'timeline' | 'dag'>('timeline')

  const { data: run } = useSuspenseQuery(
    orpc.runs.get.queryOptions({ input: { id } }),
  )
  const { data: steps } = useSuspenseQuery(
    orpc.runs.steps.queryOptions({ input: { runId: id } }),
  )
  const { data: logsData } = useQuery({
    ...orpc.runs.stepLogs.queryOptions({
      input: { runId: id, stepName: selectedStep ?? '' },
    }),
    enabled: selectedStep !== null,
  })

  const step = selectedStep
    ? steps.find((s) => s.name === selectedStep)
    : null

  function handleStepClick(name: string) {
    setSelectedStep(name === selectedStep ? null : name)
  }

  return (
    <div className="rise-in">
      {/* Run Header */}
      <div className="island-shell p-4 sm:p-5 mb-5">
        <div className="space-y-2">
          <div className="flex flex-wrap items-center gap-2 sm:gap-3">
            <StatusBadge status={run.status} />
            <Link
              to="/projects/$id"
              params={{ id: run.projectId }}
              className="display-title text-lg sm:text-xl font-bold text-foreground hover:text-primary transition-colors truncate"
            >
              {run.projectName}
            </Link>
            <span className="text-xs text-muted-foreground font-mono opacity-60">
              {run.workflowFile}
            </span>
          </div>
          <p className="text-sm text-muted-foreground flex items-center gap-2">
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
          <span className="island-kicker !text-[0.58rem]">{run.triggerType}</span>

          {/* Actions */}
          <div className="flex items-center gap-2 ml-auto">
            {(run.status === 'running' || run.status === 'pending') && (
              <button
                type="button"
                onClick={() => client.runs.cancel({ runId: run.id })}
                className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-destructive hover:border-destructive/30 transition-colors"
              >
                <Ban size={12} />
                Cancel
              </button>
            )}
            {(run.status === 'failed' || run.status === 'cancelled') && (
              <button
                type="button"
                onClick={() => client.runs.retry({ runId: run.id })}
                className="flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-primary hover:border-primary/30 transition-colors"
              >
                <RotateCcw size={12} />
                Retry
              </button>
            )}
          </div>
        </div>
      </div>

      {/* Pipeline progress */}
      <div className="mb-4">
        <PipelineProgress steps={steps} />
      </div>

      {/* View toggle + summary */}
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <h2 className="font-semibold text-sm text-foreground">Pipeline</h2>
          <span className="text-xs text-muted-foreground">
            {steps.filter((s) => s.status === 'succeeded').length}/{steps.length} complete
          </span>
        </div>
        <div className="flex items-center rounded-lg border border-border p-0.5"
          style={{ background: 'var(--surface)' }}>
          <button
            type="button"
            onClick={() => setView('timeline')}
            className={`flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium whitespace-nowrap transition-all duration-150 ${
              view === 'timeline'
                ? 'text-white shadow-sm'
                : 'text-muted-foreground hover:text-foreground'
            }`}
            style={view === 'timeline' ? { background: 'color-mix(in oklab, var(--ring), black 35%)' } : undefined}
          >
            <List size={13} />
            <span className="hidden sm:inline">Timeline</span>
          </button>
          <button
            type="button"
            onClick={() => setView('dag')}
            className={`flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium whitespace-nowrap transition-all duration-150 ${
              view === 'dag'
                ? 'text-white shadow-sm'
                : 'text-muted-foreground hover:text-foreground'
            }`}
            style={view === 'dag' ? { background: 'color-mix(in oklab, var(--ring), black 35%)' } : undefined}
          >
            <Network size={13} />
            <span className="hidden sm:inline">DAG</span>
          </button>
        </div>
      </div>

      {/* Content: either steps view OR full-width log viewer */}
      {selectedStep && step ? (
        /* -- Full-width log viewer -- */
        <div className="island-shell !p-0 overflow-hidden flex flex-col">
          {/* Log header with back + step info + nav */}
          <div className="flex items-center justify-between px-4 py-3 border-b border-border shrink-0">
            <div className="flex items-center gap-3 min-w-0">
              <button
                type="button"
                onClick={() => setSelectedStep(null)}
                className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors shrink-0"
              >
                <ArrowLeft size={14} />
                <span className="hidden sm:inline">All steps</span>
              </button>
              <span className="text-border opacity-40">|</span>
              <StatusIcon status={step.status} size={16} />
              <span className="font-semibold text-sm text-foreground truncate">{step.name}</span>
              <StatusBadge status={step.status} />
              {step.finishedAt && step.startedAt && (
                <span className="hidden sm:flex items-center gap-1 text-xs text-muted-foreground whitespace-nowrap">
                  <Timer size={11} />
                  {formatDuration(step.startedAt, step.finishedAt)}
                </span>
              )}
            </div>

            {/* Prev / Next */}
            <div className="flex items-center gap-1 shrink-0">
              {(() => {
                const idx = steps.findIndex((s) => s.name === selectedStep)
                const prev = idx > 0 ? steps[idx - 1] : null
                const next = idx < steps.length - 1 ? steps[idx + 1] : null
                return (
                  <>
                    <button
                      type="button"
                      onClick={() => prev && setSelectedStep(prev.name)}
                      disabled={!prev}
                      className="px-2 py-1 rounded text-xs text-muted-foreground hover:text-foreground hover:bg-accent disabled:opacity-25 disabled:pointer-events-none transition-colors"
                    >
                      {prev ? `\u2190 ${prev.name}` : ''}
                    </button>
                    <button
                      type="button"
                      onClick={() => next && setSelectedStep(next.name)}
                      disabled={!next}
                      className="px-2 py-1 rounded text-xs text-muted-foreground hover:text-foreground hover:bg-accent disabled:opacity-25 disabled:pointer-events-none transition-colors"
                    >
                      {next ? `${next.name} \u2192` : ''}
                    </button>
                  </>
                )
              })()}
            </div>
          </div>

          {/* Step bar -- all steps visible, scrollable */}
          <div className="flex items-center gap-1 px-4 py-2 border-b border-border overflow-x-auto shrink-0">
            {steps.map((s) => (
              <button
                key={s.name}
                type="button"
                onClick={() => setSelectedStep(s.name)}
                title={`${s.name} \u2014 ${s.status}`}
                className={`flex items-center gap-1.5 shrink-0 rounded-md px-2 py-1 text-[0.7rem] font-medium transition-all ${
                  s.name === selectedStep
                    ? 'bg-primary/15 text-primary ring-1 ring-primary/20'
                    : 'text-muted-foreground hover:text-foreground hover:bg-accent'
                }`}
              >
                <StepDot status={s.status} />
                {s.name}
              </button>
            ))}
          </div>

          {/* Output label */}
          <div className="flex items-center gap-1.5 px-4 py-2 border-b border-border shrink-0">
            <Terminal size={13} className="text-primary" />
            <span className="text-xs font-semibold text-foreground">Output</span>
          </div>

          {/* Full-width log output */}
          <div className="bg-[#0d1117] p-4 sm:p-5 font-mono text-xs sm:text-[0.82rem] leading-relaxed text-[#c9d1d9] min-h-[400px] sm:min-h-[500px] max-h-[75vh] overflow-auto">
            {logsData?.lines ? (
              logsData.lines.split('\n').map((line, i) => (
                <div key={i} className="flex gap-4 hover:bg-[#161b22] -mx-2 px-2 py-px rounded">
                  <span className="text-[#484f58] select-none shrink-0 w-7 text-right">
                    {i + 1}
                  </span>
                  <span className={colorizeLine(line)}>
                    {line}
                  </span>
                </div>
              ))
            ) : (
              <span className="text-[#484f58] italic">
                {step.status === 'pending'
                  ? 'Step has not started yet.'
                  : step.status === 'running'
                    ? 'Waiting for output...'
                    : 'No logs available.'}
              </span>
            )}
          </div>
        </div>
      ) : (
        /* -- Steps view (timeline or DAG) -- */
        <div>
          {view === 'timeline' ? (
            <StepTimeline
              steps={steps}
              selectedStep={selectedStep}
              onStepClick={handleStepClick}
            />
          ) : (
            <div className="island-shell !p-0 overflow-hidden h-[400px] sm:h-[500px]">
              <Suspense fallback={<div className="h-full flex items-center justify-center text-muted-foreground text-sm">Loading DAG...</div>}>
                <DagView
                  steps={steps}
                  onStepClick={handleStepClick}
                />
              </Suspense>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

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
    <span className={`inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[0.65rem] font-semibold whitespace-nowrap ${c.className}`}>
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

function StepDot({ status }: { status: string }) {
  const colors: Record<string, string> = {
    succeeded: 'bg-success',
    failed: 'bg-destructive',
    running: 'bg-primary animate-pulse',
    waiting: 'bg-warning',
    queued: 'bg-purple-400',
    pending: 'bg-muted-foreground opacity-30',
    skipped: 'bg-muted-foreground opacity-30',
    cancelled: 'bg-muted-foreground opacity-30',
  }
  return <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${colors[status] ?? colors.pending}`} />
}

function formatDuration(start: string, end: string): string {
  const ms = new Date(end).getTime() - new Date(start).getTime()
  const seconds = Math.floor(ms / 1000)
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}
