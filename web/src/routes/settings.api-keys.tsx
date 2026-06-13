import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Key, Calendar, Clock, AlertTriangle, Plus, Copy, Check } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { formatTime } from '#/lib/format-time'
import { ScopeBadges } from '#/components/ScopeBadges'
import { FormSelect } from '#/components/FormSelect'
import { Modal } from '#/components/Modal'
import { Badge } from '#/components/Badge'
import { ConfirmButton } from '#/components/ConfirmButton'
import { useCopyToClipboard } from '#/hooks/use-copy-to-clipboard'

export const Route = createFileRoute('/settings/api-keys')({
  component: ApiKeysPage,
})

function ApiKeysPage() {
  const { data: apiKeysData } = useSuspenseQuery(orpc.apiKeys.list.queryOptions({ input: {} }))
  const apiKeys = apiKeysData.items
  const { data: rolesData } = useSuspenseQuery(orpc.roles.list.queryOptions({ input: {} }))
  const roles = rolesData.items
  const [showCreate, setShowCreate] = useState(false)
  const del = useAction((id: string) => client.apiKeys.delete({ id }), {
    invalidate: [orpc.apiKeys.list.key()],
  })

  const roleMap = new Map(roles.map((r) => [r.slug, r]))

  return (
    <div className="space-y-5">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="display-title text-lg text-foreground">API Keys</h2>
          <p className="text-muted-foreground text-xs mt-0.5">
            {apiKeys.length} {apiKeys.length === 1 ? 'key' : 'keys'} issued
          </p>
        </div>
        <button
          type="button"
          onClick={() => setShowCreate(true)}
          className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-colors"
          style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
        >
          <Plus size={12} />
          Create key
        </button>
      </div>

      {apiKeys.length === 0 ? (
        <div className="island-shell p-12 flex flex-col items-center gap-3 text-muted-foreground">
          <Key size={32} strokeWidth={1.2} />
          <span className="text-sm">No API keys created yet.</span>
        </div>
      ) : (
        <div className="space-y-2">
          {apiKeys.map((apiKey, i) => {
            const isExpired = apiKey.expiresAt && new Date(apiKey.expiresAt) < new Date()
            const role = roleMap.get(apiKey.role)

            return (
              <div
                key={apiKey.id}
                className={`island-shell !p-0 overflow-hidden rise-in ${isExpired ? 'border-warning/40' : ''}`}
                style={{ animationDelay: `${i * 50 + 30}ms` }}
              >
                <div className="p-4 space-y-3">
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex items-start gap-2.5 min-w-0">
                      <Key size={15} className={`mt-0.5 shrink-0 ${isExpired ? 'text-warning' : 'text-muted-foreground'}`} />
                      <div className="min-w-0">
                        <h3 className="font-semibold text-sm text-foreground truncate">{apiKey.name}</h3>
                        <p className="text-xs text-muted-foreground font-mono">{apiKey.id}</p>
                      </div>
                    </div>

                    <div className="flex items-center gap-2 shrink-0">
                      {isExpired && (
                        <Badge variant="danger">
                          <AlertTriangle size={10} />
                          Expired
                        </Badge>
                      )}
                      <ConfirmButton onConfirm={() => del.mutate(apiKey.id)} title="Delete API key" />
                    </div>
                  </div>

                  {/* Role + scope */}
                  <div className="flex flex-wrap items-center gap-2">
                    <span className={`rounded-md px-1.5 py-0.5 text-[11px] font-medium ${
                      role?.isSystem
                        ? 'bg-primary/10 text-primary border border-primary/20'
                        : 'bg-secondary text-foreground border border-border'
                    }`}>
                      {role?.name ?? apiKey.role}
                    </span>

                    {(apiKey.workspaces.length > 0 || apiKey.environments.length > 0) ? (
                      <>
                        <span className="text-[11px] text-muted-foreground opacity-40">restricted to</span>
                        <ScopeBadges workspaces={apiKey.workspaces} environments={apiKey.environments} />
                      </>
                    ) : (
                      <span className="text-[11px] text-muted-foreground opacity-40">inherits role scope</span>
                    )}
                  </div>

                  {/* Metadata */}
                  <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground pt-2 border-t border-border/50">
                    <span className="flex items-center gap-1.5">
                      <Calendar size={11} />
                      Created {formatTime(apiKey.createdAt)} by <span className="font-mono">{apiKey.createdBy}</span>
                    </span>
                    {apiKey.lastUsedAt && (
                      <span className="flex items-center gap-1.5">
                        <Clock size={11} />
                        Last used {formatTime(apiKey.lastUsedAt)}
                      </span>
                    )}
                    {apiKey.expiresAt && (
                      <span className={`flex items-center gap-1.5 ${isExpired ? 'text-warning' : ''}`}>
                        <AlertTriangle size={11} />
                        {isExpired ? 'Expired' : 'Expires'} {formatTime(apiKey.expiresAt)}
                      </span>
                    )}
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      )}

      {showCreate && <CreateKeyModal onClose={() => setShowCreate(false)} />}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Create API Key Modal
// ---------------------------------------------------------------------------

const EXPIRY_OPTIONS = [
  { key: '30d', label: '30 days' },
  { key: '90d', label: '90 days' },
  { key: '1y', label: '1 year' },
  { key: 'none', label: 'No expiry' },
]

function computeExpiry(preset: string): string | undefined {
  const now = new Date()
  switch (preset) {
    case '30d': return new Date(now.getTime() + 30 * 86400000).toISOString()
    case '90d': return new Date(now.getTime() + 90 * 86400000).toISOString()
    case '1y': return new Date(now.getTime() + 365 * 86400000).toISOString()
    default: return undefined
  }
}

function CreateKeyModal({ onClose }: { onClose: () => void }) {
  const { data: rolesData } = useSuspenseQuery(orpc.roles.list.queryOptions({ input: {} }))
  const roles = rolesData.items
  const { data: wsData } = useSuspenseQuery(orpc.workspaces.list.queryOptions({ input: {} }))
  const workspaces = wsData.items
  const { data: envData } = useSuspenseQuery(orpc.environments.list.queryOptions({ input: {} }))
  const environments = envData.items

  const [name, setName] = useState('')
  const [selectedRole, setSelectedRole] = useState('')
  const [scopeMode, setScopeMode] = useState<'inherit' | 'restrict'>('inherit')
  const [selectedWs, setSelectedWs] = useState<Set<string>>(new Set())
  const [selectedEnv, setSelectedEnv] = useState<Set<string>>(new Set())
  const [expiry, setExpiry] = useState('90d')
  const [generatedToken, setGeneratedToken] = useState<string | null>(null)
  const { copied, copy } = useCopyToClipboard()

  const create = useAction(
    (input: {
      name: string
      role: string
      workspaces: string[]
      environments: string[]
      expiresAt?: string
    }) => client.apiKeys.create(input),
    {
      invalidate: [orpc.apiKeys.list.key()],
      onSuccess: (result) => setGeneratedToken((result as { token: string }).token),
    },
  )

  const role = roles.find((r) => r.slug === selectedRole)

  // Determine which workspaces/envs are available for restriction
  // Can only narrow within the role's scope
  const availableWs = role && role.workspaces.length > 0
    ? workspaces.filter((ws) => role.workspaces.includes(ws.slug))
    : workspaces
  const availableEnv = role && role.environments.length > 0
    ? environments.filter((e) => role.environments.includes(e.name))
    : environments

  function toggleSet(set: Set<string>, setFn: (s: Set<string>) => void, key: string) {
    setFn(new Set(set.has(key) ? [...set].filter((k) => k !== key) : [...set, key]))
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!name.trim() || !selectedRole) return

    create.mutate({
      name,
      role: selectedRole,
      workspaces: scopeMode === 'restrict' ? [...selectedWs] : [],
      environments: scopeMode === 'restrict' ? [...selectedEnv] : [],
      expiresAt: computeExpiry(expiry),
    })
  }

  return (
    <Modal open onClose={onClose} title="Create API Key" subtitle="Generate a new key for programmatic access" wide>
      {generatedToken ? (
        <div className="px-5 py-5 space-y-4">
          <div className="rounded-lg border border-success/30 bg-success/5 p-4 space-y-2">
            <p className="text-xs font-semibold text-success">Key created successfully</p>
            <p className="text-[12px] text-muted-foreground">
              Copy this token now. It will not be shown again.
            </p>
            <div className="flex items-center gap-2 mt-2">
              <code className="flex-1 rounded-md bg-card border border-border px-3 py-2 text-xs font-mono text-foreground break-all">
                {generatedToken}
              </code>
              <button
                type="button"
                onClick={() => generatedToken && copy(generatedToken)}
                className="shrink-0 flex items-center gap-1.5 rounded-lg border border-border px-3 py-2 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors"
              >
                {copied ? <Check size={12} className="text-success" /> : <Copy size={12} />}
                {copied ? 'Copied' : 'Copy'}
              </button>
            </div>
          </div>
          <div className="flex justify-end">
            <button
              type="button"
              onClick={onClose}
              className="rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors"
              style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
            >
              Done
            </button>
          </div>
        </div>
      ) : (
        <form onSubmit={handleSubmit} className="flex flex-col overflow-hidden">
          <div className="flex-1 overflow-y-auto px-5 py-4 space-y-4">
            {/* Name */}
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Name <span className="text-destructive">*</span></label>
              <input
                type="text" required value={name} onChange={(e) => setName(e.target.value)}
                placeholder="CI Bot"
                className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40"
              />
            </div>

            {/* Role */}
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Role <span className="text-destructive">*</span></label>
              <FormSelect
                value={selectedRole}
                onChange={(v) => { setSelectedRole(v); setScopeMode('inherit'); setSelectedWs(new Set()); setSelectedEnv(new Set()) }}
                placeholder="Select a role..."
                options={roles.map((r) => ({ key: r.slug, label: r.name }))}
              />
              {role && (
                <div className="rounded-lg border border-border p-3 space-y-1.5 mt-2">
                  <p className="text-xs text-muted-foreground">{role.description}</p>
                  <ScopeBadges workspaces={role.workspaces} environments={role.environments} />
                </div>
              )}
            </div>

            {/* Scope restriction */}
            {role && (
              <div className="space-y-2">
                <label className="text-xs font-medium text-foreground">Scope</label>
                <div className="flex items-center gap-4">
                  <label className="flex items-center gap-2 text-xs cursor-pointer">
                    <input type="radio" name="scope" checked={scopeMode === 'inherit'} onChange={() => setScopeMode('inherit')} className="accent-primary" />
                    <span className="text-muted-foreground">Same as role</span>
                  </label>
                  <label className="flex items-center gap-2 text-xs cursor-pointer">
                    <input type="radio" name="scope" checked={scopeMode === 'restrict'} onChange={() => setScopeMode('restrict')} className="accent-primary" />
                    <span className="text-muted-foreground">Restrict further</span>
                  </label>
                </div>

                {scopeMode === 'restrict' && (
                  <div className="space-y-3 rounded-lg border border-border p-3">
                    <div className="space-y-1.5">
                      <label className="text-[12px] font-medium text-muted-foreground">Workspaces</label>
                      <div className="flex flex-wrap gap-1.5">
                        {availableWs.map((ws) => (
                          <button key={ws.slug} type="button" onClick={() => toggleSet(selectedWs, setSelectedWs, ws.slug)}
                            className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                              selectedWs.has(ws.slug)
                                ? 'bg-primary/15 text-primary ring-1 ring-primary/20'
                                : 'text-muted-foreground border border-border hover:text-foreground hover:bg-accent'
                            }`}>
                            {ws.name}
                          </button>
                        ))}
                      </div>
                    </div>
                    <div className="space-y-1.5">
                      <label className="text-[12px] font-medium text-muted-foreground">Environments</label>
                      <div className="flex flex-wrap gap-1.5">
                        {availableEnv.map((env) => (
                          <button key={env.name} type="button" onClick={() => toggleSet(selectedEnv, setSelectedEnv, env.name)}
                            className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                              selectedEnv.has(env.name)
                                ? 'bg-primary/10 text-primary border border-primary/20'
                                : 'text-muted-foreground border border-border hover:text-foreground hover:bg-accent'
                            }`}>
                            {env.name}
                          </button>
                        ))}
                      </div>
                    </div>
                  </div>
                )}
              </div>
            )}

            {/* Expiry */}
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Expiry</label>
              <div className="flex flex-wrap gap-1.5">
                {EXPIRY_OPTIONS.map((opt) => (
                  <button
                    key={opt.key}
                    type="button"
                    onClick={() => setExpiry(opt.key)}
                    className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                      expiry === opt.key
                        ? 'bg-primary/15 text-primary ring-1 ring-primary/20'
                        : 'text-muted-foreground border border-border hover:text-foreground hover:bg-accent'
                    }`}
                  >
                    {opt.label}
                  </button>
                ))}
              </div>
            </div>
          </div>

          <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-border shrink-0">
            <button type="button" onClick={onClose} className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
              Cancel
            </button>
            <button type="submit" disabled={!name.trim() || !selectedRole || create.isPending}
              className="rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
              style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}>
              Create key
            </button>
          </div>
        </form>
      )}
    </Modal>
  )
}
