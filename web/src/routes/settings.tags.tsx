import { createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { useState, useRef, useEffect } from 'react'
import { Tag, Plus, ChevronRight, ChevronDown, X, GripVertical } from 'lucide-react'
import { orpc, client } from '#/lib/orpc'
import { useAction } from '#/hooks/use-action'
import { ConfirmButton } from '#/components/ConfirmButton'
import type { TagKey } from '#/lib/api/types'

export const Route = createFileRoute('/settings/tags')({
  component: TagsPage,
})

const SWATCHES = [
  '#6366f1', '#8b5cf6', '#a855f7', '#d946ef', '#ec4899', '#f43f5e',
  '#ef4444', '#f97316', '#f59e0b', '#eab308', '#84cc16', '#22c55e',
  '#10b981', '#14b8a6', '#06b6d4', '#0ea5e9', '#3b82f6', '#2563eb',
  '#64748b', '#78716c', '#0d9488', '#7c3aed', '#db2777', '#475569',
]

function TagsPage() {
  const { data } = useSuspenseQuery(orpc.tags.registry.list.queryOptions({ input: {} }))
  const tagKeys = data.items
  const [expanded, setExpanded] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  return (
    <div className="space-y-5">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="display-title text-lg text-foreground">Tags</h2>
          <p className="text-muted-foreground text-xs mt-0.5">
            {tagKeys.length} curated {tagKeys.length === 1 ? 'key' : 'keys'} — projects tag as{' '}
            <span className="font-mono">key:value</span>. Expand a key to edit its values.
          </p>
        </div>
        <button
          type="button"
          onClick={() => setCreating(true)}
          className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-colors shrink-0"
          style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
        >
          <Plus size={12} />
          Add key
        </button>
      </div>

      {tagKeys.length === 0 && !creating ? (
        <div className="island-shell p-12 flex flex-col items-center gap-3 text-muted-foreground">
          <Tag size={32} strokeWidth={1.2} />
          <span className="text-sm">No curated tag keys yet.</span>
        </div>
      ) : (
        <div className="island-shell !p-0 overflow-hidden divide-y divide-border">
          {creating && <CreateRow onClose={() => setCreating(false)} />}
          {tagKeys.map((tk) => (
            <TagKeyRow
              key={tk.id}
              tagKey={tk}
              expanded={expanded === tk.id}
              onToggle={() => setExpanded(expanded === tk.id ? null : tk.id)}
            />
          ))}
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// One expandable key row. Collapsed: a scannable summary. Expanded: edit label,
// color, and values inline — every commit persists, no modal.
// ---------------------------------------------------------------------------

function TagKeyRow({ tagKey, expanded, onToggle }: {
  tagKey: TagKey
  expanded: boolean
  onToggle: () => void
}) {
  const invalidate = [orpc.tags.registry.list.key()]
  const update = useAction(
    (d: { id: string; label: string; allowedValues: string[]; color: string }) => client.tags.registry.update(d),
    { invalidate },
  )
  const del = useAction((id: string) => client.tags.registry.delete({ id }), { invalidate })

  // Local editable state (authoritative for this row's own fields while open).
  const [label, setLabel] = useState(tagKey.label)
  const [color, setColor] = useState(tagKey.color)
  const [values, setValues] = useState<string[]>(tagKey.allowedValues)

  function persist(next: { label?: string; color?: string; values?: string[] }) {
    update.mutate({
      id: tagKey.id,
      label: (next.label ?? label).trim() || tagKey.key,
      color: next.color ?? color,
      allowedValues: next.values ?? values,
    })
  }

  return (
    <div>
      <div
        role="button"
        onClick={onToggle}
        className="w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-accent/30 transition-colors cursor-pointer"
      >
        {expanded
          ? <ChevronDown size={15} className="text-muted-foreground shrink-0" />
          : <ChevronRight size={15} className="text-muted-foreground shrink-0" />}
        <span className="w-2.5 h-2.5 rounded-full shrink-0" style={{ backgroundColor: color }} />
        <div className="flex items-baseline gap-2 min-w-0 flex-1">
          <span className="font-mono font-semibold text-sm text-foreground truncate">{tagKey.key}</span>
          <span className="text-xs text-muted-foreground truncate">· {label}</span>
        </div>
        <span className="text-[12px] text-muted-foreground shrink-0">
          {values.length === 0 ? 'free-form' : `${values.length} ${values.length === 1 ? 'value' : 'values'}`}
        </span>
        <ConfirmButton onConfirm={() => del.mutate(tagKey.id)} title="Delete key" />
      </div>

      {expanded && (
        <div className="border-t border-border/50 bg-accent/10 px-4 py-4 pl-11 space-y-4">
          <div className="grid sm:grid-cols-2 gap-4">
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Label</label>
              <input
                type="text"
                value={label}
                onChange={(e) => setLabel(e.target.value)}
                onBlur={() => { if (label.trim() && label !== tagKey.label) persist({ label }) }}
                className="w-full rounded-lg border border-border bg-transparent px-3 py-1.5 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-ring/40"
              />
            </div>
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-foreground">Color</label>
              <div className="flex flex-wrap gap-1.5 pt-1">
                {SWATCHES.map((c) => (
                  <button
                    key={c}
                    type="button"
                    onClick={() => { setColor(c); persist({ color: c }) }}
                    className={`w-5 h-5 rounded-full transition-transform ${color === c ? 'ring-2 ring-offset-2 ring-offset-background ring-foreground/40 scale-110' : ''}`}
                    style={{ backgroundColor: c }}
                    title={c}
                  />
                ))}
              </div>
            </div>
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-foreground">Allowed values</label>
            <ValueListEditor
              values={values}
              onChange={(next) => { setValues(next); persist({ values: next }) }}
              prefix={tagKey.key + ':'}
            />
            <p className="text-[11px] text-muted-foreground">Each value is one allowed option. Leave empty to allow free-form values for this key.</p>
          </div>
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Inline new-key row (no modal). `key` is the immutable namespace.
// ---------------------------------------------------------------------------

function CreateRow({ onClose }: { onClose: () => void }) {
  const [key, setKey] = useState('')
  const [label, setLabel] = useState('')
  const create = useAction(
    (d: { key: string; label: string }) => client.tags.registry.create(d),
    { invalidate: [orpc.tags.registry.list.key()], onSuccess: onClose },
  )

  function submit() {
    const k = key.trim().toLowerCase().replace(/[^a-z0-9_-]/g, '')
    if (!k || !label.trim()) return
    create.mutate({ key: k, label: label.trim() })
  }

  return (
    <div className="bg-primary/5 px-4 py-3 flex flex-wrap items-end gap-3">
      <div className="space-y-1 flex-1 min-w-[120px]">
        <label className="text-[11px] font-medium text-muted-foreground">Key</label>
        <input
          type="text" autoFocus value={key}
          onChange={(e) => setKey(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') submit(); if (e.key === 'Escape') onClose() }}
          placeholder="domain"
          className="w-full rounded-lg border border-border bg-transparent px-3 py-1.5 text-sm font-mono text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40"
        />
      </div>
      <div className="space-y-1 flex-1 min-w-[120px]">
        <label className="text-[11px] font-medium text-muted-foreground">Label</label>
        <input
          type="text" value={label}
          onChange={(e) => setLabel(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') submit(); if (e.key === 'Escape') onClose() }}
          placeholder="Domain"
          className="w-full rounded-lg border border-border bg-transparent px-3 py-1.5 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/40"
        />
      </div>
      <div className="flex items-center gap-1.5">
        <button
          type="button" onClick={submit}
          disabled={create.isPending || !key.trim() || !label.trim()}
          className="rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-colors disabled:opacity-40"
          style={{ background: 'color-mix(in oklab, var(--ring), black 20%)' }}
        >
          Create
        </button>
        <button
          type="button" onClick={onClose}
          className="rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
        >
          Cancel
        </button>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Allowed-values editor — one row per value (prefix preview + remove), with a
// dedicated add row. Rows are drag-to-reorder (the saved order is the order
// projects see in pickers). Clearer than cramming every value into one field.
// ---------------------------------------------------------------------------

function reorder(list: string[], from: number, to: number): string[] {
  const next = [...list]
  const [item] = next.splice(from, 1)
  next.splice(to, 0, item)
  return next
}

function ValueListEditor({ values, onChange, prefix }: {
  values: string[]
  onChange: (next: string[]) => void
  prefix?: string
}) {
  const [draft, setDraft] = useState('')
  // Local working copy so a drag reorders smoothly and persists once on drop,
  // rather than firing an update on every dragged-over row.
  const [items, setItems] = useState<string[]>(values)
  const dragFrom = useRef<number | null>(null)
  useEffect(() => { setItems(values) }, [values])

  function add() {
    const v = draft.trim()
    if (!v || items.includes(v)) return
    onChange([...items, v])
    setDraft('')
  }

  function commitIfChanged() {
    dragFrom.current = null
    if (items.length !== values.length || items.some((v, i) => v !== values[i])) onChange(items)
  }

  return (
    <div className="space-y-1.5">
      {items.map((v, i) => (
        <div
          key={v}
          draggable
          onDragStart={() => { dragFrom.current = i }}
          onDragEnter={() => {
            if (dragFrom.current === null || dragFrom.current === i) return
            setItems((prev) => reorder(prev, dragFrom.current!, i))
            dragFrom.current = i
          }}
          onDragOver={(e) => e.preventDefault()}
          onDragEnd={commitIfChanged}
          className={`flex items-center gap-2 rounded-lg border border-border px-2.5 py-1.5 transition-opacity ${dragFrom.current === i ? 'opacity-50' : ''}`}
        >
          <GripVertical size={14} className="text-muted-foreground/40 hover:text-muted-foreground cursor-grab active:cursor-grabbing shrink-0" />
          <span className="flex-1 text-sm font-mono text-foreground truncate">
            <span className="text-muted-foreground">{prefix}</span>{v}
          </span>
          <button
            type="button"
            onClick={() => onChange(items.filter((x) => x !== v))}
            className="text-muted-foreground/60 hover:text-destructive transition-colors shrink-0"
            aria-label={`Remove ${v}`}
          >
            <X size={13} />
          </button>
        </div>
      ))}
      <div className="flex items-center gap-2">
        <div className="flex flex-1 items-center rounded-lg border border-dashed border-border focus-within:ring-2 focus-within:ring-ring/40">
          {prefix && <span className="pl-2.5 text-sm font-mono text-muted-foreground/50 shrink-0">{prefix}</span>}
          <input
            type="text"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); add() } }}
            placeholder="add a value"
            className="flex-1 min-w-0 bg-transparent px-2 py-1.5 text-sm font-mono text-foreground placeholder:text-muted-foreground/50 focus:outline-none"
          />
        </div>
        <button
          type="button"
          onClick={add}
          disabled={!draft.trim()}
          className="rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors disabled:opacity-40 shrink-0"
        >
          Add
        </button>
      </div>
    </div>
  )
}
