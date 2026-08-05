import { ChevronDown, X } from 'lucide-react'
import { useState, useRef, useEffect } from 'react'

export interface FilterPillItem<K extends string = string> {
  key: K
  label: string
  detail?: string
  active: boolean
}

interface FilterPillProps<K extends string> {
  icon: React.ReactNode
  label: string
  active: boolean
  onClear: () => void
  items: FilterPillItem<K>[]
  onSelect: (key: K) => void
}

export function FilterPill<K extends string = string>({ icon, label, active, onClear, items, onSelect }: FilterPillProps<K>) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [])

  return (
    <div ref={ref} className="relative">
      <div className={`flex items-center rounded-lg border text-xs font-medium whitespace-nowrap transition-colors ${
        active
          ? 'border-primary bg-accent text-primary'
          : 'border-border text-muted-foreground'
      }`}>
        <button
          type="button"
          onClick={() => setOpen(!open)}
          className="flex items-center gap-1.5 px-2.5 py-1.5 hover:text-foreground transition-colors"
        >
          {icon}
          <span className="hidden sm:inline">{label}</span>
          <ChevronDown size={10} className={`hidden sm:block opacity-50 transition-transform ${open ? 'rotate-180' : ''}`} />
        </button>
        {active && (
          <button
            type="button"
            onClick={onClear}
            className="flex items-center px-1.5 py-1.5 border-l border-primary text-primary/60 hover:text-primary transition-colors"
            title={`Clear ${label}`}
          >
            <X size={11} />
          </button>
        )}
      </div>

      {open && (
        <div
          className="absolute top-full left-0 mt-1 min-w-[160px] w-max max-w-[calc(100vw-1rem)] rounded-lg border border-border overlay-edge overflow-hidden z-50"
          style={{ background: 'var(--surface-strong)' }}
        >
          {items.map((item) => (
            <button
              key={item.key}
              type="button"
              onClick={() => { onSelect(item.key); setOpen(false) }}
              className={`w-full flex items-center justify-between gap-4 px-3 py-2 text-xs font-medium whitespace-nowrap transition-colors ${
                item.active ? 'text-primary bg-accent' : 'text-muted-foreground hover:text-foreground hover:bg-accent'
              }`}
            >
              <span>{item.label}</span>
              {item.detail && <span className="text-[11px] opacity-50">{item.detail}</span>}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
