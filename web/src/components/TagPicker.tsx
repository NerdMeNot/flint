import { createPortal } from 'react-dom'
import { useState, useRef, useEffect, useLayoutEffect } from 'react'
import { Plus, Check, Search } from 'lucide-react'

export interface TagOption {
  tag: string // "key:value"
  group: string // key label, for the section header
  value: string
  color: string
}

// A portaled, keyboard-navigable, grouped multi-select tag picker. Portaling to
// <body> keeps the popover above page content (no z-index/overflow fights);
// grouping + search + arrow-key nav scale to many tags; toggling stays open so
// several tags can be applied at once.
export function TagPicker({ options, applied, onToggle, emptyHint }: {
  options: TagOption[]
  applied: Set<string>
  onToggle: (tag: string) => void
  emptyHint?: string
}) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const btnRef = useRef<HTMLButtonElement>(null)
  const popRef = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null)

  useLayoutEffect(() => {
    if (open && btnRef.current) {
      const r = btnRef.current.getBoundingClientRect()
      setPos({ top: r.bottom + 6, left: r.left })
    }
  }, [open])

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (popRef.current?.contains(e.target as Node) || btnRef.current?.contains(e.target as Node)) return
      setOpen(false)
    }
    const onScroll = () => setOpen(false)
    document.addEventListener('mousedown', onDown)
    window.addEventListener('scroll', onScroll, true)
    return () => {
      document.removeEventListener('mousedown', onDown)
      window.removeEventListener('scroll', onScroll, true)
    }
  }, [open])

  const q = query.toLowerCase().trim()
  const filtered = options.filter(
    (o) => !q || o.value.toLowerCase().includes(q) || o.group.toLowerCase().includes(q) || o.tag.toLowerCase().includes(q),
  )
  const clampedActive = Math.min(active, Math.max(0, filtered.length - 1))

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Escape') { setOpen(false); return }
    if (e.key === 'ArrowDown') { e.preventDefault(); setActive((a) => Math.min(a + 1, filtered.length - 1)) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setActive((a) => Math.max(a - 1, 0)) }
    else if (e.key === 'Enter') { e.preventDefault(); const o = filtered[clampedActive]; if (o) onToggle(o.tag) }
  }

  // Render rows with a group header whenever the key changes.
  let lastGroup = ''

  return (
    <>
      <button
        ref={btnRef}
        type="button"
        onClick={() => { setOpen((v) => !v); setQuery(''); setActive(0) }}
        className={`inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-[12px] font-medium transition-colors ${
          open ? 'border-primary/40 text-primary' : 'border-dashed border-border text-muted-foreground hover:text-foreground'
        }`}
      >
        <Plus size={11} /> tag
      </button>

      {open && pos && createPortal(
        <div
          ref={popRef}
          style={{ position: 'fixed', top: pos.top, left: pos.left, background: 'var(--surface-strong)' }}
          className="w-72 rounded-lg border border-border shadow-xl overflow-hidden z-[200]"
        >
          <div className="flex items-center gap-2 px-2.5 py-2 border-b border-border">
            <Search size={13} className="text-muted-foreground shrink-0" />
            <input
              type="text" autoFocus value={query}
              onChange={(e) => { setQuery(e.target.value); setActive(0) }}
              onKeyDown={onKeyDown}
              placeholder="Search tags…"
              className="flex-1 bg-transparent text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none"
            />
          </div>
          <div className="max-h-64 overflow-y-auto py-1">
            {options.length === 0 ? (
              <p className="px-3 py-3 text-xs text-muted-foreground text-center">{emptyHint ?? 'No tags declared.'}</p>
            ) : filtered.length === 0 ? (
              <p className="px-3 py-3 text-xs text-muted-foreground text-center">No matching tags</p>
            ) : (
              filtered.map((o, i) => {
                const header = o.group !== lastGroup ? o.group : null
                lastGroup = o.group
                const on = applied.has(o.tag)
                return (
                  <div key={o.tag}>
                    {header && (
                      <p className="px-3 pt-2 pb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60">{header}</p>
                    )}
                    <button
                      type="button"
                      onMouseEnter={() => setActive(i)}
                      onClick={() => onToggle(o.tag)}
                      className={`w-full flex items-center gap-2 px-3 py-1.5 text-sm text-left transition-colors ${
                        i === clampedActive ? 'bg-accent text-foreground' : 'text-muted-foreground'
                      }`}
                    >
                      <span className="w-2 h-2 rounded-full shrink-0" style={{ background: o.color }} />
                      <span className="flex-1 truncate">{o.value}</span>
                      {on && <Check size={13} className="text-primary shrink-0" />}
                    </button>
                  </div>
                )
              })
            )}
          </div>
        </div>,
        document.body,
      )}
    </>
  )
}
