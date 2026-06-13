import { useEffect, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { orpc } from '#/lib/orpc'
import { applyThemeMode, getStoredMode, reconcileAppearance } from '#/lib/appearance'

// AppearanceSync applies the user's appearance on load and reconciles it with
// the server profile (server wins, mirrored into localStorage for the next
// flash-free paint). The pre-paint script in __root handles the light/dark
// class; this re-applies the color palette (CSS vars) which it can't, and
// pulls down cross-device prefs once /auth/me resolves. Renders nothing.
export function AppearanceSync() {
  const { data } = useQuery(orpc.auth.me.queryOptions({}))
  const reconciled = useRef(false)

  // Apply the locally-stored appearance immediately (palette + mode).
  useEffect(() => {
    applyThemeMode(getStoredMode())
  }, [])

  // Once the server profile loads, reconcile (server is source of truth).
  useEffect(() => {
    if (!data || reconciled.current) return
    reconciled.current = true
    reconcileAppearance({ themeMode: data.themeMode, colorTheme: data.colorTheme })
  }, [data])

  return null
}
