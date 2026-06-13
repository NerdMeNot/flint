import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Boxes, FolderGit2, Calendar, Plus } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { formatTime } from '#/lib/format-time'
import { PageHeader } from '#/components/PageHeader'
import { Badge } from '#/components/Badge'
import { EmptyState } from '#/components/EmptyState'
import { ConfirmButton } from '#/components/ConfirmButton'
import { Modal } from '#/components/Modal'

export const Route = createFileRoute('/settings/workspaces')({
  component: WorkspacesPage,
})

function WorkspacesPage() {
  const { data: wsData } = useSuspenseQuery(orpc.workspaces.list.queryOptions({ input: {} }))
  const workspaces = wsData.items
  const [showCreate, setShowCreate] = useState(false)

  const del = useAction((id: string) => client.workspaces.delete({ id }), {
    invalidate: [orpc.workspaces.list.key()],
  })

  return (
    <div className="space-y-5">
      <PageHeader
        title="Workspaces"
        subtitle={`${workspaces.length} ${workspaces.length === 1 ? 'workspace' : 'workspaces'} configured`}
        action={
          <button
            type="button"
            onClick={() => setShowCreate(true)}
            className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-colors"
            style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
          >
            <Plus size={12} /> Add workspace
          </button>
        }
      />

      <GroupingPolicy />

      {workspaces.length === 0 ? (
        <EmptyState icon={Boxes} message="No workspaces configured yet." />
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {workspaces.map((ws, i) => (
            <div
              key={ws.id}
              className="feature-card rise-in p-5 space-y-3"
              style={{ animationDelay: `${i * 50 + 30}ms` }}
            >
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <h3 className="font-semibold text-sm text-foreground truncate">{ws.name}</h3>
                    {ws.isDefault && <Badge variant="primary">Default</Badge>}
                  </div>
                  <p className="text-xs text-muted-foreground font-mono truncate">{ws.slug}</p>
                </div>
                {/* The org default can't be deleted (it's where new projects land). */}
                {!ws.isDefault && <ConfirmButton onConfirm={() => del.mutate(ws.id)} title="Delete workspace" />}
              </div>

              {ws.description && (
                <p className="text-sm text-muted-foreground line-clamp-2">{ws.description}</p>
              )}

              <div className="flex items-center gap-4 text-xs text-muted-foreground pt-1">
                <span className="flex items-center gap-1.5">
                  <FolderGit2 size={12} />
                  {ws.projectCount} {ws.projectCount === 1 ? 'project' : 'projects'}
                </span>
                <span className="flex items-center gap-1.5">
                  <Calendar size={12} />
                  {formatTime(ws.createdAt)}
                </span>
              </div>
            </div>
          ))}
        </div>
      )}

      {showCreate && <CreateWorkspaceModal onClose={() => setShowCreate(false)} />}
    </div>
  )
}

// Org-level governance: require every project to declare a workspace.
function GroupingPolicy() {
  const { data: org } = useSuspenseQuery(orpc.org.get.queryOptions({}))
  const set = useAction(
    (requireProjectWorkspace: boolean) => client.org.setPolicy({ requireProjectWorkspace }),
    { invalidate: [orpc.org.get.key()] },
  )
  const on = org.requireProjectWorkspace

  return (
    <div className="island-shell p-4 flex items-center justify-between gap-4">
      <div className="min-w-0">
        <p className="text-sm font-medium text-foreground">Require a workspace on every project</p>
        <p className="text-xs text-muted-foreground mt-0.5">
          When on, a project must declare <span className="font-mono">spec.workspace</span> — those without one are
          marked not-ready instead of being auto-grouped. Off by default; projects infer a workspace from their repo owner.
        </p>
      </div>
      <button
        type="button"
        role="switch"
        aria-checked={on}
        disabled={set.isPending}
        onClick={() => set.mutate(!on)}
        className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors disabled:opacity-50 ${on ? 'bg-primary' : 'bg-muted-foreground/30'}`}
        title={on ? 'Required' : 'Optional'}
      >
        <span className={`inline-block h-4 w-4 rounded-full bg-white transition-transform ${on ? 'translate-x-4' : 'translate-x-0.5'}`} />
      </button>
    </div>
  )
}

function CreateWorkspaceModal({ onClose }: { onClose: () => void }) {
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [slugEdited, setSlugEdited] = useState(false)
  const [description, setDescription] = useState('')

  const create = useAction(
    (input: { name: string; slug: string; description?: string }) => client.workspaces.create(input),
    { invalidate: [orpc.workspaces.list.key()], onSuccess: onClose },
  )

  const slugify = (s: string) => s.toLowerCase().trim().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!name.trim() || !slug.trim()) return
    create.mutate({ name: name.trim(), slug: slug.trim(), description: description.trim() || undefined })
  }

  return (
    <Modal open onClose={onClose} title="Add workspace" subtitle="A new ownership partition for projects">
      <form onSubmit={handleSubmit}>
        <div className="px-5 py-4 space-y-4">
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-foreground">Name <span className="text-destructive">*</span></label>
            <input
              type="text" required autoFocus value={name}
              onChange={(e) => { setName(e.target.value); if (!slugEdited) setSlug(slugify(e.target.value)) }}
              placeholder="Payments"
              className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40"
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-foreground">Slug <span className="text-destructive">*</span></label>
            <input
              type="text" required value={slug}
              onChange={(e) => { setSlug(slugify(e.target.value)); setSlugEdited(true) }}
              placeholder="payments"
              className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm font-mono text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40"
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-foreground">Description</label>
            <input
              type="text" value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What lives in this workspace?"
              className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40"
            />
          </div>
        </div>
        <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-border">
          <button type="button" onClick={onClose} className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
            Cancel
          </button>
          <button type="submit" disabled={create.isPending || !name.trim() || !slug.trim()}
            className="rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
            style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}>
            Add workspace
          </button>
        </div>
      </form>
    </Modal>
  )
}
