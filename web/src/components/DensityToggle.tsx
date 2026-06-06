import { useEffect, useState } from 'react'
import { Rows3, Rows2 } from 'lucide-react'

type Density = 'comfortable' | 'compact'

function getInitialDensity(): Density {
  if (typeof window === 'undefined') return 'comfortable'
  const stored = window.localStorage.getItem('density')
  return stored === 'compact' ? 'compact' : 'comfortable'
}

function applyDensity(d: Density) {
  if (d === 'compact') {
    document.documentElement.setAttribute('data-density', 'compact')
  } else {
    document.documentElement.removeAttribute('data-density')
  }
}

export function DensityToggle() {
  const [density, setDensity] = useState<Density>('comfortable')

  useEffect(() => {
    const d = getInitialDensity()
    setDensity(d)
    applyDensity(d)
  }, [])

  function setNext(next: Density) {
    setDensity(next)
    applyDensity(next)
    window.localStorage.setItem('density', next)
  }

  const options: { value: Density; icon: typeof Rows3; label: string }[] = [
    { value: 'comfortable', icon: Rows3, label: 'Comfortable' },
    { value: 'compact', icon: Rows2, label: 'Compact' },
  ]

  return (
    <div
      className="flex items-center rounded-lg border border-border p-0.5 gap-0.5"
      style={{ background: 'var(--surface)' }}
    >
      {options.map(({ value, icon: Icon, label }) => (
        <button
          key={value}
          type="button"
          onClick={() => setNext(value)}
          aria-label={`${label} density`}
          title={label}
          className={`flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium whitespace-nowrap transition-all duration-150 ${
            density === value
              ? 'text-white shadow-sm'
              : 'text-muted-foreground hover:text-foreground'
          }`}
          style={
            density === value
              ? { background: 'color-mix(in oklab, var(--ring), black 35%)' }
              : undefined
          }
        >
          <Icon size={13} strokeWidth={density === value ? 2.2 : 1.6} />
        </button>
      ))}
    </div>
  )
}
