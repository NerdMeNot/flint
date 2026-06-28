import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { RefreshCw, Loader2, KeyRound, AlertTriangle } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { CopyField } from './CopyField'
import { ConfirmButton } from '#/components/ConfirmButton'

/**
 * SCIM 2.0 directory-sync setup. Generates the bearer token an IdP uses to
 * provision/deprovision users and groups, and surfaces the SCIM base URL.
 */
export function ScimProvisioning() {
  const { data } = useQuery(orpc.auth.providers.scim.get.queryOptions({ input: {} }))
  const [freshToken, setFreshToken] = useState<string | null>(null)

  const generate = useAction(() => client.auth.providers.scim.generate(), {
    invalidate: [orpc.auth.providers.scim.get.key()],
    onSuccess: (res: { token: string }) => setFreshToken(res.token),
  })
  const revoke = useAction(() => client.auth.providers.scim.revoke(), {
    invalidate: [orpc.auth.providers.scim.get.key()],
    onSuccess: () => setFreshToken(null),
  })

  const configured = data?.configured ?? false
  const baseUrl = data?.baseUrl ?? ''

  return (
    <div className="island-shell p-5 space-y-4">
      <div className="flex items-center gap-2">
        <RefreshCw size={16} className="text-muted-foreground" />
        <h2 className="text-base font-semibold text-foreground">Directory sync (SCIM 2.0)</h2>
        {configured && (
          <span className="ml-1 inline-flex items-center gap-1.5 rounded-full bg-[var(--success)]/10 px-2 py-0.5 text-[11px] font-medium text-[var(--success)]">
            <span className="h-1.5 w-1.5 rounded-full bg-[var(--success)]" /> Active
          </span>
        )}
      </div>
      <p className="text-sm text-muted-foreground -mt-1">
        Let your IdP push real-time user & group provisioning and deprovisioning (Okta, Entra,
        OneLogin, JumpCloud). Deactivated users have their sessions revoked immediately.
      </p>

      {baseUrl && <CopyField label="SCIM Base URL" value={baseUrl} />}

      {freshToken ? (
        <div className="space-y-2">
          <div className="flex items-start gap-2 rounded-lg border border-[var(--warning)]/30 bg-[var(--warning)]/10 p-3 text-sm">
            <AlertTriangle size={15} className="text-[var(--warning)] shrink-0 mt-0.5" />
            <span className="text-foreground">
              Copy this token now — it's shown only once. Paste it as the Bearer token in your IdP's
              SCIM configuration.
            </span>
          </div>
          <CopyField label="SCIM Bearer Token" value={freshToken} />
        </div>
      ) : (
        <p className="text-sm text-muted-foreground">
          {configured
            ? 'A SCIM token is active. Regenerate to roll it (invalidates the old one), or revoke to disable provisioning.'
            : 'No SCIM token yet. Generate one to enable provisioning from your IdP.'}
        </p>
      )}

      <div className="flex items-center gap-3 pt-1">
        <button
          onClick={() => generate.mutate(undefined)}
          disabled={generate.isPending}
          className="flex items-center gap-2 rounded-lg bg-primary text-primary-foreground font-medium text-sm px-4 py-2 hover:opacity-90 disabled:opacity-50 transition-opacity"
        >
          {generate.isPending ? <Loader2 size={14} className="animate-spin" /> : <KeyRound size={14} />}
          {configured ? 'Regenerate token' : 'Generate token'}
        </button>
        {configured && (
          <ConfirmButton
            label={revoke.isPending ? 'Revoking…' : 'Revoke'}
            onConfirm={() => revoke.mutate(undefined)}
            disabled={revoke.isPending}
          />
        )}
        {generate.isError && <span className="text-sm text-destructive">{generate.error.message || 'Failed'}</span>}
      </div>
    </div>
  )
}
