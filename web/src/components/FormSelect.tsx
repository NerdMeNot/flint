import { ChevronDown } from 'lucide-react'
import { useState, useRef } from 'react'
import { Popover } from '#/components/Popover'

interface FormSelectProps {
  value: string
  onChange: (v: string) => void
  options: Array<{ key: string; label: string }>
  placeholder?: string
}

export function FormSelect({ value, onChange, options, placeholder }: FormSelectProps) {
  const [open, setOpen] = useState(false)
  const anchorRef = useRef<HTMLButtonElement>(null)

  const selected = options.find((o) => o.key === value)

  return (
    <>
      <button
        ref={anchorRef}
        type="button"
        onClick={() => setOpen(!open)}
        className="w-full flex items-center justify-between rounded-lg border border-border px-3 py-2 text-sm transition-colors hover:border-ring/40 focus:outline-none focus:ring-2 focus:ring-ring/40"
      >
        <span className={selected ? 'text-foreground' : 'text-muted-foreground/50'}>
          {selected?.label ?? placeholder ?? 'Select...'}
        </span>
        <ChevronDown size={14} className={`text-muted-foreground transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>

      <Popover anchorRef={anchorRef} open={open} onClose={() => setOpen(false)}>
        {placeholder && (
          <button
            type="button"
            onClick={() => { onChange(''); setOpen(false) }}
            className={`w-full text-left px-3 py-2 text-sm transition-colors ${
              !value ? 'text-primary bg-primary/5' : 'text-muted-foreground hover:text-foreground hover:bg-accent'
            }`}
          >
            {placeholder}
          </button>
        )}
        {options.map((opt) => (
          <button
            key={opt.key}
            type="button"
            onClick={() => { onChange(opt.key); setOpen(false) }}
            className={`w-full text-left px-3 py-2 text-sm transition-colors ${
              value === opt.key ? 'text-primary bg-primary/5' : 'text-muted-foreground hover:text-foreground hover:bg-accent'
            }`}
          >
            {opt.label}
          </button>
        ))}
      </Popover>
    </>
  )
}
