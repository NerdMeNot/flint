// Appearance preferences — light/dark mode + color palette. Pure (non-React)
// so it can run from the pre-paint script, a root bootstrap, and the Profile UI
// alike. Source of truth is the user's server profile; localStorage mirrors it
// for instant, flash-free application on the next load.

export type ThemeMode = 'light' | 'dark' | 'auto'

export interface ColorTheme {
  name: string
  label: string
  dot: string // preview color
  vars: Record<string, string>
  darkVars: Record<string, string>
}

export const themes: ColorTheme[] = [
  {
    name: 'ember',
    label: 'Ember',
    dot: '#FF6A1A',
    vars: {
      '--background': '#F5F5F5',
      '--foreground': '#202020',
      '--card': '#FFFFFF',
      '--card-foreground': '#202020',
      '--muted': '#EDEDED',
      '--muted-foreground': '#5C5C5C',
      '--border': '#DDDDDD',
      '--sidebar': '#FFFFFF',
      '--surface': '#FFFFFF',
      '--surface-strong': '#FFFFFF',
      '--link-bg-hover': '#EDEDED',
      '--kicker': '#5C5C5C',
      '--success': '#1A7F4B',
      '--success-foreground': '#FFFFFF',
      '--warning': '#8A5200',
      '--warning-foreground': '#FFFFFF',
      '--destructive': '#C0281C',
      '--destructive-foreground': '#FFFFFF',
      '--primary': '#FF6A1A',
      '--primary-foreground': '#201004',
      '--accent': '#FFEDE2',
      '--accent-foreground': '#202020',
      '--ring': '#FF6A1A',
    },
    darkVars: {
      '--background': '#121417',
      '--foreground': '#ECEFF2',
      '--card': '#1A1D21',
      '--card-foreground': '#ECEFF2',
      '--muted': '#23272C',
      '--muted-foreground': '#99A2AB',
      '--border': '#2E333A',
      '--sidebar': '#15181B',
      '--surface': '#1A1D21',
      '--surface-strong': '#1A1D21',
      '--link-bg-hover': '#23272C',
      '--kicker': '#99A2AB',
      '--success': '#35B06E',
      '--success-foreground': '#141414',
      '--warning': '#F5CE4B',
      '--warning-foreground': '#141414',
      '--destructive': '#E4695C',
      '--destructive-foreground': '#141414',
      '--primary': '#FF7A2E',
      '--primary-foreground': '#1A0C02',
      '--accent': '#3A2008',
      '--accent-foreground': '#ECEFF2',
      '--ring': '#FF7A2E',
    },
  },
  {
    name: 'verdigris',
    label: 'Verdigris',
    dot: '#0E9B8E',
    vars: {
      '--background': '#F5F5F5',
      '--foreground': '#202020',
      '--card': '#FFFFFF',
      '--card-foreground': '#202020',
      '--muted': '#EDEDED',
      '--muted-foreground': '#5C5C5C',
      '--border': '#DDDDDD',
      '--sidebar': '#FFFFFF',
      '--surface': '#FFFFFF',
      '--surface-strong': '#FFFFFF',
      '--link-bg-hover': '#EDEDED',
      '--kicker': '#5C5C5C',
      '--success': '#1A7F4B',
      '--success-foreground': '#FFFFFF',
      '--warning': '#8A5200',
      '--warning-foreground': '#FFFFFF',
      '--destructive': '#C0281C',
      '--destructive-foreground': '#FFFFFF',
      '--primary': '#036F66',
      '--primary-foreground': '#FFFFFF',
      '--accent': '#DDF2EF',
      '--accent-foreground': '#202020',
      '--ring': '#036F66',
    },
    darkVars: {
      '--background': '#0F1615',
      '--foreground': '#E7EFED',
      '--card': '#19211F',
      '--card-foreground': '#E7EFED',
      '--muted': '#212B29',
      '--muted-foreground': '#93A5A1',
      '--border': '#2A3634',
      '--sidebar': '#131B1A',
      '--surface': '#19211F',
      '--surface-strong': '#19211F',
      '--link-bg-hover': '#212B29',
      '--kicker': '#93A5A1',
      '--success': '#35B06E',
      '--success-foreground': '#141414',
      '--warning': '#F5CE4B',
      '--warning-foreground': '#141414',
      '--destructive': '#E4695C',
      '--destructive-foreground': '#141414',
      '--primary': '#40DCC8',
      '--primary-foreground': '#062220',
      '--accent': '#0E2A27',
      '--accent-foreground': '#E7EFED',
      '--ring': '#40DCC8',
    },
  },
  {
    name: 'arc',
    label: 'Arc',
    dot: '#2E86FF',
    vars: {
      '--background': '#F5F5F5',
      '--foreground': '#202020',
      '--card': '#FFFFFF',
      '--card-foreground': '#202020',
      '--muted': '#EDEDED',
      '--muted-foreground': '#5C5C5C',
      '--border': '#DDDDDD',
      '--sidebar': '#FFFFFF',
      '--surface': '#FFFFFF',
      '--surface-strong': '#FFFFFF',
      '--link-bg-hover': '#EDEDED',
      '--kicker': '#5C5C5C',
      '--success': '#1A7F4B',
      '--success-foreground': '#FFFFFF',
      '--warning': '#8A5200',
      '--warning-foreground': '#FFFFFF',
      '--destructive': '#C0281C',
      '--destructive-foreground': '#FFFFFF',
      '--primary': '#1668E3',
      '--primary-foreground': '#FFFFFF',
      '--accent': '#E4EDFD',
      '--accent-foreground': '#202020',
      '--ring': '#1668E3',
    },
    darkVars: {
      '--background': '#0F1317',
      '--foreground': '#E9EEF2',
      '--card': '#191F24',
      '--card-foreground': '#E9EEF2',
      '--muted': '#212930',
      '--muted-foreground': '#94A3AD',
      '--border': '#2A333A',
      '--sidebar': '#131920',
      '--surface': '#191F24',
      '--surface-strong': '#191F24',
      '--link-bg-hover': '#212930',
      '--kicker': '#94A3AD',
      '--success': '#35B06E',
      '--success-foreground': '#141414',
      '--warning': '#F5CE4B',
      '--warning-foreground': '#141414',
      '--destructive': '#E4695C',
      '--destructive-foreground': '#141414',
      '--primary': '#4D9BFF',
      '--primary-foreground': '#06121F',
      '--accent': '#11253F',
      '--accent-foreground': '#E9EEF2',
      '--ring': '#4D9BFF',
    },
  },
  {
    name: 'plasma',
    label: 'Plasma',
    dot: '#7C5CFF',
    vars: {
      '--background': '#F5F5F5',
      '--foreground': '#202020',
      '--card': '#FFFFFF',
      '--card-foreground': '#202020',
      '--muted': '#EDEDED',
      '--muted-foreground': '#5C5C5C',
      '--border': '#DDDDDD',
      '--sidebar': '#FFFFFF',
      '--surface': '#FFFFFF',
      '--surface-strong': '#FFFFFF',
      '--link-bg-hover': '#EDEDED',
      '--kicker': '#5C5C5C',
      '--success': '#1A7F4B',
      '--success-foreground': '#FFFFFF',
      '--warning': '#8A5200',
      '--warning-foreground': '#FFFFFF',
      '--destructive': '#C0281C',
      '--destructive-foreground': '#FFFFFF',
      '--primary': '#6D3BE8',
      '--primary-foreground': '#FFFFFF',
      '--accent': '#ECE5FD',
      '--accent-foreground': '#202020',
      '--ring': '#6D3BE8',
    },
    darkVars: {
      '--background': '#141219',
      '--foreground': '#EDEAF4',
      '--card': '#1D1A26',
      '--card-foreground': '#EDEAF4',
      '--muted': '#26222F',
      '--muted-foreground': '#9F98B0',
      '--border': '#302B3D',
      '--sidebar': '#181521',
      '--surface': '#1D1A26',
      '--surface-strong': '#1D1A26',
      '--link-bg-hover': '#26222F',
      '--kicker': '#9F98B0',
      '--success': '#35B06E',
      '--success-foreground': '#141414',
      '--warning': '#F5CE4B',
      '--warning-foreground': '#141414',
      '--destructive': '#E4695C',
      '--destructive-foreground': '#141414',
      '--primary': '#A585FF',
      '--primary-foreground': '#150F26',
      '--accent': '#241C3D',
      '--accent-foreground': '#EDEAF4',
      '--ring': '#A585FF',
    },
  },
  {
    name: 'flare',
    label: 'Flare',
    dot: '#E5279E',
    vars: {
      '--background': '#F5F5F5',
      '--foreground': '#202020',
      '--card': '#FFFFFF',
      '--card-foreground': '#202020',
      '--muted': '#EDEDED',
      '--muted-foreground': '#5C5C5C',
      '--border': '#DDDDDD',
      '--sidebar': '#FFFFFF',
      '--surface': '#FFFFFF',
      '--surface-strong': '#FFFFFF',
      '--link-bg-hover': '#EDEDED',
      '--kicker': '#5C5C5C',
      '--success': '#1A7F4B',
      '--success-foreground': '#FFFFFF',
      '--warning': '#8A5200',
      '--warning-foreground': '#FFFFFF',
      '--destructive': '#C0281C',
      '--destructive-foreground': '#FFFFFF',
      '--primary': '#C41E7F',
      '--primary-foreground': '#FFFFFF',
      '--accent': '#FBE4F1',
      '--accent-foreground': '#202020',
      '--ring': '#C41E7F',
    },
    darkVars: {
      '--background': '#171216',
      '--foreground': '#F2E9EE',
      '--card': '#221A20',
      '--card-foreground': '#F2E9EE',
      '--muted': '#2C222A',
      '--muted-foreground': '#AD98A4',
      '--border': '#382B33',
      '--sidebar': '#1B1519',
      '--surface': '#221A20',
      '--surface-strong': '#221A20',
      '--link-bg-hover': '#2C222A',
      '--kicker': '#AD98A4',
      '--success': '#35B06E',
      '--success-foreground': '#141414',
      '--warning': '#F5CE4B',
      '--warning-foreground': '#141414',
      '--destructive': '#E4695C',
      '--destructive-foreground': '#141414',
      '--primary': '#F262B0',
      '--primary-foreground': '#1E0A16',
      '--accent': '#35162A',
      '--accent-foreground': '#F2E9EE',
      '--ring': '#F262B0',
    },
  },
  {
    name: 'carbon',
    label: 'Carbon',
    dot: '#202020',
    vars: {
      '--background': '#F5F5F5',
      '--foreground': '#202020',
      '--card': '#FFFFFF',
      '--card-foreground': '#202020',
      '--muted': '#EDEDED',
      '--muted-foreground': '#5C5C5C',
      '--border': '#DDDDDD',
      '--sidebar': '#FFFFFF',
      '--surface': '#FFFFFF',
      '--surface-strong': '#FFFFFF',
      '--link-bg-hover': '#EDEDED',
      '--kicker': '#5C5C5C',
      '--success': '#1A7F4B',
      '--success-foreground': '#FFFFFF',
      '--warning': '#8A5200',
      '--warning-foreground': '#FFFFFF',
      '--destructive': '#C0281C',
      '--destructive-foreground': '#FFFFFF',
      '--primary': '#202020',
      '--primary-foreground': '#FFFFFF',
      '--accent': '#E4E4E4',
      '--accent-foreground': '#202020',
      '--ring': '#202020',
    },
    darkVars: {
      '--background': '#131313',
      '--foreground': '#F0F0F0',
      '--card': '#1E1E1E',
      '--card-foreground': '#F0F0F0',
      '--muted': '#282828',
      '--muted-foreground': '#A6A6A6',
      '--border': '#333333',
      '--sidebar': '#181818',
      '--surface': '#1E1E1E',
      '--surface-strong': '#1E1E1E',
      '--link-bg-hover': '#282828',
      '--kicker': '#A6A6A6',
      '--success': '#35B06E',
      '--success-foreground': '#141414',
      '--warning': '#F5CE4B',
      '--warning-foreground': '#141414',
      '--destructive': '#E4695C',
      '--destructive-foreground': '#141414',
      '--primary': '#F0F0F0',
      '--primary-foreground': '#141414',
      '--accent': '#2E2E2E',
      '--accent-foreground': '#F0F0F0',
      '--ring': '#F0F0F0',
    },
  },
]

// ── Storage keys ────────────────────────────────────────────
const MODE_KEY = 'theme'
const COLOR_KEY = 'flint-color-theme'
export const DEFAULT_MODE: ThemeMode = 'auto'
export const DEFAULT_COLOR = 'ember'

export const themeModes: { value: ThemeMode; label: string }[] = [
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
  { value: 'auto', label: 'System' },
]

// ── Color palette ───────────────────────────────────────────
export function getStoredColorTheme(): string {
  if (typeof window === 'undefined') return DEFAULT_COLOR
  return window.localStorage.getItem(COLOR_KEY) ?? DEFAULT_COLOR
}

function isDark(): boolean {
  if (typeof window === 'undefined') return false
  return document.documentElement.classList.contains('dark')
}

export function applyColorTheme(theme: ColorTheme) {
  const vars = isDark() ? theme.darkVars : theme.vars
  const root = document.documentElement
  for (const [key, value] of Object.entries(vars)) {
    root.style.setProperty(key, value)
  }
  // Re-apply body background since it uses CSS vars
  document.body.style.background = ''
}

// applyColorThemeByName applies a palette by name and persists the choice.
export function setColorTheme(name: string) {
  const theme = themes.find((t) => t.name === name) ?? themes[0]
  if (typeof window !== 'undefined') window.localStorage.setItem(COLOR_KEY, theme.name)
  applyColorTheme(theme)
}

// ── Light/dark mode ─────────────────────────────────────────
export function getStoredMode(): ThemeMode {
  if (typeof window === 'undefined') return DEFAULT_MODE
  const stored = window.localStorage.getItem(MODE_KEY)
  return stored === 'light' || stored === 'dark' || stored === 'auto' ? stored : DEFAULT_MODE
}

export function applyThemeMode(mode: ThemeMode) {
  const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
  const resolved = mode === 'auto' ? (prefersDark ? 'dark' : 'light') : mode
  const root = document.documentElement
  root.classList.remove('light', 'dark')
  root.classList.add(resolved)
  if (mode === 'auto') root.removeAttribute('data-theme')
  else root.setAttribute('data-theme', mode)
  root.style.colorScheme = resolved
  // The active palette resolves different vars per mode, so re-apply it.
  applyColorTheme(themes.find((t) => t.name === getStoredColorTheme()) ?? themes[0])
}

export function setThemeMode(mode: ThemeMode) {
  if (typeof window !== 'undefined') window.localStorage.setItem(MODE_KEY, mode)
  applyThemeMode(mode)
}

// reconcileAppearance is called once the server profile loads: server values
// win, so we mirror them into localStorage and apply. Missing values keep the
// local choice. Returns the effective {mode, color} for UI state.
export function reconcileAppearance(prefs: { themeMode?: string; colorTheme?: string }): {
  mode: ThemeMode
  color: string
} {
  let mode = getStoredMode()
  if (prefs.themeMode === 'light' || prefs.themeMode === 'dark' || prefs.themeMode === 'auto') {
    mode = prefs.themeMode
    if (typeof window !== 'undefined') window.localStorage.setItem(MODE_KEY, mode)
  }
  let color = getStoredColorTheme()
  if (prefs.colorTheme && themes.some((t) => t.name === prefs.colorTheme)) {
    color = prefs.colorTheme
    if (typeof window !== 'undefined') window.localStorage.setItem(COLOR_KEY, color)
  }
  applyThemeMode(mode) // also re-applies the palette
  return { mode, color }
}
