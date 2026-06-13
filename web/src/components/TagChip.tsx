import { X } from 'lucide-react'
import type { TagKey } from '#/lib/api/types'

// Renders a project tag as a structured chip. A `key:value` tag whose key is in
// the registry shows as "Label: value" in the key's color; everything else
// (free tags, or key:value with an unknown key) renders as a neutral chip.
export function TagChip({ tag, registry, onRemove }: {
  tag: string
  registry: Map<string, TagKey>
  onRemove?: () => void
}) {
  const idx = tag.indexOf(':')
  const key = idx >= 0 ? tag.slice(0, idx) : ''
  const value = idx >= 0 ? tag.slice(idx + 1) : ''
  const meta = key ? registry.get(key) : undefined

  if (meta && value) {
    return (
      <span
        className="inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-[12px] font-medium"
        style={{
          color: meta.color,
          borderColor: `color-mix(in oklab, ${meta.color} 35%, transparent)`,
          background: `color-mix(in oklab, ${meta.color} 12%, transparent)`,
        }}
      >
        <span className="opacity-70">{meta.label}:</span>{value}
        {onRemove && <RemoveButton onRemove={onRemove} label={tag} />}
      </span>
    )
  }

  return (
    <span className="inline-flex items-center gap-1 rounded-md bg-secondary border border-border px-2 py-0.5 text-[12px] font-medium text-muted-foreground">
      {tag}
      {onRemove && <RemoveButton onRemove={onRemove} label={tag} />}
    </span>
  )
}

function RemoveButton({ onRemove, label }: { onRemove: () => void; label: string }) {
  return (
    <button
      type="button"
      onClick={onRemove}
      aria-label={`Remove ${label}`}
      className="opacity-60 hover:opacity-100 transition-opacity"
    >
      <X size={11} />
    </button>
  )
}
