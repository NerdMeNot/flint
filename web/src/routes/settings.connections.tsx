import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { GitFork, Clock } from 'lucide-react'
import { orpc } from '#/lib/orpc'
import { formatTime } from '#/lib/format-time'
import { PageHeader } from '#/components/PageHeader'

export const Route = createFileRoute('/settings/connections')({
  component: ConnectionsPage,
})

function forgeLabel(forgeType: string) {
  switch (forgeType.toLowerCase()) {
    case 'github':
      return 'GitHub'
    case 'gitlab':
      return 'GitLab'
    default:
      return forgeType
  }
}

function ForgeIcon({ className }: { forgeType: string; className?: string }) {
  return <GitFork size={14} className={className} />
}

function ConnectionsPage() {
  const { data: connData } = useSuspenseQuery(
    orpc.forgeConnections.list.queryOptions({ input: {} }),
  )
  const connections = connData.items

  return (
    <div className="space-y-5">
      <PageHeader
        title="Connections"
        subtitle={`${connections.length} forge ${connections.length === 1 ? 'connection' : 'connections'}`}
      />

      {connections.length === 0 ? (
        <div className="island-shell p-12 flex flex-col items-center gap-3 text-muted-foreground">
          <GitFork size={32} strokeWidth={1.2} />
          <span className="text-sm">No forge connections configured yet.</span>
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {connections.map((conn, i) => (
            <div
              key={conn.id}
              className="feature-card rise-in p-5 space-y-3"
              style={{ animationDelay: `${i * 50 + 30}ms` }}
            >
              <div className="flex items-start justify-between gap-3">
                <div className="flex items-center gap-2.5 min-w-0">
                  <ForgeIcon
                    forgeType={conn.forgeType}
                    className="text-muted-foreground shrink-0"
                  />
                  <div className="min-w-0">
                    <h3 className="font-semibold text-sm text-foreground truncate">
                      {conn.displayName}
                    </h3>
                  </div>
                </div>
                <span className="island-kicker !text-[11px] shrink-0">
                  {forgeLabel(conn.forgeType)}
                </span>
              </div>

              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <Clock size={12} />
                <span>Connected {formatTime(conn.createdAt)}</span>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
