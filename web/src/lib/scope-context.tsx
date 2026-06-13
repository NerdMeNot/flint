import { createContext, useContext, useState, useEffect } from 'react'

// Global scope = the ownership axis only (workspace). Environment is NOT a global
// scope: it's a property of a run/deployment, so it lives as a LOCAL filter on
// the Runs / Gates / Deployments surfaces (see docs/design/grouping-taxonomy.md).
interface ScopeContextValue {
  /** empty array = all workspaces */
  workspaces: string[]
  setWorkspaces: (ws: string[]) => void
  toggleWorkspace: (ws: string) => void
}

const ScopeContext = createContext<ScopeContextValue>({
  workspaces: [],
  setWorkspaces: () => {},
  toggleWorkspace: () => {},
})

export function useScope() {
  const ctx = useContext(ScopeContext)
  // Convenience alias for filter sites that just want to ask "is this row in
  // scope?". Empty array means "match everything".
  return {
    ...ctx,
    workspaceMatches: (ws: string | undefined) =>
      ctx.workspaces.length === 0 || (ws !== undefined && ctx.workspaces.includes(ws)),
  }
}

const WS_KEY = 'flint-workspaces'

// Backwards-compat: older builds stored a single string under flint-workspace.
// Read it once and migrate to the array key.
function loadList(arrayKey: string, legacyKey: string): string[] {
  if (typeof window === 'undefined') return []
  const raw = localStorage.getItem(arrayKey)
  if (raw) {
    try {
      const parsed = JSON.parse(raw)
      if (Array.isArray(parsed)) return parsed.filter((x) => typeof x === 'string')
    } catch {
      // fall through to legacy
    }
  }
  const legacy = localStorage.getItem(legacyKey)
  if (legacy) {
    localStorage.removeItem(legacyKey)
    return [legacy]
  }
  return []
}

export function ScopeProvider({ children }: { children: React.ReactNode }) {
  const [workspaces, setWorkspaces] = useState<string[]>([])

  useEffect(() => {
    setWorkspaces(loadList(WS_KEY, 'flint-workspace'))
    // Drop any persisted environment scope from the pre-demotion model.
    if (typeof window !== 'undefined') {
      localStorage.removeItem('flint-environments')
      localStorage.removeItem('flint-environment')
    }
  }, [])

  useEffect(() => {
    if (typeof window === 'undefined') return
    if (workspaces.length > 0) localStorage.setItem(WS_KEY, JSON.stringify(workspaces))
    else localStorage.removeItem(WS_KEY)
  }, [workspaces])

  function toggleWorkspace(ws: string) {
    setWorkspaces((prev) => (prev.includes(ws) ? prev.filter((x) => x !== ws) : [...prev, ws]))
  }

  return (
    <ScopeContext.Provider value={{ workspaces, setWorkspaces, toggleWorkspace }}>
      {children}
    </ScopeContext.Provider>
  )
}
