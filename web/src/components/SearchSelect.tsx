import { useState, useRef, useEffect } from 'react'
import { Search, X } from 'lucide-react'

export interface SearchSelectItem {
  id: string
  label: string
  detail?: string
  icon?: React.ReactNode
}

interface SearchSelectProps {
  items: SearchSelectItem[]
  selected: Set<string>
  onChange: (selected: Set<string>) => void
  placeholder?: string
}

export function SearchSelect({ items, selected, onChange, placeholder }: SearchSelectProps) {
  const [query, setQuery] = useState('')
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [])

  const filtered = query
    ? items.filter((item) =>
        !selected.has(item.id) &&
        (item.label.toLowerCase().includes(query.toLowerCase()) ||
         item.id.toLowerCase().includes(query.toLowerCase()) ||
         (item.detail?.toLowerCase().includes(query.toLowerCase()) ?? false))
      )
    : []

  function add(id: string) {
    const next = new Set(selected)
    next.add(id)
    onChange(next)
    setQuery('')
    setOpen(false)
  }

  function remove(id: string) {
    const next = new Set(selected)
    next.delete(id)
    onChange(next)
  }

  const selectedItems = items.filter((item) => selected.has(item.id))

  return (
    <div ref={ref} className="space-y-2">
      {/* Selected tags */}
      {selectedItems.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {selectedItems.map((item) => (
            <span
              key={item.id}
              className="inline-flex items-center gap-1.5 rounded-md bg-accent border border-primary px-2 py-1 text-xs font-medium text-primary"
            >
              {item.icon}
              {item.label}
              <button
                type="button"
                onClick={() => remove(item.id)}
                className="text-primary/60 hover:text-primary transition-colors"
              >
                <X size={10} />
              </button>
            </span>
          ))}
        </div>
      )}

      {/* Search input */}
      <div className="relative">
        <div className="flex items-center gap-2 rounded-lg border border-border px-3 py-2 focus-within:ring-2 focus-within:ring-ring/40">
          <Search size={13} className="text-muted-foreground shrink-0" />
          <input
            type="text"
            value={query}
            onChange={(e) => { setQuery(e.target.value); setOpen(true) }}
            onFocus={() => { if (query) setOpen(true) }}
            placeholder={placeholder ?? 'Search...'}
            className="flex-1 bg-transparent text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none"
          />
        </div>

        {/* Dropdown results */}
        {open && filtered.length > 0 && (
          <div
            className="absolute top-full left-0 right-0 mt-1 rounded-lg border border-border overlay-edge overflow-hidden z-50 max-h-[200px] overflow-y-auto"
            style={{ background: 'var(--surface-strong)' }}
          >
            {filtered.slice(0, 10).map((item) => (
              <button
                key={item.id}
                type="button"
                onClick={() => add(item.id)}
                className="w-full flex items-center gap-2.5 px-3 py-2 text-sm text-left transition-colors text-muted-foreground hover:text-foreground hover:bg-accent"
              >
                {item.icon}
                <div className="min-w-0 flex-1">
                  <span className="font-medium text-foreground truncate block" title={item.label}>{item.label}</span>
                  {item.detail && item.detail !== item.label && (
                    <span className="ml-2 text-xs text-muted-foreground">{item.detail}</span>
                  )}
                </div>
              </button>
            ))}
            {filtered.length > 10 && (
              <p className="px-3 py-2 text-[12px] text-muted-foreground text-center">
                {filtered.length - 10} more — refine your search
              </p>
            )}
          </div>
        )}

        {open && query && filtered.length === 0 && (
          <div
            className="absolute top-full left-0 right-0 mt-1 rounded-lg border border-border overlay-edge overflow-hidden z-50"
            style={{ background: 'var(--surface-strong)' }}
          >
            <p className="px-3 py-3 text-xs text-muted-foreground text-center">No results</p>
          </div>
        )}
      </div>
    </div>
  )
}
