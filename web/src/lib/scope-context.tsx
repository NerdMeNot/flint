import { createContext, useContext, useState, useEffect } from 'react'

interface ScopeContextValue {
  /** empty array = all workspaces */
  workspaces: string[]
  setWorkspaces: (ws: string[]) => void
  toggleWorkspace: (ws: string) => void
  /** empty array = all environments */
  environments: string[]
  setEnvironments: (envs: string[]) => void
  toggleEnvironment: (env: string) => void
}

const ScopeContext = createContext<ScopeContextValue>({
  workspaces: [],
  setWorkspaces: () => {},
  toggleWorkspace: () => {},
  environments: [],
  setEnvironments: () => {},
  toggleEnvironment: () => {},
})

export function useScope() {
  const ctx = useContext(ScopeContext)
  // Convenience aliases for filter sites that just want to ask
  // "is this row in scope?". Empty arrays mean "match everything".
  return {
    ...ctx,
    workspaceMatches: (ws: string | undefined) =>
      ctx.workspaces.length === 0 || (ws !== undefined && ctx.workspaces.includes(ws)),
    environmentMatches: (env: string | undefined) =>
      ctx.environments.length === 0 || (env !== undefined && ctx.environments.includes(env)),
  }
}

const WS_KEY = 'flint-workspaces'
const ENV_KEY = 'flint-environments'

// Backwards-compat: older builds stored a single string under flint-workspace
// / flint-environment. Read those once and migrate to the array keys.
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
  const [environments, setEnvironments] = useState<string[]>([])

  useEffect(() => {
    setWorkspaces(loadList(WS_KEY, 'flint-workspace'))
    setEnvironments(loadList(ENV_KEY, 'flint-environment'))
  }, [])

  useEffect(() => {
    if (typeof window === 'undefined') return
    if (workspaces.length > 0) localStorage.setItem(WS_KEY, JSON.stringify(workspaces))
    else localStorage.removeItem(WS_KEY)
  }, [workspaces])

  useEffect(() => {
    if (typeof window === 'undefined') return
    if (environments.length > 0) localStorage.setItem(ENV_KEY, JSON.stringify(environments))
    else localStorage.removeItem(ENV_KEY)
  }, [environments])

  function toggleWorkspace(ws: string) {
    setWorkspaces((prev) => (prev.includes(ws) ? prev.filter((x) => x !== ws) : [...prev, ws]))
  }

  function toggleEnvironment(env: string) {
    setEnvironments((prev) => (prev.includes(env) ? prev.filter((x) => x !== env) : [...prev, env]))
  }

  return (
    <ScopeContext.Provider
      value={{
        workspaces,
        setWorkspaces,
        toggleWorkspace,
        environments,
        setEnvironments,
        toggleEnvironment,
      }}
    >
      {children}
    </ScopeContext.Provider>
  )
}
