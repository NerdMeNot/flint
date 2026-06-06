import { createFileRoute } from '@tanstack/react-router'
import { useState, useEffect } from 'react'
import { Monitor, Smartphone, Globe, Trash2, Loader2, Shield } from 'lucide-react'

export const Route = createFileRoute('/settings/sessions')({
  component: SessionsPage,
})

interface Session {
  id: string
  ip_address: string | null
  user_agent: string | null
  created_at: string
  last_activity: string
  expires_at: string
}

function SessionsPage() {
  const [sessions, setSessions] = useState<Session[]>([])
  const [loading, setLoading] = useState(true)
  const [revoking, setRevoking] = useState<string | null>(null)

  const fetchSessions = async () => {
    try {
      const token = localStorage.getItem('flint_access_token')
      const res = await fetch('/auth/sessions', {
        headers: { 'Authorization': `Bearer ${token}` },
      })
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
    setRevoking(id)
    try {
      const token = localStorage.getItem('flint_access_token')
      await fetch(`/auth/sessions/${id}`, {
        method: 'DELETE',
        headers: { 'Authorization': `Bearer ${token}` },
      })
      setSessions((prev) => prev.filter((s) => s.id !== id))
    } catch {
      // ignore
    } finally {
      setRevoking(null)
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
    const now = new Date()
    const diff = now.getTime() - d.getTime()
    if (diff < 60000) return 'Just now'
    if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`
    if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`
    return d.toLocaleDateString()
  }

  return (
    <div className="max-w-2xl space-y-6">
      <div>
        <h1 className="display-title text-xl font-bold text-foreground">Active Sessions</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Manage your active sessions across devices. Revoking a session forces re-authentication.
        </p>
      </div>

      {loading ? (
        <div className="flex justify-center py-12">
          <Loader2 size={20} className="animate-spin text-muted-foreground" />
        </div>
      ) : sessions.length === 0 ? (
        <div className="island-shell p-8 text-center">
          <Shield size={24} className="mx-auto text-muted-foreground mb-2" />
          <p className="text-sm text-muted-foreground">No active sessions found</p>
        </div>
      ) : (
        <div className="space-y-2">
          {sessions.map((session, i) => {
            const device = parseDevice(session.user_agent)
            const DeviceIcon = device.icon
            const isCurrent = i === 0 // most recent = current (heuristic)

            return (
              <div key={session.id} className="island-shell p-4">
                <div className="flex items-start justify-between">
                  <div className="flex items-start gap-3">
                    <div className={`flex h-9 w-9 items-center justify-center rounded-lg shrink-0 ${
                      isCurrent ? 'bg-primary/10' : 'bg-muted/50'
                    }`}>
                      <DeviceIcon size={18} className={isCurrent ? 'text-primary' : 'text-muted-foreground'} />
                    </div>
                    <div>
                      <div className="flex items-center gap-2">
                        <span className="text-sm font-medium text-foreground">{device.label}</span>
                        {isCurrent && (
                          <span className="text-[10px] font-semibold uppercase tracking-wider text-primary px-1.5 py-0.5 rounded bg-primary/10">
                            Current
                          </span>
                        )}
                      </div>
                      <div className="flex items-center gap-2 text-xs text-muted-foreground mt-0.5">
                        {session.ip_address && <span>{session.ip_address}</span>}
                        <span className="opacity-40">·</span>
                        <span>Active {formatTime(session.last_activity)}</span>
                      </div>
                      {session.user_agent && (
                        <p className="text-xs text-muted-foreground/60 mt-1 truncate max-w-[300px]">
                          {session.user_agent}
                        </p>
                      )}
                    </div>
                  </div>
                  {!isCurrent && (
                    <button
                      onClick={() => revokeSession(session.id)}
                      disabled={revoking === session.id}
                      className="flex items-center gap-1.5 rounded-lg border border-destructive/20 text-destructive text-xs font-medium px-3 py-1.5 hover:bg-destructive/5 disabled:opacity-50 transition-colors shrink-0"
                    >
                      {revoking === session.id
                        ? <Loader2 size={12} className="animate-spin" />
                        : <Trash2 size={12} />}
                      Revoke
                    </button>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
