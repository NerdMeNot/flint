import { useQuery } from '@tanstack/react-query'
import { ShieldCheck, KeyRound } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { Switch } from '#/components/ui/switch'

/**
 * Require-SSO enforcement toggle. When on, IdP-provisioned users must sign in
 * via SSO — password login is rejected for them. Local/manual accounts (created
 * by an admin) remain a break-glass path and are never locked out. The toggle is
 * disabled until a provider is configured, so an admin can't lock the org out.
 */
export function RequireSso() {
  const { data } = useQuery(orpc.auth.providers.list.queryOptions({ input: {} }))
  const configured = (data?.oidcConfigured || data?.samlConfigured) ?? false
  const enabled = data?.requireSso ?? false

  const toggle = useAction(
    (next: boolean) => client.auth.providers.setRequireSso({ enabled: next }),
    { invalidate: [orpc.auth.providers.list.key()] },
  )

  return (
    <div className="island-shell p-5 space-y-4">
      <div className="flex items-center gap-2">
        <ShieldCheck size={16} className="text-muted-foreground" />
        <h2 className="text-base font-semibold text-foreground">Require single sign-on</h2>
        {enabled && (
          <span className="ml-1 inline-flex items-center gap-1.5 rounded-full bg-[var(--success)]/10 px-2 py-0.5 text-[11px] font-medium text-[var(--success)]">
            <span className="h-1.5 w-1.5 rounded-full bg-[var(--success)]" /> Enforced
          </span>
        )}
      </div>

      <div className="flex items-start gap-3">
        <Switch
          checked={enabled}
          onCheckedChange={(next) => toggle.mutate(next)}
          disabled={!configured || toggle.isPending}
          className="mt-0.5"
        />
        <div className="text-sm text-muted-foreground">
          <p>
            Users provisioned by your identity provider must sign in through it; password sign-in is
            rejected for them.
          </p>
          {!configured && (
            <p className="mt-1 text-xs text-[var(--warning)]">
              Configure and test an SSO provider before you can require it.
            </p>
          )}
        </div>
      </div>

      <div className="flex items-start gap-2 rounded-lg border border-border bg-muted px-3 py-2.5">
        <KeyRound size={14} className="mt-0.5 shrink-0 text-muted-foreground" />
        <p className="text-xs text-muted-foreground">
          <span className="font-medium text-foreground">Break-glass preserved.</span> Local accounts
          created here by an admin can always sign in with a password, even while SSO is required — so
          you can never be locked out.
        </p>
      </div>
    </div>
  )
}
