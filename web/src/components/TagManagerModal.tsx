import { useState } from 'react'
import { Search, Check } from 'lucide-react'
import { Modal } from '#/components/Modal'

export interface TagGroup {
  key: string
  label: string
  color: string
  values: string[]
}

// A roomy, grouped tag picker. Tags are laid out by key in a responsive grid so
// many keys/values stay browsable; search narrows within every group at once and
// toggling applies immediately. Used both to manage a project's tags and to build
// a tag filter — pass `counts` to show how many items carry each tag.
export function TagManagerModal({
  open,
  onClose,
  groups,
  applied,
  onToggle,
  onClear,
  counts,
  title = 'Manage tags',
  subtitle = 'Apply curated tags to this project',
}: {
  open: boolean
  onClose: () => void
  groups: TagGroup[]
  applied: Set<string>
  onToggle: (tag: string) => void
  onClear?: () => void
  counts?: Map<string, number>
  title?: string
  subtitle?: string
}) {
  const [query, setQuery] = useState('')
  const q = query.toLowerCase().trim()

  const filtered = groups
    .map((g) => ({
      ...g,
      values: q
        ? g.values.filter((v) => v.toLowerCase().includes(q) || g.label.toLowerCase().includes(q))
        : g.values,
    }))
    .filter((g) => g.values.length > 0)

  return (
    <Modal open={open} onClose={onClose} title={title} subtitle={subtitle} wide>
      {groups.length === 0 ? (
        <div className="px-5 py-10 text-center text-sm text-muted-foreground">
          No tags declared — add some in Settings → Tags.
        </div>
      ) : (
        <>
          <div className="flex items-center gap-2 px-5 py-3 border-b border-border shrink-0">
            <Search size={14} className="text-muted-foreground shrink-0" />
            <input
              type="text"
              autoFocus
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search tags…"
              className="flex-1 bg-transparent text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none"
            />
          </div>

          <div className="flex-1 overflow-y-auto px-5 py-4">
            {filtered.length === 0 ? (
              <p className="py-6 text-center text-sm text-muted-foreground">No matching tags</p>
            ) : (
              <div className="grid gap-x-6 gap-y-5 sm:grid-cols-2">
                {filtered.map((g) => (
                  <div key={g.key}>
                    <div className="flex items-center gap-1.5 mb-2">
                      <span className="w-2 h-2 rounded-full shrink-0" style={{ background: g.color }} />
                      <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/70">
                        {g.label}
                      </span>
                    </div>
                    <div className="flex flex-wrap gap-1.5">
                      {g.values.map((v) => {
                        const tag = `${g.key}:${v}`
                        const on = applied.has(tag)
                        const count = counts?.get(tag)
                        return (
                          <button
                            key={tag}
                            type="button"
                            onClick={() => onToggle(tag)}
                            className={`inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-[12px] font-medium transition-colors ${
                              on ? '' : 'border-border text-muted-foreground hover:text-foreground hover:border-foreground/30'
                            }`}
                            style={
                              on
                                ? {
                                    color: g.color,
                                    borderColor: `color-mix(in oklab, ${g.color} 35%, transparent)`,
                                    background: `color-mix(in oklab, ${g.color} 14%, transparent)`,
                                  }
                                : undefined
                            }
                          >
                            {v}
                            {count != null && <span className="text-[10px] opacity-50 tabular-nums">{count}</span>}
                            {on && <Check size={11} className="shrink-0" />}
                          </button>
                        )
                      })}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>

          <div className="flex items-center justify-between px-5 py-3 border-t border-border shrink-0">
            <span className="text-xs text-muted-foreground">
              {applied.size} tag{applied.size === 1 ? '' : 's'} applied
              {applied.size > 0 && onClear && (
                <button
                  type="button"
                  onClick={onClear}
                  className="ml-2 text-muted-foreground/70 hover:text-foreground underline underline-offset-2 transition-colors"
                >
                  Clear all
                </button>
              )}
            </span>
            <button
              type="button"
              onClick={onClose}
              className="rounded-md bg-primary px-3 py-1.5 text-xs font-semibold text-primary-foreground hover:bg-primary/90 transition-colors"
            >
              Done
            </button>
          </div>
        </>
      )}
    </Modal>
  )
}
