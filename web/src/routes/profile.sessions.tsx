import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { Monitor, Smartphone, Globe } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { PageHeader } from '#/components/PageHeader'
import { EmptyState } from '#/components/EmptyState'
import { Badge } from '#/components/Badge'
import { ConfirmButton } from '#/components/ConfirmButton'
import type { Session } from '#/lib/api/types'

export const Route = createFileRoute('/profile/sessions')({
  component: SessionsTab,
})

function deviceOf(ua?: string) {
  if (!ua) return { icon: Globe, label: 'Unknown device' }
  const lower = ua.toLowerCase()
  if (lower.includes('mobile') || lower.includes('android') || lower.includes('iphone')) {
    return { icon: Smartphone, label: 'Mobile' }
  }
  if (lower.includes('cli')) return { icon: Globe, label: 'CLI' }
  return { icon: Monitor, label: 'Desktop' }
}

function relativeTime(iso: string) {
  const diff = Date.now() - new Date(iso).getTime()
  if (diff < 60_000) return 'just now'
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`
  return new Date(iso).toLocaleDateString()
}

function SessionsTab() {
  const { data: sessions } = useQuery(orpc.auth.sessions.list.queryOptions({ input: {} }))

  const revoke = useAction((id: string) => client.auth.sessions.revoke({ id }), {
    invalidate: [orpc.auth.sessions.list.key()],
  })

  return (
    <div className="max-w-2xl space-y-6">
      <PageHeader title="Sessions" subtitle="Your active sign-ins. Revoking a session forces re-authentication." />

      {!sessions ? (
        <div className="island-shell h-28 animate-pulse" />
      ) : sessions.length === 0 ? (
        <EmptyState icon={Monitor} message="No active sessions found." />
      ) : (
        <div className="island-shell !p-0 overflow-hidden divide-y divide-border">
          {sessions.map((session: Session) => {
            const device = deviceOf(session.userAgent)
            const DeviceIcon = device.icon
            return (
              <div key={session.id} className="flex items-start justify-between gap-3 px-4 py-3">
                <div className="flex items-start gap-3 min-w-0">
                  <div className={`flex h-9 w-9 items-center justify-center rounded-lg shrink-0 ${session.current ? 'bg-primary/10' : 'bg-muted/50'}`}>
                    <DeviceIcon size={17} className={session.current ? 'text-primary' : 'text-muted-foreground'} />
                  </div>
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium text-foreground">{device.label}</span>
                      {session.current && <Badge variant="primary">Current</Badge>}
                    </div>
                    <div className="flex items-center gap-2 text-xs text-muted-foreground mt-0.5">
                      {session.ipAddress && <span>{session.ipAddress}</span>}
                      <span className="opacity-40">·</span>
                      <span>Active {relativeTime(session.lastActivity)}</span>
                    </div>
                    {session.userAgent && (
                      <p className="text-xs text-muted-foreground/60 mt-1 truncate max-w-[320px]">{session.userAgent}</p>
                    )}
                  </div>
                </div>
                {!session.current && <ConfirmButton label="Revoke" onConfirm={() => revoke.mutate(session.id)} />}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
