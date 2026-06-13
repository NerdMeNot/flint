import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Tag, Plus, Trash2, Pencil, X } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { Modal } from '#/components/Modal'
import type { TagKey } from '#/lib/api/types'

export const Route = createFileRoute('/settings/tags')({
  component: TagsPage,
})

const SWATCHES = ['#6366f1', '#ef4444', '#10b981', '#f59e0b', '#06b6d4', '#8b5cf6', '#ec4899', '#64748b']

function TagsPage() {
  const { data } = useSuspenseQuery(orpc.tags.registry.list.queryOptions({ input: {} }))
  const tagKeys = data.items
  const [showCreate, setShowCreate] = useState(false)
  const [editing, setEditing] = useState<TagKey | null>(null)

  const del = useAction((id: string) => client.tags.registry.delete({ id }), {
    invalidate: [orpc.tags.registry.list.key()],
  })

  return (
    <div className="space-y-5">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="display-title text-lg text-foreground">Tags</h2>
          <p className="text-muted-foreground text-xs mt-0.5">
            {tagKeys.length} curated {tagKeys.length === 1 ? 'key' : 'keys'} — projects carry tags as{' '}
            <span className="font-mono">key:value</span>
          </p>
        </div>
        <button
          type="button"
          onClick={() => setShowCreate(true)}
          className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-colors"
          style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
        >
          <Plus size={12} />
          Add key
        </button>
      </div>

      {tagKeys.length === 0 ? (
        <div className="island-shell p-12 flex flex-col items-center gap-3 text-muted-foreground">
          <Tag size={32} strokeWidth={1.2} />
          <span className="text-sm">No curated tag keys yet.</span>
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {tagKeys.map((tk, i) => (
            <div
              key={tk.id}
              className="feature-card rise-in p-5 space-y-3"
              style={{ animationDelay: `${i * 50 + 30}ms` }}
            >
              <div className="flex items-start justify-between gap-2">
                <div className="flex items-center gap-2 min-w-0">
                  <span className="w-2.5 h-2.5 rounded-full shrink-0" style={{ backgroundColor: tk.color }} />
                  <div className="min-w-0">
                    <h3 className="font-semibold text-sm text-foreground truncate">{tk.label}</h3>
                    <p className="text-xs text-muted-foreground font-mono truncate">{tk.key}</p>
                  </div>
                </div>
                <div className="flex items-center gap-1 shrink-0">
                  <button
                    type="button"
                    onClick={() => setEditing(tk)}
                    className="w-6 h-6 flex items-center justify-center rounded text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
                  >
                    <Pencil size={12} />
                  </button>
                  <button
                    type="button"
                    onClick={() => del.mutate(tk.id)}
                    className="w-6 h-6 flex items-center justify-center rounded text-muted-foreground hover:text-destructive hover:bg-destructive/5 transition-colors"
                  >
                    <Trash2 size={12} />
                  </button>
                </div>
              </div>

              {tk.allowedValues.length > 0 ? (
                <div className="flex flex-wrap gap-1.5">
                  {tk.allowedValues.map((v) => (
                    <span key={v} className="rounded-md bg-secondary border border-border px-2 py-0.5 text-[12px] font-mono text-muted-foreground">
                      {tk.key}:{v}
                    </span>
                  ))}
                </div>
              ) : (
                <p className="text-[12px] text-muted-foreground opacity-50">Free-form values</p>
              )}
            </div>
          ))}
        </div>
      )}

      {showCreate && <TagKeyModal onClose={() => setShowCreate(false)} />}
      {editing && <TagKeyModal tagKey={editing} onClose={() => setEditing(null)} />}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Chip/token input — each committed value becomes a removable chip. Enter or
// comma commits; Backspace on an empty field removes the last chip. Duplicates
// and blanks are dropped. `prefix` previews the `key:` namespace on each chip.
// ---------------------------------------------------------------------------

function ChipInput({ values, onChange, prefix, placeholder }: {
  values: string[]
  onChange: (next: string[]) => void
  prefix?: string
  placeholder?: string
}) {
  const [draft, setDraft] = useState('')

  function commit(raw: string) {
    const v = raw.trim()
    if (!v) return
    if (!values.includes(v)) onChange([...values, v])
    setDraft('')
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault()
      commit(draft)
    } else if (e.key === 'Backspace' && draft === '' && values.length > 0) {
      onChange(values.slice(0, -1))
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-1.5 rounded-lg border border-border bg-transparent px-2 py-1.5 focus-within:ring-2 focus-within:ring-ring/40">
      {values.map((v) => (
        <span key={v} className="flex items-center gap-1 rounded-md bg-secondary border border-border px-2 py-0.5 text-[12px] font-mono text-foreground">
          <span className="text-muted-foreground">{prefix}</span>{v}
          <button
            type="button"
            onClick={() => onChange(values.filter((x) => x !== v))}
            className="text-muted-foreground hover:text-destructive transition-colors"
            aria-label={`Remove ${v}`}
          >
            <X size={11} />
          </button>
        </span>
      ))}
      <input
        type="text"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={handleKeyDown}
        onBlur={() => commit(draft)}
        placeholder={placeholder}
        className="flex-1 min-w-[8ch] bg-transparent px-1 py-0.5 text-sm font-mono text-foreground placeholder:text-muted-foreground/50 focus:outline-none"
      />
    </div>
  )
}

// ---------------------------------------------------------------------------
// Create / edit modal — `key` is immutable once created (it's the namespace).
// ---------------------------------------------------------------------------

function TagKeyModal({ tagKey, onClose }: { tagKey?: TagKey; onClose: () => void }) {
  const isEdit = !!tagKey
  const [key, setKey] = useState(tagKey?.key ?? '')
  const [label, setLabel] = useState(tagKey?.label ?? '')
  const [values, setValues] = useState<string[]>(tagKey?.allowedValues ?? [])
  const [color, setColor] = useState(tagKey?.color ?? SWATCHES[0])

  const invalidate = [orpc.tags.registry.list.key()]
  const create = useAction(
    (d: { key: string; label: string; allowedValues?: string[]; color?: string }) => client.tags.registry.create(d),
    { invalidate, onSuccess: onClose },
  )
  const update = useAction(
    (d: { id: string; label: string; allowedValues?: string[]; color?: string }) => client.tags.registry.update(d),
    { invalidate, onSuccess: onClose },
  )

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!label.trim()) return
    if (isEdit) {
      update.mutate({ id: tagKey.id, label: label.trim(), allowedValues: values, color })
    } else {
      if (!key.trim()) return
      create.mutate({ key: key.trim().toLowerCase().replace(/[^a-z0-9_-]/g, ''), label: label.trim(), allowedValues: values, color })
    }
  }

  const pending = create.isPending || update.isPending

  return (
    <Modal open onClose={onClose} title={isEdit ? 'Edit tag key' : 'Add tag key'} subtitle={isEdit ? tagKey.key : 'Define a curated namespace'}>
      <form onSubmit={handleSubmit}>
        <div className="px-5 py-4 space-y-4">
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-foreground">Key <span className="text-destructive">*</span></label>
            <input
              type="text"
              required
              value={key}
              disabled={isEdit}
              onChange={(e) => setKey(e.target.value)}
              placeholder="domain"
              className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm font-mono text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40 disabled:opacity-50"
            />
            <p className="text-[11px] text-muted-foreground">Lowercase namespace, e.g. <span className="font-mono">domain</span>, <span className="font-mono">tier</span>. Immutable once created.</p>
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-foreground">Label <span className="text-destructive">*</span></label>
            <input
              type="text"
              required
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder="Domain"
              className="w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40"
            />
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-foreground">Allowed values</label>
            <ChipInput
              values={values}
              onChange={setValues}
              prefix={(key.trim() || 'key') + ':'}
              placeholder={values.length === 0 ? 'Type a value, press Enter' : 'Add another…'}
            />
            <p className="text-[11px] text-muted-foreground">Press Enter to add each value. Leave empty to allow free-form values.</p>
          </div>

          <div className="space-y-2">
            <label className="text-xs font-medium text-foreground">Color</label>
            <div className="flex flex-wrap gap-2">
              {SWATCHES.map((c) => (
                <button
                  key={c}
                  type="button"
                  onClick={() => setColor(c)}
                  className={`w-6 h-6 rounded-full transition-transform ${color === c ? 'ring-2 ring-offset-2 ring-offset-background ring-foreground/40 scale-110' : ''}`}
                  style={{ backgroundColor: c }}
                  title={c}
                />
              ))}
            </div>
          </div>
        </div>

        <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-border">
          <button type="button" onClick={onClose} className="rounded-lg border border-border px-3.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
            Cancel
          </button>
          <button
            type="submit"
            disabled={pending || !label.trim() || (!isEdit && !key.trim())}
            className="rounded-lg px-3.5 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
            style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
          >
            {isEdit ? 'Save changes' : 'Add key'}
          </button>
        </div>
      </form>
    </Modal>
  )
}
