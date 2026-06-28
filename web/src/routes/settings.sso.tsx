import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Plus, ShieldCheck, Settings2 } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { ConfirmButton } from '#/components/ConfirmButton'
import { SSOWizard } from '#/components/sso/SSOWizard'
import { GroupRoleMappings } from '#/components/sso/GroupRoleMappings'
import { ScimProvisioning } from '#/components/sso/ScimProvisioning'

export const Route = createFileRoute('/settings/sso')({
  component: SSOSettings,
})

function SSOSettings() {
  const [wizard, setWizard] = useState(false)
  const { data: providers } = useQuery(orpc.auth.providers.list.queryOptions({ input: {} }))

  const remove = useAction(
    (providerType: string) => client.auth.providers.delete({ providerType }),
    { invalidate: [orpc.auth.providers.list.key()] },
  )

  const configured = providers?.providers ?? []
  const liveFor = (type: string) =>
    type === 'oidc' ? providers?.oidcConfigured : providers?.samlConfigured

  return (
    <div className="max-w-2xl space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="display-title text-xl font-bold text-foreground">Single Sign-On</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Let your team sign in with your corporate identity provider via OIDC or SAML 2.0.
          </p>
        </div>
        {!wizard && (
          <button
            onClick={() => setWizard(true)}
            className="flex shrink-0 items-center gap-2 rounded-lg bg-primary text-primary-foreground font-medium text-sm px-3.5 py-2 hover:opacity-90 transition-opacity"
          >
            <Plus size={15} /> Connect a provider
          </button>
        )}
      </div>

      {wizard ? (
        <div className="island-shell p-5">
          <SSOWizard onComplete={() => setWizard(false)} onCancel={() => setWizard(false)} />
        </div>
      ) : configured.length === 0 ? (
        <div className="island-shell p-8 flex flex-col items-center text-center gap-3">
          <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted/40">
            <ShieldCheck size={22} className="text-muted-foreground" />
          </div>
          <div>
            <h2 className="text-base font-semibold text-foreground">No identity provider connected</h2>
            <p className="text-sm text-muted-foreground mt-1 max-w-sm">
              Connect Okta, Entra ID, Google, Auth0, OneLogin, Keycloak — or any generic OIDC / SAML
              provider — with a guided setup that validates before going live.
            </p>
          </div>
          <button
            onClick={() => setWizard(true)}
            className="mt-1 flex items-center gap-2 rounded-lg bg-primary text-primary-foreground font-medium text-sm px-4 py-2 hover:opacity-90 transition-opacity"
          >
            <Plus size={15} /> Connect a provider
          </button>
        </div>
      ) : (
        <div className="space-y-6">
          <div className="space-y-3">
          {configured.map((p) => (
            <div key={p.id} className="island-shell flex items-center justify-between gap-4 p-4 rise-in">
              <div className="flex items-center gap-3 min-w-0">
                <div className="flex h-9 w-9 items-center justify-center rounded-lg border border-border bg-muted/30">
                  <Settings2 size={16} className="text-foreground" />
                </div>
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-semibold text-foreground truncate">{p.displayName}</span>
                    <span className="rounded-md bg-muted px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground uppercase">
                      {p.providerType}
                    </span>
                  </div>
                  <div className="flex items-center gap-1.5 mt-0.5">
                    <span className={`h-1.5 w-1.5 rounded-full ${liveFor(p.providerType) ? 'bg-[var(--success)]' : 'bg-[var(--warning)]'}`} />
                    <span className="text-xs text-muted-foreground">
                      {liveFor(p.providerType) ? 'Active' : 'Saved — restart required'}
                    </span>
                  </div>
                </div>
              </div>
              <div className="flex items-center gap-2 shrink-0">
                <button
                  onClick={() => setWizard(true)}
                  className="rounded-md border border-border px-2.5 py-1 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors"
                >
                  Reconfigure
                </button>
                <ConfirmButton
                  label={remove.isPending ? 'Removing…' : 'Remove'}
                  onConfirm={() => remove.mutate(p.providerType)}
                  disabled={remove.isPending}
                />
              </div>
            </div>
          ))}
          </div>
          <GroupRoleMappings />
          <ScimProvisioning />
        </div>
      )}
    </div>
  )
}
