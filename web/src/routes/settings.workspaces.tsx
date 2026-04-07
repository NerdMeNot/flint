import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { Boxes, FolderGit2, Calendar } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { formatTime } from '#/lib/format-time'

export const Route = createFileRoute('/settings/workspaces')({
  component: WorkspacesPage,
})

function WorkspacesPage() {
  const { data: wsData } = useSuspenseQuery(orpc.workspaces.list.queryOptions({ input: {} }))
  const workspaces = wsData.items

  return (
    <div className="space-y-5">
      <p className="text-muted-foreground text-sm">
        {workspaces.length} {workspaces.length === 1 ? 'workspace' : 'workspaces'} configured
      </p>

      {workspaces.length === 0 ? (
        <div className="island-shell p-12 flex flex-col items-center gap-3 text-muted-foreground">
          <Boxes size={32} strokeWidth={1.2} />
          <span className="text-sm">No workspaces configured yet.</span>
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {workspaces.map((ws, i) => (
            <div
              key={ws.id}
              className="feature-card rise-in p-5 space-y-3"
              style={{ animationDelay: `${i * 50 + 30}ms` }}
            >
              <div className="flex items-start justify-between">
                <div>
                  <h3 className="font-semibold text-sm text-foreground truncate">{ws.name}</h3>
                  <p className="text-xs text-muted-foreground font-mono truncate">{ws.slug}</p>
                </div>
                <span className="island-kicker !text-[0.55rem]">{ws.id}</span>
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
    </div>
  )
}
