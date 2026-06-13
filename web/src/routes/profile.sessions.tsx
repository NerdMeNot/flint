import { createFileRoute } from '@tanstack/react-router'
import { useState, useEffect } from 'react'
import { Monitor, Smartphone, Globe, Loader2 } from 'lucide-react'
import { PageHeader } from '#/components/PageHeader'
import { EmptyState } from '#/components/EmptyState'
import { Badge } from '#/components/Badge'
import { ConfirmButton } from '#/components/ConfirmButton'

export const Route = createFileRoute('/profile/sessions')({
  component: SessionsTab,
})

interface Session {
  id: string
  ip_address: string | null
  user_agent: string | null
  created_at: string
  last_activity: string
  expires_at: string
}

function SessionsTab() {
  const [sessions, setSessions] = useState<Session[]>([])
  const [loading, setLoading] = useState(true)

  const fetchSessions = async () => {
    try {
      const token = localStorage.getItem('flint_access_token')
      const res = await fetch('/auth/sessions', { headers: { 'Authorization': `Bearer ${token}` } })
      const data = await res.json()
      setSessions(data.items || [])
    } catch {
      // ignore
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchSessions() }, [])

  const revokeSession = async (id: string) => {
    try {
      const token = localStorage.getItem('flint_access_token')
      await fetch(`/auth/sessions/${id}`, { method: 'DELETE', headers: { 'Authorization': `Bearer ${token}` } })
      setSessions((prev) => prev.filter((s) => s.id !== id))
    } catch {
      // ignore
    }
  }

  const parseDevice = (ua: string | null) => {
    if (!ua) return { icon: Globe, label: 'Unknown device' }
    const lower = ua.toLowerCase()
    if (lower.includes('mobile') || lower.includes('android') || lower.includes('iphone')) {
      return { icon: Smartphone, label: 'Mobile' }
    }
    return { icon: Monitor, label: 'Desktop' }
  }

  const formatTime = (iso: string) => {
    const d = new Date(iso)
    const diff = Date.now() - d.getTime()
    if (diff < 60000) return 'Just now'
    if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`
    if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`
    return d.toLocaleDateString()
  }

  return (
    <div className="max-w-2xl space-y-6">
      <PageHeader title="Sessions" subtitle="Your active sign-ins. Revoking a session forces re-authentication." />

      {loading ? (
        <div className="flex justify-center py-12">
          <Loader2 size={20} className="animate-spin text-muted-foreground" />
        </div>
      ) : sessions.length === 0 ? (
        <EmptyState icon={Monitor} message="No active sessions found." />
      ) : (
        <div className="island-shell !p-0 overflow-hidden divide-y divide-border">
          {sessions.map((session, i) => {
            const device = parseDevice(session.user_agent)
            const DeviceIcon = device.icon
            const isCurrent = i === 0 // most recent = current (heuristic; real flag in Phase 2)

            return (
              <div key={session.id} className="flex items-start justify-between gap-3 px-4 py-3">
                <div className="flex items-start gap-3 min-w-0">
                  <div className={`flex h-9 w-9 items-center justify-center rounded-lg shrink-0 ${isCurrent ? 'bg-primary/10' : 'bg-muted/50'}`}>
                    <DeviceIcon size={17} className={isCurrent ? 'text-primary' : 'text-muted-foreground'} />
                  </div>
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium text-foreground">{device.label}</span>
                      {isCurrent && <Badge variant="primary">Current</Badge>}
                    </div>
                    <div className="flex items-center gap-2 text-xs text-muted-foreground mt-0.5">
                      {session.ip_address && <span>{session.ip_address}</span>}
                      <span className="opacity-40">·</span>
                      <span>Active {formatTime(session.last_activity)}</span>
                    </div>
                    {session.user_agent && (
                      <p className="text-xs text-muted-foreground/60 mt-1 truncate max-w-[320px]">{session.user_agent}</p>
                    )}
                  </div>
                </div>
                {!isCurrent && <ConfirmButton label="Revoke" onConfirm={() => revokeSession(session.id)} />}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
