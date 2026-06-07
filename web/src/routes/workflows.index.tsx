import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { useSuspenseQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Workflow, Plus, ChevronRight, Calendar, Hand, Webhook } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { RunStatusPill } from '#/components/RunStatusPill'
import { WorkflowTriggerModal } from '#/components/WorkflowTriggerModal'
import { formatTime } from '#/lib/format-time'

export const Route = createFileRoute('/workflows/')({
  component: WorkflowsPage,
})

const triggerIcon: Record<string, React.ReactNode> = {
  schedule: <Calendar size={12} />,
  manual: <Hand size={12} />,
  api: <Webhook size={12} />,
}

function WorkflowsPage() {
  const { data } = useSuspenseQuery(orpc.workflows.list.queryOptions({ input: { limit: 50 } }))
  const [showTrigger, setShowTrigger] = useState(false)
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const runs = data.items

  return (
    <div className="space-y-6 rise-in">
      <header className="flex items-center justify-between gap-4">
        <div>
          <h1 className="display-title text-2xl lg:text-3xl font-bold text-foreground tracking-tight">Workflows</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Generic declarative workflows on the Flint engine — no repository required.
          </p>
        </div>
        <button
          type="button"
          onClick={() => setShowTrigger(true)}
          className="inline-flex items-center gap-1.5 rounded-lg px-3.5 py-2 text-sm font-medium text-white shadow-sm shrink-0"
          style={{ background: 'linear-gradient(135deg, var(--primary), color-mix(in oklab, var(--primary), black 12%))' }}
        >
          <Plus size={16} strokeWidth={2.2} /> New run
        </button>
      </header>

      {runs.length === 0 ? (
        <EmptyState onNew={() => setShowTrigger(true)} />
      ) : (
        <div className="island-shell !p-0 overflow-hidden">
          <div className="hidden sm:grid grid-cols-[1fr_120px_140px_120px_40px] gap-3 px-5 py-2.5 border-b border-border text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground/45">
            <span>Workflow</span>
            <span>Status</span>
            <span>Trigger</span>
            <span>Duration</span>
            <span />
          </div>
          <div className="divide-y divide-border">
            {runs.map((run) => (
              <Link
                key={run.id}
                to="/workflows/$id"
                params={{ id: run.id }}
                className="group grid grid-cols-[1fr_auto] sm:grid-cols-[1fr_120px_140px_120px_40px] items-center gap-3 px-5 py-3.5 hover:bg-[var(--link-bg-hover)] transition-colors"
              >
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <Workflow size={15} className="text-primary shrink-0" />
                    <span className="font-medium text-foreground truncate">{run.name}</span>
                  </div>
                  <div className="mt-0.5 flex items-center gap-2 text-[12px] text-muted-foreground">
                    <span className="font-mono opacity-70">{run.id}</span>
                    <span className="opacity-40">·</span>
                    <span>{run.stepCount} steps</span>
                    <span className="opacity-40">·</span>
                    <span>{formatTime(run.startedAt)}</span>
                  </div>
                </div>
                <div className="hidden sm:block"><RunStatusPill status={run.status} /></div>
                <div className="hidden sm:flex items-center gap-1.5 text-xs text-muted-foreground">
                  <span className="opacity-70">{triggerIcon[run.triggerType]}</span>
                  <span className="capitalize">{run.triggerType}</span>
                  <span className="opacity-40 truncate">· {run.triggeredBy}</span>
                </div>
                <div className="hidden sm:block text-xs text-muted-foreground font-mono">{run.duration}</div>
                <div className="flex items-center justify-end sm:justify-center">
                  {/* Mobile shows status inline since the columns collapse */}
                  <span className="sm:hidden mr-2"><RunStatusPill status={run.status} size="sm" /></span>
                  <ChevronRight size={15} className="text-muted-foreground/40 group-hover:text-foreground transition-colors shrink-0" />
                </div>
              </Link>
            ))}
          </div>
        </div>
      )}

      <WorkflowTriggerModal
        open={showTrigger}
        onClose={() => setShowTrigger(false)}
        onTriggered={(runId) => {
          setShowTrigger(false)
          queryClient.invalidateQueries({ queryKey: orpc.workflows.list.key() })
          navigate({ to: '/workflows/$id', params: { id: runId } })
        }}
      />
    </div>
  )
}

function EmptyState({ onNew }: { onNew: () => void }) {
  return (
    <div className="rounded-xl border border-dashed border-border bg-card/40 px-6 py-16 text-center">
      <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-xl"
        style={{ background: 'color-mix(in oklab, var(--primary) 16%, transparent)', color: 'var(--primary)' }}>
        <Workflow size={22} strokeWidth={1.8} />
      </div>
      <p className="mt-4 text-sm font-medium text-foreground">No workflow runs yet</p>
      <p className="mx-auto mt-1 max-w-md text-sm text-muted-foreground">
        Run a workflow by pasting a YAML definition, or trigger one via{' '}
        <code className="rounded bg-muted px-1.5 py-0.5 text-xs">POST /api/v1/workflows/runs</code>.
      </p>
      <button
        type="button"
        onClick={onNew}
        className="mt-5 inline-flex items-center gap-1.5 rounded-lg px-3.5 py-2 text-sm font-medium text-white shadow-sm"
        style={{ background: 'linear-gradient(135deg, var(--primary), color-mix(in oklab, var(--primary), black 12%))' }}
      >
        <Plus size={16} strokeWidth={2.2} /> New run
      </button>
    </div>
  )
}
