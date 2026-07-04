import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Cloud, Server, Plus, FlaskConical, Trash2, Check, KeyRound } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { PageHeader } from '#/components/PageHeader'
import { Badge } from '#/components/Badge'
import { EmptyState } from '#/components/EmptyState'
import { Modal } from '#/components/Modal'
import { FormSelect } from '#/components/FormSelect'
import type { ComputeProvider } from '#/lib/api/types'

export const Route = createFileRoute('/settings/providers')({
  component: ProvidersPage,
})

const inputClass =
  'w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40 disabled:opacity-60'

function ProvidersPage() {
  const { data } = useSuspenseQuery(orpc.computeProviders.list.queryOptions({}))
  const providers = data.providers
  const [editing, setEditing] = useState<ComputeProvider | null>(null)
  const [creating, setCreating] = useState(false)

  return (
    <div className="space-y-6">
      <PageHeader
        title="Compute Providers"
        subtitle={providers.length === 0 ? 'No providers yet' : `${providers.length} ${providers.length === 1 ? 'provider' : 'providers'}`}
        action={
          <button
            onClick={() => setCreating(true)}
            className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium text-white transition-colors"
            style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
          >
            <Plus size={14} /> New provider
          </button>
        }
      />

      {providers.length === 0 ? (
        <EmptyState
          icon={Cloud}
          message="No compute providers configured. The built-in static provider covers bring-your-own machines; add a cloud provider for elastic pools."
        />
      ) : (
        <div className="island-shell !p-0 overflow-hidden divide-y divide-border">
          {providers.map((p, i) => (
            <button
              key={p.id}
              onClick={() => setEditing(p)}
              className="flex w-full items-center gap-3.5 px-4 py-3.5 text-left hover:bg-accent/30 transition-colors rise-in"
              style={{ animationDelay: `${i * 35 + 20}ms` }}
            >
              <span className={`flex h-9 w-9 items-center justify-center rounded-lg shrink-0 ${p.type === 'static' ? 'bg-muted text-muted-foreground' : 'bg-primary/10 text-primary'}`}>
                {p.type === 'static' ? <Server size={16} /> : <Cloud size={16} />}
              </span>
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-semibold text-foreground font-mono">{p.name}</span>
                  <Badge variant="neutral">{p.type}</Badge>
                  {p.hasCredentials && (
                    <span className="inline-flex items-center gap-1 text-[11px] text-muted-foreground">
                      <KeyRound size={11} /> stored credentials
                    </span>
                  )}
                </div>
              </div>
              <TestButton name={p.name} />
            </button>
          ))}
        </div>
      )}

      {(creating || editing) && (
        <ProviderModal
          provider={editing ?? undefined}
          onClose={() => { setCreating(false); setEditing(null) }}
        />
      )}
    </div>
  )
}

// TestButton dry-runs a Quote against the stored provider — verifies config
// and credentials before a pool depends on it.
function TestButton({ name }: { name: string }) {
  const [result, setResult] = useState<string | null>(null)
  const test = useAction(() => client.computeProviders.test({ name }), {
    onSuccess: (r) => setResult(r.ok ? (r.elastic ? `ok — ${r.offers?.length ?? 0} offers` : 'ok — static') : `failed: ${r.error}`),
  })
  return (
    <span className="flex items-center gap-2 shrink-0" onClick={(e) => e.stopPropagation()}>
      {result && (
        <span className={`text-[11px] ${result.startsWith('ok') ? 'text-success' : 'text-danger'}`}>{result}</span>
      )}
      <span
        role="button"
        onClick={() => test.mutate(undefined)}
        className="flex items-center gap-1 rounded-md border border-border px-2 py-1 text-[11px] font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
      >
        <FlaskConical size={11} /> {test.isPending ? 'Testing…' : 'Test'}
      </span>
    </span>
  )
}

// ProviderModal creates/edits a provider. Config and credentials are free-form
// JSON — each provider type documents its own keys; credentials are
// envelope-encrypted server-side and never returned.
function ProviderModal({ provider, onClose }: { provider?: ComputeProvider; onClose: () => void }) {
  const editing = !!provider
  const [name, setName] = useState(provider?.name ?? '')
  const [type, setType] = useState(provider?.type ?? 'aws')
  const [config, setConfig] = useState(JSON.stringify(provider?.config ?? {}, null, 2))
  const [credentials, setCredentials] = useState('')
  const [jsonError, setJsonError] = useState('')
  const [showDelete, setShowDelete] = useState(false)

  const invalidate = [orpc.computeProviders.list.key()]
  const save = useAction(
    (data: Parameters<typeof client.computeProviders.create>[0]) =>
      editing ? client.computeProviders.update(data) : client.computeProviders.create(data),
    { invalidate, onSuccess: onClose },
  )
  const remove = useAction(() => client.computeProviders.delete({ name }), {
    invalidate,
    onSuccess: onClose,
  })

  const submit = () => {
    let cfg: Record<string, unknown> | undefined
    let creds: Record<string, unknown> | undefined
    try {
      cfg = config.trim() ? JSON.parse(config) : undefined
      creds = credentials.trim() ? JSON.parse(credentials) : undefined
    } catch {
      setJsonError('Config and credentials must be valid JSON objects')
      return
    }
    setJsonError('')
    save.mutate({ name: name.trim(), type, config: cfg, credentials: creds })
  }

  return (
    <Modal open onClose={onClose} title={editing ? `Edit ${provider.name}` : 'New compute provider'}>
      <div className="space-y-4">
        <label className="block space-y-1.5">
          <span className="text-xs font-medium text-muted-foreground">Name</span>
          <input className={`${inputClass} font-mono`} value={name} onChange={(e) => setName(e.target.value)} placeholder="aws-us-east" disabled={editing} />
        </label>
        <label className="block space-y-1.5">
          <span className="text-xs font-medium text-muted-foreground">Type</span>
          <FormSelect
            value={type}
            onChange={setType}
            options={[
              { key: 'aws', label: 'aws — EC2 (spot-capable)' },
              { key: 'static', label: 'static — bring your own machines' },
            ]}
          />
        </label>
        <label className="block space-y-1.5">
          <span className="text-xs font-medium text-muted-foreground">Config (JSON)</span>
          <textarea
            className={`${inputClass} font-mono !text-xs min-h-[90px]`}
            value={config}
            onChange={(e) => setConfig(e.target.value)}
            placeholder='{ "region": "us-east-1" }'
            spellCheck={false}
          />
        </label>
        <label className="block space-y-1.5">
          <span className="text-xs font-medium text-muted-foreground">Credentials (JSON, optional)</span>
          <textarea
            className={`${inputClass} font-mono !text-xs min-h-[70px]`}
            value={credentials}
            onChange={(e) => setCredentials(e.target.value)}
            placeholder={provider?.hasCredentials ? 'stored — leave blank to keep' : '{ "accessKeyId": "…", "secretAccessKey": "…" }'}
            spellCheck={false}
          />
          <span className="block text-[11px] text-muted-foreground/70">
            Most clouds should omit credentials and use the ambient chain (instance role / env). Stored credentials are envelope-encrypted and never returned.
          </span>
        </label>

        {(jsonError || save.isError) && (
          <p className="text-xs text-danger">{jsonError || ((save.error as Error)?.message ?? 'Save failed')}</p>
        )}

        <div className="flex items-center justify-between gap-2">
          <div>
            {editing && (
              <button
                onClick={() => setShowDelete(true)}
                className="flex items-center gap-1.5 rounded-lg border border-danger/40 px-3 py-1.5 text-xs font-medium text-danger hover:bg-danger/10 transition-colors"
              >
                <Trash2 size={13} /> Delete
              </button>
            )}
          </div>
          <div className="flex items-center gap-2">
            <button onClick={onClose} className="rounded-lg border border-border px-3 py-1.5 text-xs font-medium hover:bg-accent transition-colors">
              Cancel
            </button>
            <button
              onClick={submit}
              disabled={!name.trim() || save.isPending}
              className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-50"
              style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
            >
              <Check size={13} /> {save.isPending ? 'Saving…' : editing ? 'Save' : 'Create'}
            </button>
          </div>
        </div>

        {showDelete && (
          <div className="rounded-lg border border-danger/40 bg-danger/5 p-3 space-y-2">
            <p className="text-xs text-muted-foreground">
              Deleting a provider that pools reference is refused — repoint them first.
            </p>
            <div className="flex justify-end gap-2">
              <button onClick={() => setShowDelete(false)} className="rounded-lg border border-border px-3 py-1.5 text-xs font-medium hover:bg-accent transition-colors">
                Cancel
              </button>
              <button
                onClick={() => remove.mutate(undefined)}
                className="rounded-lg bg-danger px-3 py-1.5 text-xs font-medium text-white hover:opacity-90 transition-colors"
              >
                Delete provider
              </button>
            </div>
            {remove.isError && <p className="text-xs text-danger">{(remove.error as Error)?.message ?? 'Delete failed'}</p>}
          </div>
        )}
      </div>
    </Modal>
  )
}
