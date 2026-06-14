// Server-only: carries the incoming request's Authorization header across the
// oRPC handler execution so backend.ts can forward it to the Go API. Set in the
// /api/rpc route (rpc.$.ts) per request; read in backend.ts. AsyncLocalStorage
// keeps it correct under concurrent requests (no shared-mutable race).
import { AsyncLocalStorage } from 'node:async_hooks'

export const authTokenALS = new AsyncLocalStorage<string | undefined>()

/** The current request's Authorization header value, if any (server-side). */
export function currentAuthHeader(): string | undefined {
  return authTokenALS.getStore()
}
