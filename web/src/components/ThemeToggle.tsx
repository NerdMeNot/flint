import { useEffect, useState } from 'react'
import { Sun, Moon, Monitor } from 'lucide-react'

type ThemeMode = 'light' | 'dark' | 'auto'

function getInitialMode(): ThemeMode {
  if (typeof window === 'undefined') return 'auto'
  const stored = window.localStorage.getItem('theme')
  if (stored === 'light' || stored === 'dark' || stored === 'auto') return stored
  return 'auto'
}

function applyThemeMode(mode: ThemeMode) {
  const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
  const resolved = mode === 'auto' ? (prefersDark ? 'dark' : 'light') : mode
  document.documentElement.classList.remove('light', 'dark')
  document.documentElement.classList.add(resolved)
  if (mode === 'auto') {
    document.documentElement.removeAttribute('data-theme')
  } else {
    document.documentElement.setAttribute('data-theme', mode)
  }
  document.documentElement.style.colorScheme = resolved
}

export default function ThemeToggle() {
  const [mode, setMode] = useState<ThemeMode>('auto')

  useEffect(() => {
    const m = getInitialMode()
    setMode(m)
    applyThemeMode(m)
  }, [])

  useEffect(() => {
    if (mode !== 'auto') return
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = () => applyThemeMode('auto')
    media.addEventListener('change', onChange)
    return () => media.removeEventListener('change', onChange)
  }, [mode])

  function setTheme(next: ThemeMode) {
    setMode(next)
    applyThemeMode(next)
    window.localStorage.setItem('theme', next)
  }

  const options: { value: ThemeMode; icon: typeof Sun; label: string }[] = [
    { value: 'light', icon: Sun, label: 'Light' },
    { value: 'dark', icon: Moon, label: 'Dark' },
    { value: 'auto', icon: Monitor, label: 'System' },
  ]

  return (
    <div className="flex items-center rounded-lg border border-border p-0.5 gap-0.5"
      style={{ background: 'var(--surface)' }}>
      {options.map(({ value, icon: Icon, label }) => (
        <button
          key={value}
          type="button"
          onClick={() => setTheme(value)}
          aria-label={`Switch to ${label} theme`}
          title={label}
          className={`flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium transition-all duration-150 ${
            mode === value
              ? 'text-white shadow-sm'
              : 'text-muted-foreground hover:text-foreground'
          }`}
          style={mode === value ? { background: 'color-mix(in oklab, var(--ring), black 35%)' } : undefined}
        >
          <Icon size={13} strokeWidth={mode === value ? 2.2 : 1.6} />
          <span className="hidden sm:inline">{label}</span>
        </button>
      ))}
    </div>
  )
}
