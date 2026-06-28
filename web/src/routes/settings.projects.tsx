import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { FolderGit2, Plus, Pencil, RotateCcw } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { PageHeader } from '#/components/PageHeader'
import { EmptyState } from '#/components/EmptyState'
import { ConfirmButton } from '#/components/ConfirmButton'
import { NewProjectModal } from '#/components/NewProjectModal'
import { EditProjectModal } from '#/components/ProjectSettings'

export const Route = createFileRoute('/settings/projects')({
  component: ProjectsAdminPage,
})

const invalidateProjects = () => [orpc.projects.list.key(), orpc.projects.archived.key()]

function ProjectsAdminPage() {
  const [showNew, setShowNew] = useState(false)
  const [editing, setEditing] = useState<{ id: string; name: string; colour: string; workspace: string } | null>(null)

  const { data: list } = useSuspenseQuery(orpc.projects.list.queryOptions({ input: { limit: 200 } }))
  const { data: archived } = useSuspenseQuery(orpc.projects.archived.queryOptions({ input: {} }))
  const active = list.items
  const archivedItems = archived.items

  const archive = useAction(client.projects.archive, { invalidate: invalidateProjects() })
  const restore = useAction(client.projects.restore, { invalidate: invalidateProjects() })

  return (
    <div className="space-y-6">
      {showNew && <NewProjectModal onClose={() => setShowNew(false)} />}
      {editing && <EditProjectModal projectId={editing.id} initial={editing} onClose={() => setEditing(null)} />}

      <PageHeader
        title="Projects"
        subtitle={`${active.length} active${archivedItems.length ? ` · ${archivedItems.length} archived` : ''}`}
        action={
          <button
            type="button"
            onClick={() => setShowNew(true)}
            className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium text-white transition-colors"
            style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
          >
            <Plus size={14} />
            New project
          </button>
        }
      />

      {active.length === 0 ? (
        <EmptyState icon={FolderGit2} message="No projects yet. Register a repository to get started." />
      ) : (
        <div className="island-shell !p-0 overflow-hidden">
          <div className="hidden sm:grid sm:grid-cols-[1.4fr_1fr_auto] gap-3 px-4 py-2.5 border-b border-border bg-muted/30 text-[12px] font-medium text-muted-foreground uppercase tracking-wider">
            <span>Project</span>
            <span>Workspace</span>
            <span className="text-right">Actions</span>
          </div>
          <div className="divide-y divide-border">
            {active.map((p) => (
              <div key={p.id} className="px-4 py-3 sm:grid sm:grid-cols-[1.4fr_1fr_auto] sm:gap-3 sm:items-center space-y-1 sm:space-y-0">
                <div className="flex items-center gap-3 min-w-0">
                  <span className="w-2.5 h-2.5 rounded-full shrink-0" style={{ backgroundColor: p.colour }} />
                  <div className="min-w-0">
                    <p className="text-sm font-medium text-foreground truncate">{p.name}</p>
                    <p className="text-xs text-muted-foreground font-mono truncate">{p.repo}</p>
                  </div>
                </div>
                <span className="text-xs text-muted-foreground">{p.workspace || '—'}</span>
                <div className="flex items-center justify-end gap-1.5">
                  <button
                    type="button"
                    title="Edit project"
                    onClick={() => setEditing({ id: p.id, name: p.name, colour: p.colour, workspace: p.workspace })}
                    className="flex items-center justify-center h-7 w-7 rounded-lg border border-border text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
                  >
                    <Pencil size={13} />
                  </button>
                  <ConfirmButton onConfirm={() => archive.mutate({ id: p.id })} title="Archive project" />
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {archivedItems.length > 0 && (
        <div className="space-y-2">
          <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Archived</h3>
          <div className="island-shell !p-0 overflow-hidden">
            <div className="divide-y divide-border">
              {archivedItems.map((p) => (
                <div key={p.id} className="flex items-center gap-3 px-4 py-3">
                  <span className="w-2.5 h-2.5 rounded-full shrink-0 opacity-50" style={{ backgroundColor: p.colour }} />
                  <div className="min-w-0 flex-1">
                    <p className="text-sm font-medium text-muted-foreground truncate">{p.name}</p>
                    <p className="text-xs text-muted-foreground/70 font-mono truncate">{p.repo}</p>
                  </div>
                  <button
                    type="button"
                    onClick={() => restore.mutate({ id: p.id })}
                    disabled={restore.isPending}
                    className="flex items-center gap-1.5 rounded-lg border border-border px-2.5 py-1 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
                  >
                    <RotateCcw size={12} />
                    Restore
                  </button>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
