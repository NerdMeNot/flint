import { useQuery } from '@tanstack/react-query'
import { History, CheckCircle2, XCircle } from 'lucide-react'
import { orpc } from '#/lib/orpc'

/**
 * Recent SSO sign-in attempts with field-level failure diagnostics — the
 * per-connection sign-in history (WorkOS/Scalekit diagnostics model). Each
 * failure surfaces a short human summary plus the raw provider detail so an
 * admin can tell an audience mismatch from a clock-skew from a missing email.
 */
export function SignInLog() {
  const { data } = useQuery(orpc.auth.providers.signInLog.queryOptions({ input: {} }))
  const events = data?.events ?? []

  return (
    <div className="island-shell p-5 space-y-4">
      <div className="flex items-center gap-2">
        <History size={16} className="text-muted-foreground" />
        <h2 className="text-base font-semibold text-foreground">Recent sign-ins</h2>
      </div>
      <p className="text-sm text-muted-foreground -mt-1">
        The last sign-in attempts through your identity provider. Failures include the field-level
        reason so you can diagnose a misconfiguration without reading server logs.
      </p>

      {events.length === 0 ? (
        <p className="text-sm text-muted-foreground/80 rounded-lg border border-dashed border-border px-4 py-6 text-center">
          No sign-in attempts recorded yet.
        </p>
      ) : (
        <ul className="divide-y divide-border rounded-lg border border-border overflow-hidden">
          {events.map((e, i) => (
            <li key={i} className="flex items-start gap-3 px-3.5 py-2.5">
              {e.result === 'success' ? (
                <CheckCircle2 size={16} className="mt-0.5 shrink-0 text-[var(--success)]" />
              ) : (
                <XCircle size={16} className="mt-0.5 shrink-0 text-[var(--destructive)]" />
              )}
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="text-sm font-medium text-foreground truncate">
                    {e.email || (e.result === 'failure' ? 'Unknown user' : '—')}
                  </span>
                  {e.result === 'failure' && e.summary && (
                    <span className="rounded-md bg-[var(--destructive)]/10 px-1.5 py-0.5 text-[11px] font-medium text-[var(--destructive)]">
                      {e.summary}
                    </span>
                  )}
                </div>
                {e.result === 'failure' && e.detail && (
                  <p className="mt-0.5 text-xs text-muted-foreground font-mono break-words">{e.detail}</p>
                )}
              </div>
              <div className="shrink-0 text-right">
                <div className="text-xs text-muted-foreground tabular-nums">{formatTime(e.time)}</div>
                {e.ip && <div className="text-[11px] text-muted-foreground/70 tabular-nums">{e.ip}</div>}
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function formatTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}
