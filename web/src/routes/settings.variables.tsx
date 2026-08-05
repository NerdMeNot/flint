import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Lock, Variable, Plus, ChevronDown, ChevronRight, AlertTriangle, Globe, Pencil, Eye, EyeOff } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { Modal } from '#/components/Modal'
import { Badge } from '#/components/Badge'
import { ConfirmButton } from '#/components/ConfirmButton'

export const Route = createFileRoute('/settings/variables')({
  component: VariablesPage,
})

function VariablesPage() {
  const { data: envData } = useSuspenseQuery(orpc.environments.list.queryOptions({ input: {} }))
  const environments = envData.items
  const { data: varsData } = useSuspenseQuery(orpc.envVariables.list.queryOptions({ input: {} }))
  const variables = varsData.items
  const { data: valuesData } = useSuspenseQuery(orpc.envVariables.values.queryOptions({ input: {} }))
  const values = valuesData.items
  const [expanded, setExpanded] = useState<string | null>(null)
  const [showCreate, setShowCreate] = useState(false)

  const varInvalidate = [orpc.envVariables.list.key(), orpc.envVariables.values.key()]
  const delVar = useAction((id: string) => client.envVariables.delete({ id }), { invalidate: varInvalidate })
  const setVal = useAction(
    (a: { variableId: string; environmentId?: string; value: string }) => client.envVariables.setValue(a),
    { invalidate: varInvalidate },
  )

  const globalVars = variables.filter((v) => v.scope === 'global')
  const envVars = variables.filter((v) => v.scope === 'environment')

  // Build lookup: variableId:environmentId → value
  const valueMap = new Map<string, string>()
  for (const v of values) {
    valueMap.set(`${v.variableId}:${v.environmentId}`, v.value)
  }

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="display-title text-lg text-foreground">Variables</h2>
          <p className="text-muted-foreground text-xs mt-0.5">
            {globalVars.length} global, {envVars.length} environment-scoped
          </p>
        </div>
        <button
          type="button"
          onClick={() => setShowCreate(true)}
          className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-colors"
          style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
        >
          <Plus size={12} />
          Add variable
        </button>
      </div>

      {/* Global variables */}
      <section className="space-y-2">
        <div className="flex items-center gap-2">
          <Globe size={13} className="text-muted-foreground" />
          <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">Global</h3>
          <span className="text-[11px] text-muted-foreground opacity-50">{globalVars.length}</span>
        </div>

        {globalVars.length === 0 ? (
          <p className="text-xs text-muted-foreground opacity-50 pl-5">No global variables.</p>
        ) : (
          <div className="space-y-1.5">
            {globalVars.map((variable) => (
              <GlobalVariableRow key={variable.id} variable={variable} />
            ))}
          </div>
        )}
      </section>

      {/* Environment variables */}
      <section className="space-y-2">
        <div className="flex items-center gap-2">
          <Variable size={13} className="text-muted-foreground" />
          <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">Per-environment</h3>
          <span className="text-[11px] text-muted-foreground opacity-50">{envVars.length}</span>
        </div>

        {envVars.length === 0 ? (
          <p className="text-xs text-muted-foreground opacity-50 pl-5">No environment variables.</p>
        ) : (
          <div className="space-y-1.5">
            {envVars.map((variable) => {
              const envValues = environments.map((env) => ({
                env,
                value: valueMap.get(`${variable.id}:${env.id}`),
              }))
              const setCount = envValues.filter((v) => v.value).length
              const missingCount = envValues.filter((v) => !v.value).length

              return (
                <div key={variable.id} className="island-shell !p-0 overflow-hidden">
                  <div
                    role="button"
                    onClick={() => setExpanded(expanded === variable.id ? null : variable.id)}
                    className="w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-accent transition-colors cursor-pointer"
                  >
                    {expanded === variable.id
                      ? <ChevronDown size={14} className="text-muted-foreground shrink-0" />
                      : <ChevronRight size={14} className="text-muted-foreground shrink-0" />}

                    {variable.isSecret
                      ? <Lock size={14} className="text-warning shrink-0" />
                      : <Variable size={14} className="text-muted-foreground shrink-0" />}

                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="font-mono font-semibold text-sm text-foreground">{variable.name}</span>
                        {variable.isSecret && <Badge variant="warning">SECRET</Badge>}
                      </div>
                      {variable.description && (
                        <p className="text-xs text-muted-foreground mt-0.5 truncate">{variable.description}</p>
                      )}
                    </div>

                    <div className="flex items-center gap-3 shrink-0 text-[12px] text-muted-foreground" onClick={(e) => e.stopPropagation()}>
                      <span>{setCount}/{environments.length} set</span>
                      {missingCount > 0 && (
                        <span className="flex items-center gap-0.5 text-destructive">
                          <AlertTriangle size={9} />
                          {missingCount}
                        </span>
                      )}
                      <ConfirmButton onConfirm={() => delVar.mutate(variable.id)} title="Delete variable" />
                    </div>
                  </div>

                  {expanded === variable.id && (
                    <div className="border-t border-border px-4 py-3 space-y-2">
                      {envValues.map(({ env, value }) => (
                        <ValueRow
                          key={env.id}
                          label={env.name}
                          value={value}
                          isSecret={variable.isSecret}
                          onSave={(val) => setVal.mutate({ variableId: variable.id, environmentId: env.id, value: val })}
                        />
                      ))}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </section>

      {showCreate && <CreateVariableModal onClose={() => setShowCreate(false)} />}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Global variable row — simple: name + single value
// ---------------------------------------------------------------------------

function GlobalVariableRow({ variable }: { variable: { id: string; name: string; description?: string; isSecret: boolean; value?: string } }) {
  const [editing, setEditing] = useState(false)
  const [editValue, setEditValue] = useState('')
  const [revealed, setRevealed] = useState(false)

  const varInvalidate = [orpc.envVariables.list.key(), orpc.envVariables.values.key()]
  const setVal = useAction(
    (value: string) => client.envVariables.setValue({ variableId: variable.id, value }),
    { invalidate: varInvalidate },
  )
  const delVar = useAction((id: string) => client.envVariables.delete({ id }), { invalidate: varInvalidate })

  function startEdit() {
    setEditValue(variable.isSecret ? '' : (variable.value ?? ''))
    setEditing(true)
  }

  function save() {
    if (editValue.trim()) {
      setVal.mutate(editValue)
    }
    setEditing(false)
  }

  return (
  <>
    <div className="island-shell px-4 py-3 flex items-center gap-3">
      {variable.isSecret
        ? <Lock size={13} className="text-warning shrink-0" />
        : <Globe size={13} className="text-muted-foreground shrink-0" />}

      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="font-mono font-semibold text-sm text-foreground">{variable.name}</span>
          {variable.isSecret && <Badge variant="warning">SECRET</Badge>}
        </div>
        {variable.description && (
          <p className="text-[12px] text-muted-foreground truncate">{variable.description}</p>
        )}
      </div>

      {!editing && (
        <div className="flex items-center gap-1.5 shrink-0">
          <span className="text-xs font-mono text-foreground">
            {variable.isSecret && !revealed ? '••••••••' : (variable.value || '—')}
          </span>
          {variable.isSecret && variable.value && (
            <button
              type="button"
              onClick={(e) => { e.stopPropagation(); setRevealed(!revealed) }}
              className="w-7 h-7 flex items-center justify-center rounded text-muted-foreground hover:text-foreground transition-colors"
              title={revealed ? 'Hide secret' : 'Reveal secret'}
            >
              {revealed ? <EyeOff size={11} /> : <Eye size={11} />}
            </button>
          )}
          <button type="button" onClick={startEdit} className="w-7 h-7 flex items-center justify-center rounded text-muted-foreground hover:text-foreground transition-colors">
            <Pencil size={11} />
          </button>
        </div>
      )}

      <ConfirmButton onConfirm={() => delVar.mutate(variable.id)} title="Delete variable" />
    </div>

    {editing && (
      <div className="island-shell p-3 space-y-2">
        <textarea
          value={editValue}
          onChange={(e) => setEditValue(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Escape') setEditing(false) }}
          autoFocus
          rows={2}
          placeholder={variable.isSecret ? 'Paste value (supports multi-line)' : 'Enter value (supports multi-line)'}
          className="w-full rounded-md border border-primary bg-transparent px-2.5 py-1.5 text-xs font-mono text-foreground focus:outline-none focus:ring-1 focus:ring-primary/40 resize-y"
        />
        <div className="flex items-center gap-1.5">
          <button type="button" onClick={save} className="rounded-md px-2.5 py-1 text-xs font-medium text-success border border-success hover:bg-success-subtle transition-colors">Save</button>
          <button type="button" onClick={() => setEditing(false)} className="rounded-md px-2.5 py-1 text-xs font-medium text-muted-foreground border border-border hover:bg-accent transition-colors">Cancel</button>
        </div>
      </div>
    )}
  </>
  )
}

// ---------------------------------------------------------------------------
// Editable value row for per-environment values
// ---------------------------------------------------------------------------

function ValueRow({ label, value, isSecret, onSave }: {
  label: string; value?: string; isSecret: boolean; onSave: (v: string) => void
}) {
  const [editing, setEditing] = useState(false)
  const [editValue, setEditValue] = useState('')
  const [revealed, setRevealed] = useState(false)

  function startEdit() {
    setEditValue(isSecret ? '' : (value ?? ''))
    setEditing(true)
  }

  function save() {
    if (editValue.trim()) onSave(editValue)
    setEditing(false)
  }

  return (
    <div className="space-y-1.5">
      <div className="flex items-center gap-3">
        <span className="w-[100px] shrink-0 text-xs font-medium text-foreground">{label}</span>

        {!editing && value ? (
          <div className="flex items-center gap-1.5 flex-1 min-w-0">
            <span className="text-xs font-mono text-foreground truncate">
              {isSecret && !revealed ? '••••••••' : value}
            </span>
            {isSecret && (
              <button
                type="button"
                onClick={(e) => { e.stopPropagation(); setRevealed(!revealed) }}
                className="w-7 h-7 flex items-center justify-center rounded text-muted-foreground hover:text-foreground transition-colors shrink-0"
                title={revealed ? 'Hide' : 'Reveal'}
              >
                {revealed ? <EyeOff size={11} /> : <Eye size={11} />}
              </button>
            )}
            <button type="button" onClick={startEdit} className="w-7 h-7 flex items-center justify-center rounded text-muted-foreground hover:text-foreground transition-colors shrink-0">
              <Pencil size={11} />
            </button>
          </div>
        ) : !editing ? (
          <button type="button" onClick={startEdit} className="text-xs text-primary/70 hover:text-primary transition-colors">
            + set value
          </button>
        ) : null}
      </div>

      {editing && (
        <div className="pl-[112px] space-y-1.5">
          <textarea
            value={editValue}
            onChange={(e) => setEditValue(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Escape') setEditing(false) }}
            autoFocus
            rows={2}
            placeholder={isSecret ? 'Paste value (supports multi-line, e.g. certificates, keys)' : 'Enter value (supports multi-line)'}
            className="w-full rounded-md border border-primary bg-transparent px-2.5 py-1.5 text-xs font-mono text-foreground focus:outline-none focus:ring-1 focus:ring-primary/40 resize-y"
          />
          <div className="flex items-center gap-1.5">
            <button type="button" onClick={save} className="rounded-md px-2.5 py-1 text-xs font-medium text-success border border-success hover:bg-success-subtle transition-colors">Save</button>
            <button type="button" onClick={() => setEditing(false)} className="rounded-md px-2.5 py-1 text-xs font-medium text-muted-foreground border border-border hover:bg-accent transition-colors">Cancel</button>
          </div>
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Create variable modal
// ---------------------------------------------------------------------------

function CreateVariableModal({ onClose }: { onClose: () => void }) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [scope, setScope] = useState<'global' | 'environment'>('environment')
  const [isSecret, setIsSecret] = useState(false)
  const [globalValue, setGlobalValue] = useState('')
  const [submitted, setSubmitted] = useState(false)

  const create = useAction(
    (data: { name: string; description?: string; scope: 'global' | 'environment'; isSecret: boolean; value?: string }) =>
      client.envVariables.create(data),
    {
      invalidate: [orpc.envVariables.list.key(), orpc.envVariables.values.key()],
      onSuccess: () => {
        setSubmitted(true)
        setTimeout(onClose, 1000)
      },
    },
  )

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!name.trim()) return
    create.mutate({
      name: name.toUpperCase().replace(/[^A-Z0-9_]/g, '_'),
      description: description || undefined,
      scope,
      isSecret,
      value: scope === 'global' ? globalValue : undefined,
    })
  }

  return (
    <Modal open onClose={onClose} title="Add Variable" subtitle="Define a new variable">
      {submitted ? (
        <div className="px-5 py-8 text-center">
          <p className="text-sm text-success font-medium">
            Variable created{scope === 'environment' ? ' — expand it to set values per environment' : ''}
          </p>
        </div>
      ) : (
        <form onSubmit={handleSubmit}>
          <div className="px-5 py-4 space-y-4">
            {/* Name */}
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Name <span className="text-destructive">*</span></label>
              <input
                type="text" required value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="CLUSTER_URL"
                className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm font-mono text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40"
              />
            </div>

            {/* Description */}
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Description</label>
              <input
                type="text" value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="What is this variable for?"
                className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40"
              />
            </div>

            {/* Scope */}
            <div className="space-y-2">
              <label className="text-xs font-medium text-foreground">Scope</label>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                <button
                  type="button"
                  onClick={() => setScope('global')}
                  className={`rounded-lg border p-3 text-left transition-colors ${
                    scope === 'global'
                      ? 'border-primary bg-accent'
                      : 'border-border hover:border-muted-foreground'
                  }`}
                >
                  <div className="flex items-center gap-2 mb-1">
                    <Globe size={13} className={scope === 'global' ? 'text-primary' : 'text-muted-foreground'} />
                    <span className={`text-xs font-medium ${scope === 'global' ? 'text-primary' : 'text-foreground'}`}>Global</span>
                  </div>
                  <p className="text-[11px] text-muted-foreground">Single value, same everywhere</p>
                </button>
                <button
                  type="button"
                  onClick={() => setScope('environment')}
                  className={`rounded-lg border p-3 text-left transition-colors ${
                    scope === 'environment'
                      ? 'border-primary bg-accent'
                      : 'border-border hover:border-muted-foreground'
                  }`}
                >
                  <div className="flex items-center gap-2 mb-1">
                    <Variable size={13} className={scope === 'environment' ? 'text-primary' : 'text-muted-foreground'} />
                    <span className={`text-xs font-medium ${scope === 'environment' ? 'text-primary' : 'text-foreground'}`}>Per-environment</span>
                  </div>
                  <p className="text-[11px] text-muted-foreground">Different value per environment</p>
                </button>
              </div>
            </div>

            {/* Secret toggle */}
            <label className="flex items-center gap-2 cursor-pointer">
              <input type="checkbox" checked={isSecret} onChange={(e) => setIsSecret(e.target.checked)} className="rounded border-border" />
              <div>
                <span className="text-xs font-medium text-foreground">Secret</span>
                <p className="text-[12px] text-muted-foreground">Encrypted, never displayed after creation</p>
              </div>
            </label>

            {/* Global value (only for global scope) */}
            {scope === 'global' && (
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-foreground">Value</label>
                <input
                  type={isSecret ? 'password' : 'text'}
                  value={globalValue}
                  onChange={(e) => setGlobalValue(e.target.value)}
                  placeholder={isSecret ? 'Enter secret value' : 'Enter value'}
                  className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm font-mono text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40"
                />
              </div>
            )}
          </div>

          <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-border">
            <button type="button" onClick={onClose} className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
              Cancel
            </button>
            <button type="submit" disabled={!name.trim()}
              className="rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
              style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}>
              Add variable
            </button>
          </div>
        </form>
      )}
    </Modal>
  )
}
