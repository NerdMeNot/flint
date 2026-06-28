import { useState } from 'react'
import { useSuspenseQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { Modal } from '#/components/Modal'
import { FormSelect } from '#/components/FormSelect'

const inputClass =
  'w-full rounded-lg border border-border bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-ring/40'

// NewProjectModal registers a project via the API (the canonical create path).
// Repo + forge are required; workspace is required when org policy demands it —
// surfaced as a server 400 here.
export function NewProjectModal({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate()
  const { data: forges } = useSuspenseQuery(orpc.forgeConnections.list.queryOptions({ input: {} }))
  const { data: workspaces } = useSuspenseQuery(orpc.workspaces.list.queryOptions({ input: {} }))

  const forgeItems = forges.items
  const [repo, setRepo] = useState('')
  const [forgeRef, setForgeRef] = useState(forgeItems[0]?.displayName ?? '')
  const [displayName, setDisplayName] = useState('')
  const [workspace, setWorkspace] = useState('')

  const create = useAction(client.projects.create, {
    invalidate: [orpc.projects.list.key()],
    onSuccess: (res) => navigate({ to: '/ci/projects/$id', params: { id: res.id } }),
  })

  return (
    <Modal open onClose={onClose} title="New project" subtitle="Register a repository with a .flint/ pipeline">
      <div className="px-5 py-4 space-y-4">
        <div className="space-y-1.5">
          <label className="text-xs font-medium text-foreground">Repository</label>
          <input
            value={repo}
            onChange={(e) => setRepo(e.target.value)}
            placeholder="owner/repo"
            className={`${inputClass} font-mono`}
            autoFocus
          />
        </div>
        <div className="space-y-1.5">
          <label className="text-xs font-medium text-foreground">Forge</label>
          <FormSelect
            value={forgeRef}
            onChange={setForgeRef}
            placeholder="Select a forge connection"
            options={forgeItems.map((f) => ({ key: f.displayName, label: f.displayName }))}
          />
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
        <div className="space-y-1.5">
          <label className="text-xs font-medium text-foreground">Display name</label>
          <input value={displayName} onChange={(e) => setDisplayName(e.target.value)} placeholder="Optional" className={inputClass} />
        </div>
        {create.isError && (
          <p className="text-xs text-red-500">
            {String((create.error as Error)?.message || '').includes('409')
              ? 'A project for this repo already exists.'
              : String((create.error as Error)?.message || '').includes('workspace is required')
                ? 'A workspace is required by your org policy.'
                : 'Could not create the project.'}
          </p>
        )}
      </div>
      <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-border">
        <button type="button" onClick={onClose} className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
          Cancel
        </button>
        <button
          type="button"
          disabled={!repo.trim() || !forgeRef || create.isPending}
          onClick={() => create.mutate({
            repo: repo.trim(),
            forgeRef,
            workspace: workspace || undefined,
            displayName: displayName.trim() || undefined,
          })}
          className="rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
          style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
        >
          {create.isPending ? 'Creating…' : 'Create project'}
        </button>
      </div>
    </Modal>
  )
}
