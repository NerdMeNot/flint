import { useRef, useState } from 'react'
import { ChevronDown, X, Check, Plus, Pencil } from 'lucide-react'
import { Popover } from '#/components/Popover'

export type ComboOption = { value: string; label?: string; hint?: string }

const baseField =
  'w-full rounded-lg border border-border bg-transparent px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground/50 focus-within:ring-2 focus-within:ring-ring/40'

function MenuItem({ active, onClick, label, hint, icon }: { active?: boolean; onClick: () => void; label: string; hint?: string; icon?: React.ReactNode }) {
  return (
    <button
      type="button"
      onMouseDown={(e) => { e.preventDefault(); onClick() }}
      className={`w-full text-left px-3 py-1.5 text-sm transition-colors flex items-center justify-between gap-2 ${
        active ? 'text-primary bg-primary/5' : 'text-muted-foreground hover:text-foreground hover:bg-accent'
      }`}
    >
      <span className="flex items-center gap-1.5 min-w-0">{icon}<span className="font-mono truncate">{label}</span></span>
      {hint && <span className="text-[10px] text-muted-foreground/70 shrink-0">{hint}</span>}
    </button>
  )
}

// A muted "you can type your own value" footer, shown in every combo dropdown so
// it's obvious the presets are only suggestions.
function CustomHint() {
  return (
    <div className="flex items-center gap-1.5 border-t border-border/60 px-3 py-1.5 text-[10px] text-muted-foreground/70">
      <Pencil size={10} /> Presets are suggestions — type to use any value
    </div>
  )
}

// ComboBox — a single free-text value with a dropdown of presets. The typed
// value is the value; presets are suggestions. When the text doesn't match a
// preset, a "Use …" row confirms the custom value will be kept.
export function ComboBox({ value, onChange, options, placeholder, mono = true }: {
  value: string
  onChange: (v: string) => void
  options: ComboOption[]
  placeholder?: string
  mono?: boolean
}) {
  const [open, setOpen] = useState(false)
  const anchorRef = useRef<HTMLDivElement>(null)

  const trimmed = value.trim()
  const q = trimmed.toLowerCase()
  const filtered = q ? options.filter((o) => o.value.toLowerCase().includes(q) || o.label?.toLowerCase().includes(q)) : options
  const exact = options.some((o) => o.value === trimmed)

  return (
    <>
      <div ref={anchorRef} className={`${baseField} flex items-center gap-1`}>
        <input
          value={value}
          onChange={(e) => { onChange(e.target.value); setOpen(true) }}
          onFocus={() => setOpen(true)}
          placeholder={placeholder ?? 'Select or type…'}
          className={`flex-1 bg-transparent outline-none ${mono ? 'font-mono' : ''}`}
        />
        <button type="button" tabIndex={-1} onClick={() => setOpen((o) => !o)} className="shrink-0 text-muted-foreground">
          <ChevronDown size={14} className={`transition-transform ${open ? 'rotate-180' : ''}`} />
        </button>
      </div>
      <Popover anchorRef={anchorRef} open={open} onClose={() => setOpen(false)}>
        {trimmed && !exact && (
          <MenuItem icon={<Plus size={12} className="text-primary" />} onClick={() => { onChange(trimmed); setOpen(false) }} label={`Use “${trimmed}”`} hint="custom" />
        )}
        {filtered.map((o) => (
          <MenuItem
            key={o.value} active={o.value === trimmed}
            onClick={() => { onChange(o.value); setOpen(false) }}
            label={o.label ?? o.value} hint={o.hint}
          />
        ))}
        <CustomHint />
      </Popover>
    </>
  )
}

// PillSelect — a multi-value picker shown as a grid of toggleable preset pills
// plus an explicit "+ custom" entry. More discoverable than a token input: every
// option is visible and clickable, and adding your own value is an obvious step.
export function PillSelect({ values, onChange, options, addPlaceholder = 'custom' }: {
  values: string[]
  onChange: (v: string[]) => void
  options: ComboOption[]
  addPlaceholder?: string
}) {
  const [adding, setAdding] = useState(false)
  const [draft, setDraft] = useState('')
  const sel = new Set(values)
  const customs = values.filter((v) => !options.some((o) => o.value === v))

  const toggle = (v: string) => onChange(sel.has(v) ? values.filter((x) => x !== v) : [...values, v])
  const addCustom = () => {
    const t = draft.trim()
    if (t && !sel.has(t)) onChange([...values, t])
    setDraft(''); setAdding(false)
  }

  const pill = (on: boolean) =>
    `inline-flex items-center gap-1 rounded-md border px-2 py-1 text-[11px] font-mono transition-colors ${
      on ? 'border-primary/40 bg-primary/10 text-primary' : 'border-border text-muted-foreground hover:border-muted-foreground/40 hover:text-foreground'
    }`

  return (
    <div className="flex flex-wrap gap-1.5">
      {options.map((o) => {
        const on = sel.has(o.value)
        return (
          <button key={o.value} type="button" onClick={() => toggle(o.value)} className={pill(on)}>
            {on && <Check size={10} />}{o.value}
            {o.hint && <span className="opacity-50">{o.hint}</span>}
          </button>
        )
      })}
      {customs.map((v) => (
        <span key={v} className={pill(true)}>
          <Check size={10} />{v}
          <button type="button" onClick={() => onChange(values.filter((x) => x !== v))} className="text-primary/60 hover:text-primary ml-0.5">
            <X size={10} />
          </button>
        </span>
      ))}
      {adding ? (
        <input
          autoFocus value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') { e.preventDefault(); addCustom() }
            else if (e.key === 'Escape') { setDraft(''); setAdding(false) }
          }}
          onBlur={addCustom}
          placeholder={addPlaceholder}
          className="w-24 rounded-md border border-primary/40 bg-transparent px-2 py-1 text-[11px] font-mono outline-none"
        />
      ) : (
        <button
          type="button" onClick={() => setAdding(true)}
          className="inline-flex items-center gap-1 rounded-md border border-dashed border-border px-2 py-1 text-[11px] text-muted-foreground hover:text-foreground hover:border-muted-foreground/40 transition-colors"
        >
          <Plus size={10} /> custom
        </button>
      )}
    </div>
  )
}
