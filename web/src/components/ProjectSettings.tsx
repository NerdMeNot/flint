import { useState } from 'react'
import { useSuspenseQuery } from '@tanstack/react-query'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { Modal } from '#/components/Modal'
import { FormSelect } from '#/components/FormSelect'

const inputClass =
  'w-full rounded-lg border border-border bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-ring/40'

// EditProjectModal PATCHes a project's metadata (display name / colour / workspace).
// Used by the admin Projects page (settings/projects).
export function EditProjectModal({
  projectId,
  initial,
  onClose,
}: {
  projectId: string
  initial: { name: string; colour: string; workspace: string }
  onClose: () => void
}) {
  const { data: workspaces } = useSuspenseQuery(orpc.workspaces.list.queryOptions({ input: {} }))
  const [displayName, setDisplayName] = useState(initial.name)
  const [colour, setColour] = useState(initial.colour || '#6366f1')
  const [workspace, setWorkspace] = useState(initial.workspace)

  const update = useAction(client.projects.update, {
    invalidate: [orpc.projects.list.key(), orpc.projects.get.key()],
    onSuccess: onClose,
  })

  return (
    <Modal open onClose={onClose} title="Edit project" subtitle="Update project metadata">
      <div className="px-5 py-4 space-y-4">
        <div className="space-y-1.5">
          <label className="text-xs font-medium text-foreground">Display name</label>
          <input value={displayName} onChange={(e) => setDisplayName(e.target.value)} className={inputClass} autoFocus />
        </div>
        <div className="space-y-1.5">
          <label className="text-xs font-medium text-foreground">Accent colour</label>
          <div className="flex items-center gap-2">
            <input type="color" value={colour} onChange={(e) => setColour(e.target.value)} className="h-9 w-12 rounded-md border border-border bg-transparent" />
            <input value={colour} onChange={(e) => setColour(e.target.value)} className={`${inputClass} font-mono`} />
          </div>
        </div>
        <div className="space-y-1.5">
          <label className="text-xs font-medium text-foreground">Workspace</label>
          <FormSelect
            value={workspace}
            onChange={setWorkspace}
            placeholder="Default (Unsorted)"
            options={workspaces.items.map((w) => ({ key: w.slug, label: w.name }))}
          />
        </div>
        {update.isError && <p className="text-xs text-red-500">Could not update the project.</p>}
      </div>
      <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-border">
        <button type="button" onClick={onClose} className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
          Cancel
        </button>
        <button
          type="button"
          disabled={update.isPending}
          onClick={() => update.mutate({
            id: projectId,
            displayName: displayName.trim() || undefined,
            colour,
            workspace: workspace || undefined,
          })}
          className="rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
          style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
        >
          {update.isPending ? 'Saving…' : 'Save changes'}
        </button>
      </div>
    </Modal>
  )
}
