import { createFileRoute } from '@tanstack/react-router'
import { useState, lazy, Suspense } from 'react'
import {
  GitBranch,
  GitCommit,
  Clock,
  User,
  CheckCircle,
  XCircle,
  Loader2,
  ChevronRight,
  Terminal,
} from 'lucide-react'
import { mockRuns, mockPipelineSteps, mockStepLogs } from '#/lib/mock-data'

const DagView = lazy(() =>
  import('#/components/pipeline/dag-view').then((m) => ({ default: m.DagView }))
)

export const Route = createFileRoute('/runs/$id')({
  component: RunDetailPage,
})

function RunDetailPage() {
  const { id } = Route.useParams()
  const [selectedStep, setSelectedStep] = useState<string | null>(null)

  const run = mockRuns.find((r) => r.id === id) ?? mockRuns[2]! // default to running run

  return (
    <div className="space-y-6 max-w-7xl rise-in">
      {/* Run Header */}
      <div className="island-shell p-5">
        <div className="flex items-start justify-between">
          <div className="space-y-2">
            <div className="flex items-center gap-3">
              <RunStatusBadge status={run.status} />
              <h1 className="display-title text-xl font-bold text-foreground">
                {run.projectName}
              </h1>
              <span className="text-xs text-muted-foreground font-mono opacity-60">
                {run.workflowFile}
              </span>
            </div>
            <p className="text-sm text-muted-foreground flex items-center gap-2">
              <GitCommit size={14} />
              {run.commitMessage}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-5 mt-4 text-xs text-muted-foreground">
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
        </div>
      </div>

      {/* DAG Visualization */}
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h2 className="font-semibold text-sm text-foreground">
            Pipeline DAG
          </h2>
          <span className="island-kicker !text-[0.6rem]">
            {mockPipelineSteps.length} steps
          </span>
        </div>

        <div className="island-shell !p-0 overflow-hidden rounded-2xl">
          <Suspense fallback={<div className="h-[400px] flex items-center justify-center text-muted-foreground">Loading DAG...</div>}>
            <DagView
              steps={mockPipelineSteps}
              onStepClick={(name) => setSelectedStep(name)}
            />
          </Suspense>
        </div>
      </div>

      {/* Steps List + Log Viewer */}
      <div className="grid gap-4 lg:grid-cols-[320px_1fr]">
        {/* Steps List */}
        <div className="island-shell !p-0 overflow-hidden">
          <div className="px-4 py-3 border-b border-border">
            <h3 className="font-semibold text-sm text-foreground">Steps</h3>
          </div>
          <div className="divide-y divide-[var(--line)]">
            {mockPipelineSteps.map((step) => (
              <button
                key={step.name}
                onClick={() => setSelectedStep(step.name)}
                className={`w-full flex items-center gap-3 px-4 py-2.5 text-left transition-colors ${
                  selectedStep === step.name
                    ? 'bg-primary/10'
                    : 'hover:bg-accent'
                }`}
              >
                <StepStatusDot status={step.status} />
                <div className="flex-1 min-w-0">
                  <span className="text-sm font-medium text-foreground block truncate">
                    {step.name}
                  </span>
                  <span className="text-[0.65rem] text-muted-foreground">
                    {step.execType}{step.finishedAt && step.startedAt
                      ? ` \u00B7 ${formatDuration(step.startedAt, step.finishedAt)}`
                      : step.status === 'running'
                        ? ' \u00B7 running...'
                        : ''}
                  </span>
                </div>
                <ChevronRight size={14} className="text-muted-foreground opacity-40 shrink-0" />
              </button>
            ))}
          </div>
        </div>

        {/* Log Viewer */}
        <div className="island-shell !p-0 overflow-hidden">
          <div className="flex items-center gap-2 px-4 py-3 border-b border-border">
            <Terminal size={14} className="text-primary" />
            <h3 className="font-semibold text-sm text-foreground">
              {selectedStep ? selectedStep : 'Select a step'}
            </h3>
          </div>
          <div className="bg-[#0d1117] p-4 font-mono text-xs leading-relaxed text-[#c9d1d9] min-h-[300px] max-h-[440px] overflow-auto">
            {selectedStep && mockStepLogs[selectedStep] ? (
              mockStepLogs[selectedStep].split('\n').map((line, i) => (
                <div key={i} className="flex gap-3 hover:bg-[#161b22] -mx-1 px-1 rounded">
                  <span className="text-[#484f58] select-none shrink-0 w-5 text-right">
                    {i + 1}
                  </span>
                  <span className={
                    line.includes('PASS') || line.includes('complete') || line.includes('saved')
                      ? 'text-[#7ee787]'
                      : line.includes('Error') || line.includes('FAIL')
                        ? 'text-[#ff7b72]'
                        : line.includes('Warning') || line.includes('deprecated')
                          ? 'text-[#d29922]'
                          : line.startsWith('[')
                            ? 'text-[#c9d1d9]'
                            : 'text-[#8b949e]'
                  }>
                    {line}
                  </span>
                </div>
              ))
            ) : selectedStep ? (
              <span className="text-[#484f58] italic">
                {mockPipelineSteps.find((s) => s.name === selectedStep)?.status === 'pending'
                  ? 'Step has not started yet.'
                  : 'No logs available.'}
              </span>
            ) : (
              <span className="text-[#484f58] italic">
                Click a step to view its logs.
              </span>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

function RunStatusBadge({ status }: { status: string }) {
  const config: Record<string, { icon: React.ReactNode; label: string; className: string }> = {
    succeeded: {
      icon: <CheckCircle size={14} />,
      label: 'Succeeded',
      className: 'bg-success/10 text-success border-success/20',
    },
    failed: {
      icon: <XCircle size={14} />,
      label: 'Failed',
      className: 'bg-red-500/10 text-red-500 border-red-500/20',
    },
    running: {
      icon: <Loader2 size={14} className="animate-spin" />,
      label: 'Running',
      className: 'bg-primary/10 text-primary border-primary/20',
    },
    pending: {
      icon: <Clock size={14} />,
      label: 'Pending',
      className: 'bg-[var(--chip-bg)] text-muted-foreground border-border',
    },
    cancelled: {
      icon: <Clock size={14} />,
      label: 'Cancelled',
      className: 'bg-[var(--chip-bg)] text-muted-foreground border-border',
    },
  }

  const c = config[status] ?? config.pending!

  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-semibold ${c.className}`}>
      {c.icon}
      {c.label}
    </span>
  )
}

function StepStatusDot({ status }: { status: string }) {
  const colors: Record<string, string> = {
    succeeded: 'bg-success',
    failed: 'bg-red-500',
    running: 'bg-primary animate-pulse',
    waiting: 'bg-amber-400',
    queued: 'bg-purple-400',
    pending: 'bg-[var(--sea-ink-soft)] opacity-30',
    skipped: 'bg-[var(--sea-ink-soft)] opacity-30',
    cancelled: 'bg-[var(--sea-ink-soft)] opacity-30',
  }

  return (
    <span className={`w-2 h-2 rounded-full shrink-0 ${colors[status] ?? colors.pending}`} />
  )
}

function formatDuration(start: string, end: string): string {
  const ms = new Date(end).getTime() - new Date(start).getTime()
  const seconds = Math.floor(ms / 1000)
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}
