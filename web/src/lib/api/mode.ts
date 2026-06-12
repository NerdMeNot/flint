// API mode controls how oRPC handlers resolve data. Set via the FLINT_API_MODE
// env var at server startup (the oRPC router runs on the TanStack Start server).
//
//   mock  — never call the backend; serve the in-memory mock store. Writes
//           mutate that store, so pure UI development feels real, works offline,
//           and is deterministic.
//   live  — always call the backend; never fall back. Real errors surface to
//           the UI (no silent demo data). Use in production + integration.
//   auto  — try the backend, fall back to mock data only when it is genuinely
//           unreachable, and flag it so the UI can warn. Convenient default for
//           local dev where the backend may or may not be running.
//
// Default: 'live' in production (never silently serve fake data), 'auto' in dev.

export type ApiMode = 'mock' | 'live' | 'auto'

export function apiMode(): ApiMode {
  const raw = (typeof process !== 'undefined' ? process.env?.FLINT_API_MODE : '')?.toLowerCase()
  if (raw === 'mock' || raw === 'live' || raw === 'auto') return raw
  const isProd = typeof process !== 'undefined' && process.env?.NODE_ENV === 'production'
  return isProd ? 'live' : 'auto'
}

export const isMockMode = () => apiMode() === 'mock'
export const isLiveMode = () => apiMode() === 'live'
