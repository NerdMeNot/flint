// Client-side access to the session token stored by the login flow
// (login.tsx writes flint_access_token to localStorage). Used to authenticate
// oRPC calls and SSE connections from the browser.
export function getAccessToken(): string | null {
  if (typeof window === 'undefined') return null
  try {
    return localStorage.getItem('flint_access_token')
  } catch {
    return null
  }
}
