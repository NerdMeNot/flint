import { useState, useEffect } from 'react'
import { Palette } from 'lucide-react'

interface ColorTheme {
  name: string
  label: string
  dot: string // preview color
  vars: Record<string, string>
  darkVars: Record<string, string>
}

const themes: ColorTheme[] = [
  {
    name: 'ocean',
    label: 'Ocean',
    dot: '#22d3ee',
    vars: {
      '--background': '#e4f6fa',
      '--foreground': '#083344',
      '--card': 'rgba(255,255,255,0.93)',
      '--card-foreground': '#083344',
      '--primary': '#0891b2',
      '--primary-foreground': '#ffffff',
      '--muted': '#d2f0f6',
      '--muted-foreground': '#1a6578',
      '--accent': '#d2f0f6',
      '--accent-foreground': '#083344',
      '--border': 'rgba(8,51,68,0.12)',
      '--ring': '#22d3ee',
      '--surface': 'rgba(255,255,255,0.78)',
      '--surface-strong': 'rgba(255,255,255,0.92)',
      '--inset-glint': 'rgba(255,255,255,0.85)',
      '--kicker': 'rgba(8,145,178,0.8)',
      '--hero-a': 'rgba(34,211,238,0.34)',
      '--hero-b': 'rgba(6,182,212,0.22)',
      '--hero-c': 'rgba(34,211,238,0.10)',
      '--link-bg-hover': 'rgba(255,255,255,0.92)',
      '--success': '#059669',
      '--warning': '#d97706',
      '--destructive': '#dc2626',
      '--sidebar': 'rgba(240,253,255,0.86)',
    },
    darkVars: {
      '--background': '#061a22',
      '--foreground': '#cef2f8',
      '--card': 'rgba(8,30,40,0.88)',
      '--card-foreground': '#cef2f8',
      '--primary': '#22d3ee',
      '--primary-foreground': '#061a22',
      '--muted': '#0a2430',
      '--muted-foreground': '#7dd8e8',
      '--accent': 'rgba(34,211,238,0.12)',
      '--accent-foreground': '#cef2f8',
      '--border': 'rgba(34,211,238,0.18)',
      '--ring': '#22d3ee',
      '--surface': 'rgba(8,30,40,0.82)',
      '--surface-strong': 'rgba(6,26,34,0.94)',
      '--inset-glint': 'rgba(160,240,255,0.07)',
      '--kicker': '#67e8f9',
      '--hero-a': 'rgba(34,211,238,0.18)',
      '--hero-b': 'rgba(6,182,212,0.10)',
      '--hero-c': 'rgba(34,211,238,0.05)',
      '--link-bg-hover': 'rgba(12,42,56,0.8)',
      '--success': '#34d399',
      '--warning': '#fbbf24',
      '--destructive': '#f87171',
      '--sidebar': 'rgba(6,26,34,0.92)',
    },
  },
  {
    name: 'indigo',
    label: 'Indigo',
    dot: '#6366f1',
    vars: {
      '--background': '#eef0f8',
      '--foreground': '#1e1b3a',
      '--card': 'rgba(255,255,255,0.92)',
      '--card-foreground': '#1e1b3a',
      '--primary': '#4f46e5',
      '--primary-foreground': '#ffffff',
      '--muted': '#e0e2ee',
      '--muted-foreground': '#5b5880',
      '--accent': '#e0e2ee',
      '--accent-foreground': '#1e1b3a',
      '--border': 'rgba(30,27,58,0.12)',
      '--ring': '#6366f1',
      '--surface': 'rgba(255,255,255,0.76)',
      '--surface-strong': 'rgba(255,255,255,0.92)',
      '--inset-glint': 'rgba(255,255,255,0.80)',
      '--kicker': 'rgba(79,70,229,0.75)',
      '--hero-a': 'rgba(99,102,241,0.28)',
      '--hero-b': 'rgba(139,92,246,0.18)',
      '--hero-c': 'rgba(99,102,241,0.08)',
      '--link-bg-hover': 'rgba(255,255,255,0.9)',
      '--success': '#16a34a',
      '--warning': '#ca8a04',
      '--destructive': '#dc2626',
      '--sidebar': 'rgba(248,248,255,0.84)',
    },
    darkVars: {
      '--background': '#0c0a1a',
      '--foreground': '#e2e0f0',
      '--card': 'rgba(18,16,36,0.88)',
      '--card-foreground': '#e2e0f0',
      '--primary': '#818cf8',
      '--primary-foreground': '#0c0a1a',
      '--muted': '#1a1830',
      '--muted-foreground': '#a5a2c8',
      '--accent': 'rgba(129,140,248,0.10)',
      '--accent-foreground': '#e2e0f0',
      '--border': 'rgba(129,140,248,0.16)',
      '--ring': '#818cf8',
      '--surface': 'rgba(18,16,36,0.82)',
      '--surface-strong': 'rgba(16,14,32,0.94)',
      '--inset-glint': 'rgba(200,198,240,0.06)',
      '--kicker': '#c4b5fd',
      '--hero-a': 'rgba(129,140,248,0.16)',
      '--hero-b': 'rgba(167,139,250,0.10)',
      '--hero-c': 'rgba(129,140,248,0.05)',
      '--link-bg-hover': 'rgba(30,26,58,0.8)',
      '--success': '#4ade80',
      '--warning': '#fbbf24',
      '--destructive': '#f87171',
      '--sidebar': 'rgba(12,10,26,0.92)',
    },
  },
  {
    name: 'rose',
    label: 'Rose',
    dot: '#f43f5e',
    vars: {
      '--background': '#faf0f2',
      '--foreground': '#3b1424',
      '--card': 'rgba(255,255,255,0.92)',
      '--card-foreground': '#3b1424',
      '--primary': '#e11d48',
      '--primary-foreground': '#ffffff',
      '--muted': '#f0dde2',
      '--muted-foreground': '#7a4058',
      '--accent': '#f0dde2',
      '--accent-foreground': '#3b1424',
      '--border': 'rgba(59,20,36,0.12)',
      '--ring': '#f43f5e',
      '--surface': 'rgba(255,255,255,0.76)',
      '--surface-strong': 'rgba(255,255,255,0.92)',
      '--inset-glint': 'rgba(255,255,255,0.82)',
      '--kicker': 'rgba(225,29,72,0.7)',
      '--hero-a': 'rgba(244,63,94,0.24)',
      '--hero-b': 'rgba(236,72,153,0.14)',
      '--hero-c': 'rgba(244,63,94,0.06)',
      '--link-bg-hover': 'rgba(255,255,255,0.9)',
      '--success': '#16a34a',
      '--warning': '#ca8a04',
      '--destructive': '#dc2626',
      '--sidebar': 'rgba(255,250,252,0.84)',
    },
    darkVars: {
      '--background': '#1a0a10',
      '--foreground': '#f0d4dc',
      '--card': 'rgba(30,12,20,0.88)',
      '--card-foreground': '#f0d4dc',
      '--primary': '#fb7185',
      '--primary-foreground': '#1a0a10',
      '--muted': '#2a1420',
      '--muted-foreground': '#c8889a',
      '--accent': 'rgba(251,113,133,0.10)',
      '--accent-foreground': '#f0d4dc',
      '--border': 'rgba(251,113,133,0.18)',
      '--ring': '#fb7185',
      '--surface': 'rgba(30,12,20,0.82)',
      '--surface-strong': 'rgba(26,10,16,0.94)',
      '--inset-glint': 'rgba(240,180,200,0.06)',
      '--kicker': '#fda4af',
      '--hero-a': 'rgba(251,113,133,0.14)',
      '--hero-b': 'rgba(236,72,153,0.08)',
      '--hero-c': 'rgba(251,113,133,0.04)',
      '--link-bg-hover': 'rgba(42,20,32,0.8)',
      '--success': '#4ade80',
      '--warning': '#fbbf24',
      '--destructive': '#f87171',
      '--sidebar': 'rgba(26,10,16,0.92)',
    },
  },
  {
    name: 'emerald',
    label: 'Emerald',
    dot: '#10b981',
    vars: {
      '--background': '#e8f5ee',
      '--foreground': '#0a2e1c',
      '--card': 'rgba(255,255,255,0.92)',
      '--card-foreground': '#0a2e1c',
      '--primary': '#059669',
      '--primary-foreground': '#ffffff',
      '--muted': '#d6ebe0',
      '--muted-foreground': '#3d7558',
      '--accent': '#d6ebe0',
      '--accent-foreground': '#0a2e1c',
      '--border': 'rgba(10,46,28,0.12)',
      '--ring': '#10b981',
      '--surface': 'rgba(255,255,255,0.76)',
      '--surface-strong': 'rgba(255,255,255,0.91)',
      '--inset-glint': 'rgba(255,255,255,0.82)',
      '--kicker': 'rgba(5,150,105,0.75)',
      '--hero-a': 'rgba(16,185,129,0.28)',
      '--hero-b': 'rgba(52,211,153,0.16)',
      '--hero-c': 'rgba(16,185,129,0.06)',
      '--link-bg-hover': 'rgba(255,255,255,0.9)',
      '--success': '#047857',
      '--warning': '#ca8a04',
      '--destructive': '#dc2626',
      '--sidebar': 'rgba(248,255,250,0.84)',
    },
    darkVars: {
      '--background': '#061210',
      '--foreground': '#d0ece0',
      '--card': 'rgba(8,22,18,0.88)',
      '--card-foreground': '#d0ece0',
      '--primary': '#34d399',
      '--primary-foreground': '#061210',
      '--muted': '#0c1e18',
      '--muted-foreground': '#86c4a8',
      '--accent': 'rgba(52,211,153,0.10)',
      '--accent-foreground': '#d0ece0',
      '--border': 'rgba(52,211,153,0.16)',
      '--ring': '#34d399',
      '--surface': 'rgba(8,22,18,0.82)',
      '--surface-strong': 'rgba(6,18,16,0.94)',
      '--inset-glint': 'rgba(180,240,210,0.06)',
      '--kicker': '#6ee7b7',
      '--hero-a': 'rgba(52,211,153,0.14)',
      '--hero-b': 'rgba(16,185,129,0.08)',
      '--hero-c': 'rgba(52,211,153,0.04)',
      '--link-bg-hover': 'rgba(14,34,28,0.8)',
      '--success': '#34d399',
      '--warning': '#fbbf24',
      '--destructive': '#f87171',
      '--sidebar': 'rgba(6,18,16,0.92)',
    },
  },
  {
    name: 'yellow',
    label: 'Yellow',
    dot: '#fde047',
    vars: {
      '--background': '#fefce8',
      '--foreground': '#422006',
      '--card': 'rgba(255,255,255,0.94)',
      '--card-foreground': '#422006',
      '--primary': '#eab308',
      '--primary-foreground': '#422006',
      '--muted': '#fef9c3',
      '--muted-foreground': '#713f12',
      '--accent': '#fef9c3',
      '--accent-foreground': '#422006',
      '--border': 'rgba(234,179,8,0.20)',
      '--ring': '#fde047',
      '--surface': 'rgba(255,255,255,0.78)',
      '--surface-strong': 'rgba(255,255,255,0.94)',
      '--inset-glint': 'rgba(255,255,255,0.86)',
      '--kicker': 'rgba(161,98,7,0.85)',
      '--hero-a': 'rgba(253,224,71,0.40)',
      '--hero-b': 'rgba(250,204,21,0.28)',
      '--hero-c': 'rgba(234,179,8,0.12)',
      '--link-bg-hover': 'rgba(255,255,255,0.94)',
      '--success': '#16a34a',
      '--warning': '#a16207',
      '--destructive': '#dc2626',
      '--sidebar': 'rgba(254,252,232,0.88)',
    },
    darkVars: {
      '--background': '#18130a',
      '--foreground': '#fefce8',
      '--card': 'rgba(30,24,12,0.90)',
      '--card-foreground': '#fefce8',
      '--primary': '#fde047',
      '--primary-foreground': '#422006',
      '--muted': '#28200e',
      '--muted-foreground': '#d4a620',
      '--accent': 'rgba(253,224,71,0.14)',
      '--accent-foreground': '#fefce8',
      '--border': 'rgba(253,224,71,0.18)',
      '--ring': '#fde047',
      '--surface': 'rgba(30,24,12,0.84)',
      '--surface-strong': 'rgba(24,19,10,0.95)',
      '--inset-glint': 'rgba(253,224,71,0.06)',
      '--kicker': '#fef08a',
      '--hero-a': 'rgba(253,224,71,0.20)',
      '--hero-b': 'rgba(250,204,21,0.12)',
      '--hero-c': 'rgba(253,224,71,0.05)',
      '--link-bg-hover': 'rgba(40,32,16,0.8)',
      '--success': '#4ade80',
      '--warning': '#fde047',
      '--destructive': '#f87171',
      '--sidebar': 'rgba(24,19,10,0.94)',
    },
  },
  {
    name: 'slate',
    label: 'Slate',
    dot: '#94a3b8',
    vars: {
      '--background': '#f1f5f9',
      '--foreground': '#0f172a',
      '--card': 'rgba(255,255,255,0.93)',
      '--card-foreground': '#0f172a',
      '--primary': '#475569',
      '--primary-foreground': '#ffffff',
      '--muted': '#e2e8f0',
      '--muted-foreground': '#64748b',
      '--accent': '#e2e8f0',
      '--accent-foreground': '#0f172a',
      '--border': 'rgba(15,23,42,0.10)',
      '--ring': '#94a3b8',
      '--surface': 'rgba(255,255,255,0.78)',
      '--surface-strong': 'rgba(255,255,255,0.93)',
      '--inset-glint': 'rgba(255,255,255,0.85)',
      '--kicker': 'rgba(71,85,105,0.7)',
      '--hero-a': 'rgba(148,163,184,0.22)',
      '--hero-b': 'rgba(100,116,139,0.14)',
      '--hero-c': 'rgba(148,163,184,0.06)',
      '--link-bg-hover': 'rgba(255,255,255,0.92)',
      '--success': '#16a34a',
      '--warning': '#ca8a04',
      '--destructive': '#dc2626',
      '--sidebar': 'rgba(248,250,252,0.86)',
    },
    darkVars: {
      '--background': '#0f1218',
      '--foreground': '#e2e8f0',
      '--card': 'rgba(18,22,30,0.90)',
      '--card-foreground': '#e2e8f0',
      '--primary': '#94a3b8',
      '--primary-foreground': '#0f1218',
      '--muted': '#1e2330',
      '--muted-foreground': '#94a3b8',
      '--accent': 'rgba(148,163,184,0.10)',
      '--accent-foreground': '#e2e8f0',
      '--border': 'rgba(148,163,184,0.14)',
      '--ring': '#94a3b8',
      '--surface': 'rgba(18,22,30,0.82)',
      '--surface-strong': 'rgba(15,18,24,0.94)',
      '--inset-glint': 'rgba(200,210,225,0.05)',
      '--kicker': '#cbd5e1',
      '--hero-a': 'rgba(148,163,184,0.12)',
      '--hero-b': 'rgba(100,116,139,0.06)',
      '--hero-c': 'rgba(148,163,184,0.03)',
      '--link-bg-hover': 'rgba(30,35,48,0.8)',
      '--success': '#4ade80',
      '--warning': '#fbbf24',
      '--destructive': '#f87171',
      '--sidebar': 'rgba(15,18,24,0.92)',
    },
  },
]

function getActiveThemeName(): string {
  if (typeof window === 'undefined') return 'ocean'
  return window.localStorage.getItem('flint-color-theme') ?? 'ocean'
}

function isDark(): boolean {
  if (typeof window === 'undefined') return false
  return document.documentElement.classList.contains('dark')
}

function applyColorTheme(theme: ColorTheme) {
  const vars = isDark() ? theme.darkVars : theme.vars
  const root = document.documentElement
  for (const [key, value] of Object.entries(vars)) {
    root.style.setProperty(key, value)
  }
  // Re-apply body background since it uses CSS vars
  document.body.style.background = ''
}

export function ThemeSwitcher() {
  const [active, setActive] = useState('teal')
  const [open, setOpen] = useState(false)

  useEffect(() => {
    const name = getActiveThemeName()
    setActive(name)
    const theme = themes.find((t) => t.name === name)
    if (theme) applyColorTheme(theme)

    // Re-apply when light/dark mode changes
    const observer = new MutationObserver(() => {
      const t = themes.find((t) => t.name === getActiveThemeName())
      if (t) applyColorTheme(t)
    })
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['class'],
    })
    return () => observer.disconnect()
  }, [])

  function selectTheme(theme: ColorTheme) {
    setActive(theme.name)
    window.localStorage.setItem('flint-color-theme', theme.name)
    applyColorTheme(theme)
    setOpen(false)
  }

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="flex items-center gap-1.5 rounded-lg border border-border px-2.5 py-1.5 text-xs font-medium whitespace-nowrap text-muted-foreground hover:text-foreground transition-colors"
        style={{ background: 'var(--surface)' }}
      >
        <Palette size={14} />
        <span
          className="w-3 h-3 rounded-full border border-border"
          style={{ background: themes.find((t) => t.name === active)?.dot }}
        />
      </button>

      {open && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setOpen(false)} />
          <div
            className="absolute right-0 top-full mt-2 z-50 rounded-xl border border-border p-2 shadow-lg min-w-[160px]"
            style={{ background: 'var(--surface-strong)', backdropFilter: 'blur(12px)' }}
          >
            {themes.map((theme) => (
              <button
                key={theme.name}
                type="button"
                onClick={() => selectTheme(theme)}
                className={`w-full flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm font-medium whitespace-nowrap transition-colors ${
                  active === theme.name
                    ? 'text-foreground'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
                style={active === theme.name ? { background: 'var(--link-bg-hover)' } : undefined}
              >
                <span
                  className="w-3.5 h-3.5 rounded-full border border-border shrink-0"
                  style={{ background: theme.dot }}
                />
                {theme.label}
                {active === theme.name && (
                  <span className="ml-auto text-xs opacity-50">active</span>
                )}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  )
}
