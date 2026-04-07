import { createContext, useContext, useState, useEffect } from 'react'

interface ScopeContextValue {
  /** undefined = all workspaces */
  workspace: string | undefined
  setWorkspace: (ws: string | undefined) => void
  /** undefined = all environments */
  environment: string | undefined
  setEnvironment: (env: string | undefined) => void
}

const ScopeContext = createContext<ScopeContextValue>({
  workspace: undefined,
  setWorkspace: () => {},
  environment: undefined,
  setEnvironment: () => {},
})

export function useScope() {
  return useContext(ScopeContext)
}

export function ScopeProvider({ children }: { children: React.ReactNode }) {
  const [workspace, setWorkspace] = useState<string | undefined>(undefined)
  const [environment, setEnvironment] = useState<string | undefined>(undefined)

  useEffect(() => {
    if (typeof window === 'undefined') return
    const savedWs = localStorage.getItem('flint-workspace')
    const savedEnv = localStorage.getItem('flint-environment')
    if (savedWs) setWorkspace(savedWs)
    if (savedEnv) setEnvironment(savedEnv)
  }, [])

  useEffect(() => {
    if (typeof window === 'undefined') return
    if (workspace) localStorage.setItem('flint-workspace', workspace)
    else localStorage.removeItem('flint-workspace')
  }, [workspace])

  useEffect(() => {
    if (typeof window === 'undefined') return
    if (environment) localStorage.setItem('flint-environment', environment)
    else localStorage.removeItem('flint-environment')
  }, [environment])

  return (
    <ScopeContext.Provider value={{ workspace, setWorkspace, environment, setEnvironment }}>
      {children}
    </ScopeContext.Provider>
  )
}
