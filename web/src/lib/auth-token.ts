// Client-side session token storage. The token lives in BOTH localStorage (read
// by the browser oRPC link and SSE connections) and a cookie (so the SSR data
// layer — backend.ts, which runs server-side — can authenticate the initial
// render; localStorage isn't available during SSR). Keep the two in sync via
// storeSession / clearSession.

const ACCESS_KEY = 'flint_access_token'
const REFRESH_KEY = 'flint_refresh_token'
export const ACCESS_COOKIE = 'flint_access_token'

export function getAccessToken(): string | null {
  if (typeof window === 'undefined') return null
  try {
    return localStorage.getItem(ACCESS_KEY)
  } catch {
    return null
  }
}

// storeSession persists the tokens after a successful login (client-side). The
// access token is also mirrored to a cookie so SSR can forward it to the API.
export function storeSession(accessToken: string, refreshToken: string): void {
  if (typeof window === 'undefined') return
  try {
    localStorage.setItem(ACCESS_KEY, accessToken)
    localStorage.setItem(REFRESH_KEY, refreshToken)
  } catch {
    // ignore storage failures (private mode, etc.)
  }
  // SameSite=Lax: sent on top-level navigations (the SSR document request) but
  // not cross-site. max-age tracks the 24h session.
  document.cookie = `${ACCESS_COOKIE}=${accessToken}; path=/; max-age=86400; SameSite=Lax`
}

// clearSession removes the session on logout / 401.
export function clearSession(): void {
  if (typeof window === 'undefined') return
  try {
    localStorage.removeItem(ACCESS_KEY)
    localStorage.removeItem(REFRESH_KEY)
  } catch {
    // ignore
  }
  document.cookie = `${ACCESS_COOKIE}=; path=/; max-age=0; SameSite=Lax`
}
