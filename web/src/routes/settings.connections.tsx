import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { GitFork, Clock, Plus, ExternalLink } from 'lucide-react'
import { useState } from 'react'
import { orpc } from '#/lib/orpc'
import { formatTime } from '#/lib/format-time'
import { PageHeader } from '#/components/PageHeader'
import { Badge } from '#/components/Badge'
import { EmptyState } from '#/components/EmptyState'
import { Modal } from '#/components/Modal'

export const Route = createFileRoute('/settings/connections')({
  component: ConnectionsPage,
})

function forgeLabel(forgeType: string) {
  switch (forgeType.toLowerCase()) {
    case 'github': return 'GitHub'
    case 'gitlab': return 'GitLab'
    default: return forgeType
  }
}

function ConnectionsPage() {
  const { data: connData } = useSuspenseQuery(
    orpc.forgeConnections.list.queryOptions({ input: {} }),
  )
  const connections = connData.items
  const [showConnect, setShowConnect] = useState(false)

  const connectButton = (
    <button
      type="button"
      onClick={() => setShowConnect(true)}
      className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-colors"
      style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
    >
      <Plus size={12} /> Connect forge
    </button>
  )

  return (
    <div className="space-y-5">
      <PageHeader
        title="Connections"
        subtitle={`${connections.length} forge ${connections.length === 1 ? 'connection' : 'connections'}`}
        action={connectButton}
      />

      {connections.length === 0 ? (
        <EmptyState icon={GitFork} message="No forge connections yet." action={connectButton} />
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
                  <GitFork size={14} className="text-muted-foreground shrink-0" />
                  <div className="min-w-0">
                    <h3 className="font-semibold text-sm text-foreground truncate">{conn.displayName}</h3>
                  </div>
                </div>
                <Badge>{forgeLabel(conn.forgeType)}</Badge>
              </div>

              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <Clock size={12} />
                <span>Connected {formatTime(conn.createdAt)}</span>
              </div>
            </div>
          ))}
        </div>
      )}

      {showConnect && <ConnectModal onClose={() => setShowConnect(false)} />}
    </div>
  )
}

// Forge connections are established by installing the Flint app on the forge
// (a server-side OAuth/app-install flow), not created from a form here — so
// this explains the steps rather than faking a connection.
function ConnectModal({ onClose }: { onClose: () => void }) {
  return (
    <Modal open onClose={onClose} title="Connect a forge" subtitle="Link GitHub or GitLab to Flint">
      <div className="px-5 py-4 space-y-4 text-sm text-muted-foreground">
        <p>Forge connections are established by installing the Flint app on your forge organization — this grants Flint scoped access to the repositories you choose.</p>
        <ol className="list-decimal pl-5 space-y-1.5">
          <li>Install the <span className="text-foreground font-medium">Flint app</span> on your GitHub or GitLab organization.</li>
          <li>Authorize the repositories Flint should see.</li>
          <li>The connection appears here once the install callback completes.</li>
        </ol>
        <a
          href="https://docs.flint.example/forge-connections"
          target="_blank"
          rel="noopener"
          className="inline-flex items-center gap-1.5 text-xs font-medium text-primary hover:underline"
        >
          Forge connection guide <ExternalLink size={11} />
        </a>
      </div>
      <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-border">
        <button type="button" onClick={onClose} className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
          Close
        </button>
      </div>
    </Modal>
  )
}
