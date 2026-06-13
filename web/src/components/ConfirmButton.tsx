import { useState, useRef, useEffect } from 'react'
import { Check, Trash2 } from 'lucide-react'

// Inline two-step confirm for destructive actions — no modal, in keeping with
// the app's inline-editing direction. First click *arms* the button (turns
// destructive, swaps to a check / "Confirm?"); a second click within 3s, or
// while still focused, confirms. Blur or the timeout disarms it.
//
//   icon form:  <ConfirmButton onConfirm={...} title="Delete key" />
//   text form:  <ConfirmButton label="Delete role" onConfirm={...} />
export function ConfirmButton({ onConfirm, label, title, disabled, size = 13 }: {
  onConfirm: () => void
  label?: string
  title?: string
  disabled?: boolean
  size?: number
}) {
  const [armed, setArmed] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(() => () => clearTimeout(timer.current), [])

  function handle(e: React.MouseEvent) {
    e.preventDefault()
    e.stopPropagation()
    if (disabled) return
    if (!armed) {
      setArmed(true)
      timer.current = setTimeout(() => setArmed(false), 3000)
    } else {
      clearTimeout(timer.current)
      setArmed(false)
      onConfirm()
    }
  }
  const disarm = () => { clearTimeout(timer.current); setArmed(false) }

  if (label) {
    return (
      <button
        type="button"
        disabled={disabled}
        onClick={handle}
        onBlur={disarm}
        title={armed ? 'Click again to confirm' : title}
        className={`rounded-md px-2.5 py-1 text-xs font-medium border transition-colors disabled:opacity-40 ${
          armed
            ? 'border-destructive bg-destructive text-white'
            : 'border-border text-muted-foreground hover:text-destructive hover:border-destructive/40'
        }`}
      >
        {armed ? 'Confirm?' : label}
      </button>
    )
  }

  return (
    <button
      type="button"
      disabled={disabled}
      onClick={handle}
      onBlur={disarm}
      title={armed ? 'Click again to confirm' : (title ?? 'Delete')}
      aria-label={title ?? 'Delete'}
      className={`w-7 h-7 flex items-center justify-center rounded shrink-0 transition-colors disabled:opacity-40 ${
        armed ? 'bg-destructive text-white' : 'text-muted-foreground/50 hover:text-destructive hover:bg-destructive/5'
      }`}
    >
      {armed ? <Check size={size} /> : <Trash2 size={size} />}
    </button>
  )
}
