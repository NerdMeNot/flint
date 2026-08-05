import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { ArrowLeft, Workflow, Calendar, Hand, Webhook, Clock, User, Loader2, Network } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { DagView } from '#/components/pipeline/dag-view'
import { RunStatusPill } from '#/components/RunStatusPill'
import { formatTime } from '#/lib/format-time'
import type { PipelineStep } from '#/lib/api/types'

export const Route = createFileRoute('/workflows/$id')({
  component: WorkflowRunPage,
})

const triggerIcon: Record<string, React.ReactNode> = {
  schedule: <Calendar size={13} />,
  manual: <Hand size={13} />,
  api: <Webhook size={13} />,
}

function WorkflowRunPage() {
  const { id } = Route.useParams()
  const { data: run, isLoading } = useQuery(orpc.workflows.get.queryOptions({ input: { id } }))

  return (
    <div className="space-y-5 rise-in">
      <Link
        to="/workflows"
        className="inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors"
      >
        <ArrowLeft size={13} /> Workflows
      </Link>

      {isLoading ? (
        <div className="flex items-center gap-2 px-1 py-16 text-sm text-muted-foreground">
          <Loader2 size={15} className="animate-spin" /> Loading run…
        </div>
      ) : !run ? (
        <NotFound id={id} />
      ) : (
        <>
          <header className="island-shell p-4 sm:p-5 lg:p-6">
            <div className="flex flex-wrap items-center gap-3">
              <Workflow size={20} className="text-primary shrink-0" />
              <h1 className="display-title text-2xl lg:text-3xl font-bold text-foreground truncate">{run.name}</h1>
              <RunStatusPill status={run.status} />
            </div>
            <div className="mt-3 flex flex-wrap items-center gap-x-5 gap-y-1.5 text-xs text-muted-foreground">
              <span className="font-mono opacity-70">{run.id}</span>
              <span className="flex items-center gap-1.5">
                {triggerIcon[run.triggerType]}<span className="capitalize">{run.triggerType}</span>
              </span>
              <span className="flex items-center gap-1.5"><User size={13} />{run.triggeredBy}</span>
              <span className="flex items-center gap-1.5"><Clock size={13} />{formatTime(run.startedAt)}</span>
              <span className="flex items-center gap-1.5"><Clock size={13} />{run.duration}</span>
            </div>
          </header>

          {/* DAG */}
          <section className="island-shell !p-0 overflow-hidden">
            <div className="flex items-center gap-2 px-5 py-3 border-b border-border">
              <Network size={15} className="text-muted-foreground" />
              <h2 className="text-sm font-semibold text-foreground">Execution graph</h2>
              <span className="text-xs text-muted-foreground opacity-60">{run.steps.length} steps</span>
            </div>
            <div className="h-[440px]">
              <DagView steps={run.steps} direction="RIGHT" />
            </div>
          </section>

          {/* Steps table */}
          <StepsTable steps={run.steps} />
        </>
      )}
    </div>
  )
}

function StepsTable({ steps }: { steps: PipelineStep[] }) {
  return (
    <section className="island-shell !p-0 overflow-hidden">
      <div className="px-5 py-3 border-b border-border">
        <h2 className="text-sm font-semibold text-foreground">Steps</h2>
      </div>
      <div className="divide-y divide-border">
        {steps.map((step) => (
          <div key={step.name} className="grid grid-cols-[1fr_auto] sm:grid-cols-[1fr_110px_120px_120px] items-center gap-3 px-5 py-3">
            <div className="min-w-0">
              <span className="font-medium text-foreground truncate">{step.name}</span>
              {step.dependsOn && step.dependsOn.length > 0 && (
                <p className="mt-0.5 text-[12px] text-muted-foreground truncate">
                  needs {step.dependsOn.join(', ')}
                </p>
              )}
            </div>
            <div className="hidden sm:block"><RunStatusPill status={step.status} size="sm" /></div>
            <div className="hidden sm:block text-xs text-muted-foreground">
              <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px]">{step.execType}</span>
            </div>
            <div className="hidden sm:block text-xs text-muted-foreground font-mono">{stepDuration(step)}</div>
            <div className="sm:hidden"><RunStatusPill status={step.status} size="sm" /></div>
          </div>
        ))}
      </div>
    </section>
  )
}

function stepDuration(step: PipelineStep): string {
  if (!step.startedAt) return '—'
  if (!step.finishedAt) return 'running'
  const ms = new Date(step.finishedAt).getTime() - new Date(step.startedAt).getTime()
  if (isNaN(ms) || ms < 0) return '—'
  const s = Math.round(ms / 1000)
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`
}

function NotFound({ id }: { id: string }) {
  return (
    <div className="rounded-xl border border-dashed border-border bg-card px-6 py-16 text-center">
      <p className="text-sm font-medium text-foreground">Run not found</p>
      <p className="mx-auto mt-1 max-w-md text-sm text-muted-foreground">
        No workflow run <code className="rounded bg-muted px-1.5 py-0.5 text-xs">{id}</code> is available.
        It may still be starting, or this is a freshly triggered run in mock mode.
      </p>
      <Link
        to="/workflows"
        className="mt-5 inline-flex items-center gap-1.5 rounded-lg border border-border px-3.5 py-2 text-sm font-medium text-muted-foreground hover:text-foreground transition-colors"
      >
        <ArrowLeft size={15} /> Back to workflows
      </Link>
    </div>
  )
}
