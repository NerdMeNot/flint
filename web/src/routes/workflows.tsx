import { createFileRoute } from '@tanstack/react-router'
import { Workflow, Plus } from 'lucide-react'

export const Route = createFileRoute('/workflows')({
  component: WorkflowsPage,
})

function WorkflowsPage() {
  return (
    <div className="space-y-6">
      <header className="flex items-center justify-between gap-4">
        <div>
          <h1 className="display-title text-2xl font-bold text-foreground tracking-tight">Workflows</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            Generic declarative workflows on the Flint engine — no repository required.
          </p>
        </div>
        <button
          type="button"
          className="inline-flex items-center gap-1.5 rounded-lg px-3 py-2 text-sm font-medium text-white shadow-sm"
          style={{ background: 'linear-gradient(135deg, var(--primary), color-mix(in oklab, var(--primary), black 12%))' }}
        >
          <Plus size={16} strokeWidth={2.2} /> New run
        </button>
      </header>

      <div className="rounded-xl border border-dashed border-border bg-card/40 px-6 py-16 text-center">
        <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-xl"
          style={{ background: 'color-mix(in oklab, var(--primary) 16%, transparent)', color: 'var(--primary)' }}>
          <Workflow size={22} strokeWidth={1.8} />
        </div>
        <p className="mt-4 text-sm font-medium text-foreground">No workflow runs yet</p>
        <p className="mx-auto mt-1 max-w-md text-sm text-muted-foreground">
          Trigger one with <code className="rounded bg-muted px-1.5 py-0.5 text-xs">POST /api/v1/workflows/runs</code>{' '}
          or set up a cron schedule. The runs list and DAG view land here next.
        </p>
      </div>
    </div>
  )
}
